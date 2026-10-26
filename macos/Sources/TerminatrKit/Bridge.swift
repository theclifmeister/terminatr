import Foundation

/// The lines between the app and tm app bridge (docs/SPEC.md §3.8): the
/// bridge's stdout one line at a time, and the app's requests to its
/// stdin. ProcessTransport is the real one; tests script their own.
public protocol BridgeTransport: Sendable {
    /// The bridge's lines, without their newline. It finishes when the
    /// bridge does, throwing BridgeError.closed when that wasn't asked.
    var lines: AsyncThrowingStream<Data, any Error> { get }
    /// Writes one line (the newline is the transport's).
    func send(_ line: Data) throws
    /// Ends the bridge: its stdin closes, and it exits.
    func close()
}

public enum BridgeError: Error, LocalizedError, Equatable {
    /// The bridge ended, or couldn't start: why, as far as it said.
    case closed(String)
    /// The bridge reached no server (its hello's error): `server`, or
    /// `server-mismatch` when the server is older than tm.
    case hello(AppAPI.Error)
    /// A line that isn't the app API's.
    case badLine(String)

    public var errorDescription: String? {
        switch self {
        case .closed(let why): return why.isEmpty ? "tm app bridge ended." : why
        case .hello(let e):
            if e.code == "server-mismatch" {
                return "\(e.message) Run tm server restart to switch to the installed tm."
            }
            return e.message
        case .badLine(let why): return "tm app bridge sent a line the app can't read: \(why)"
        }
    }
}

/// One event of a subscription: its name (one of the method's events)
/// and its data.
public struct BridgeEvent<Value: Sendable>: Sendable {
    public let name: String
    public let data: Value
}

/// An open stream: the method's result, then its events until the
/// stream ends. Ending the iteration (or dropping the stream) cancels it.
public struct BridgeSubscription<Answer: Sendable, Value: Sendable>: Sendable {
    public let id: Int
    public let result: Answer
    public let events: AsyncThrowingStream<BridgeEvent<Value>, any Error>
}

/// The client of tm app bridge: one child process, its requests
/// multiplexed by id. Calls answer once; each subscription is one
/// request id whose events come as an AsyncThrowingStream.
public actor Bridge {
    public nonisolated let transport: any BridgeTransport
    /// The bridge's hello: its app API, tm and the server it reached.
    public private(set) var hello: AppAPI.Hello?

    private var nextID = 1
    private var calls: [Int: @Sendable (Result<Data, any Error>) -> Void] = [:]
    private var streams: [Int: Stream] = [:]
    private var helloWaiter: CheckedContinuation<AppAPI.Hello, any Error>?
    private var ended: (any Error)?
    private let decoder = AppAPI.decoder()
    private let encoder = AppAPI.encoder()

    /// A stream's id: before its result, the waiter for it; after, its
    /// events' sink.
    private struct Stream {
        var started: (@Sendable (Result<Data, any Error>) -> Void)?
        var event: @Sendable (Data) -> Void
        var end: @Sendable ((any Error)?) -> Void
    }

    public init(transport: any BridgeTransport) {
        self.transport = transport
    }

    /// Starts tm app bridge with the tm at tm and waits for its hello.
    public static func launch(tm: URL, environment: [String: String]? = nil) async throws -> Bridge {
        let b = Bridge(transport: try ProcessTransport(tm: tm, environment: environment))
        _ = try await b.start()
        return b
    }

    /// Reads the lines until the bridge ends, and returns its hello. A
    /// hello with an error throws BridgeError.hello.
    public func start() async throws -> AppAPI.Hello {
        let lines = transport.lines
        Task { await self.read(lines) }
        return try await withCheckedThrowingContinuation { helloWaiter = $0 }
    }

    /// Calls method m with params and returns its result.
    public func call<P, R>(_ m: AppAPI.Call<P, R>, _ params: P) async throws -> R {
        if let ended { throw ended }
        let id = take()
        let line = try request(id, m.name, params)
        let decoder = self.decoder
        return try await withCheckedThrowingContinuation { (k: CheckedContinuation<R, any Error>) in
            calls[id] = { r in
                k.resume(with: r.flatMap { data in
                    Result { try decoder.decode(ResultLine<R>.self, from: data).result }
                })
            }
            do {
                try transport.send(line)
            } catch {
                calls[id] = nil
                k.resume(throwing: BridgeError.closed(error.localizedDescription))
            }
        }
    }

    public func call<R>(_ m: AppAPI.Call<AppAPI.NoParams, R>) async throws -> R {
        try await call(m, AppAPI.NoParams())
    }

    /// Opens stream m with params: its result once the bridge answers,
    /// and its events. Cancelling the events' iteration sends cancel.
    public func subscribe<P, R, D>(_ m: AppAPI.Subscription<P, R, D>, _ params: P) async throws -> BridgeSubscription<R, D> {
        if let ended { throw ended }
        let id = take()
        let line = try request(id, m.name, params)
        let decoder = self.decoder
        let (events, sink) = AsyncThrowingStream<BridgeEvent<D>, any Error>.makeStream()
        sink.onTermination = { [weak self] why in
            if case .cancelled = why, let self {
                Task { await self.cancel(id) }
            }
        }
        let result: R = try await withCheckedThrowingContinuation { (k: CheckedContinuation<R, any Error>) in
            streams[id] = Stream(
                started: { r in
                    k.resume(with: r.flatMap { data in
                        Result { try decoder.decode(ResultLine<R>.self, from: data).result }
                    })
                },
                event: { data in
                    do {
                        let e = try decoder.decode(EventLine<D>.self, from: data)
                        sink.yield(BridgeEvent(name: e.event, data: e.data))
                    } catch {
                        sink.finish(throwing: BridgeError.badLine(String(describing: error)))
                    }
                },
                end: { err in sink.finish(throwing: err) }
            )
            do {
                try transport.send(line)
            } catch {
                streams[id] = nil
                k.resume(throwing: BridgeError.closed(error.localizedDescription))
            }
        }
        return BridgeSubscription(id: id, result: result, events: events)
    }

    /// Ends stream id: the bridge answers with its end line.
    public func cancel(_ id: Int) {
        guard streams[id] != nil, ended == nil else { return }
        let cid = take()
        guard let line = try? request(cid, "cancel", AppAPI.CancelParams(id: id)) else { return }
        calls[cid] = { _ in }
        try? transport.send(line)
    }

    /// Ends the bridge.
    public func close() {
        transport.close()
    }

    private func take() -> Int {
        defer { nextID += 1 }
        return nextID
    }

    private func request<P: Encodable>(_ id: Int, _ method: String, _ params: P) throws -> Data {
        if params is AppAPI.NoParams {
            return try encoder.encode(RequestLine<P>(id: id, method: method, params: nil))
        }
        return try encoder.encode(RequestLine(id: id, method: method, params: params))
    }

    private func read(_ lines: AsyncThrowingStream<Data, any Error>) async {
        do {
            for try await line in lines {
                receive(line)
            }
            finish(BridgeError.closed(""))
        } catch {
            finish(error)
        }
    }

    func receive(_ line: Data) {
        let h: Header
        do {
            h = try decoder.decode(Header.self, from: line)
        } catch {
            return // not ours to guess at; the bridge writes only Out lines
        }
        if let hello = h.hello {
            self.hello = hello
            if let e = hello.error {
                helloWaiter?.resume(throwing: BridgeError.hello(e))
            } else {
                helloWaiter?.resume(returning: hello)
            }
            helloWaiter = nil
            return
        }
        guard let id = h.id else { return }
        if let done = calls.removeValue(forKey: id) {
            done(h.error.map { .failure($0) } ?? .success(line))
            return
        }
        guard var s = streams[id] else { return }
        if let started = s.started {
            s.started = nil
            if let e = h.error {
                streams[id] = nil
                started(.failure(e))
                s.end(e)
                return
            }
            streams[id] = s
            started(.success(line))
            return
        }
        if h.end == true {
            streams[id] = nil
            s.end(h.error)
        } else if h.event != nil {
            s.event(line)
        }
    }

    private func finish(_ error: any Error) {
        ended = error
        helloWaiter?.resume(throwing: error)
        helloWaiter = nil
        for (_, done) in calls {
            done(.failure(error))
        }
        calls = [:]
        for (_, s) in streams {
            s.started?(.failure(error))
            s.end(error)
        }
        streams = [:]
    }
}

private struct RequestLine<P: Encodable>: Encodable {
    let id: Int
    let method: String
    let params: P?

    enum CodingKeys: String, CodingKey { case id, method, params }

    func encode(to encoder: any Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(id, forKey: .id)
        try c.encode(method, forKey: .method)
        try c.encodeIfPresent(params, forKey: .params)
    }
}

/// What every Out line has besides its result or data.
private struct Header: Decodable {
    let id: Int?
    let hello: AppAPI.Hello?
    let error: AppAPI.Error?
    let event: String?
    let end: Bool?
}

private struct ResultLine<R: Decodable>: Decodable {
    let result: R
}

private struct EventLine<D: Decodable>: Decodable {
    let event: String
    let data: D
}

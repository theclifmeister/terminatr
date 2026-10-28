import Foundation

/// A bridge that plays back a recording (the iOS app's demo mode, also
/// for App Review): its hello, then each method's recorded result, the
/// recorded events of a stream after it. It goes through Bridge as a
/// real transport does, so the recording decodes as the app API's lines.
/// It answers only what was recorded: a method it has none of, and
/// every write, gets the error demo.
///
/// A recording is one JSON object:
///
///     {"hello": {...},
///      "calls": {"session.list": {...}, ...},
///      "streams": {"projects.watch": {"result": {...}, "events": [{"event": "...", "data": {...}}]}},
///      "transcripts": {"<session id>": {"result": {...}, "events": [...]}}}
///
/// transcripts are transcript.watch's, by its params' session.
public final class DemoTransport: BridgeTransport, @unchecked Sendable {
    public let lines: AsyncThrowingStream<Data, any Error>

    private let sink: AsyncThrowingStream<Data, any Error>.Continuation
    private let calls: [String: Data]
    private let streams: [String: Stream]
    private let transcripts: [String: Stream]
    private let lock = NSLock()
    private var closed = false

    private struct Stream {
        var result: Data
        /// Each event line's body: its event and data, without its id.
        var events: [Data]
    }

    /// The error code of what the recording has no answer for.
    public static let refused = "demo"

    public init(recording: Data) throws {
        guard let r = try JSONSerialization.jsonObject(with: recording) as? [String: Any],
              let hello = r["hello"] as? [String: Any]
        else { throw BridgeError.badLine("a demo recording is an object with a hello") }
        func json(_ v: Any) throws -> Data {
            try JSONSerialization.data(withJSONObject: v, options: [.fragmentsAllowed, .sortedKeys])
        }
        func stream(_ v: Any) throws -> Stream {
            guard let s = v as? [String: Any], let result = s["result"] else {
                throw BridgeError.badLine("a demo stream has a result")
            }
            let events = try (s["events"] as? [[String: Any]] ?? []).map { e in
                try json(["event": e["event"] ?? "", "data": e["data"] ?? [String: Any]()])
            }
            return Stream(result: try json(result), events: events)
        }
        calls = try (r["calls"] as? [String: Any] ?? [:]).mapValues(json)
        streams = try (r["streams"] as? [String: Any] ?? [:]).mapValues(stream)
        transcripts = try (r["transcripts"] as? [String: Any] ?? [:]).mapValues(stream)
        (lines, sink) = AsyncThrowingStream<Data, any Error>.makeStream()
        sink.yield(try json(["hello": hello]))
    }

    public func send(_ line: Data) throws {
        guard let req = try? JSONSerialization.jsonObject(with: line) as? [String: Any],
              let id = req["id"] as? Int, let method = req["method"] as? String
        else { throw BridgeError.badLine("not a request") }
        let params = req["params"] as? [String: Any] ?? [:]
        if method == "cancel" {
            reply(id, result: Data("{}".utf8))
            if let stream = params["id"] as? Int { yield("{\"id\":\(stream),\"end\":true}") }
            return
        }
        let s = method == AppAPI.Methods.transcriptWatch.name
            ? (params["session"] as? String).flatMap { transcripts[$0] }
            : streams[method]
        if let s {
            reply(id, result: s.result)
            for e in s.events {
                // {"event":…,"data":…} with the stream's id put first.
                var l = Data("{\"id\":\(id),".utf8)
                l.append(e.dropFirst())
                yield(l)
            }
        } else if let result = calls[method] {
            reply(id, result: result)
        } else {
            yield("{\"id\":\(id),\"error\":{\"code\":\"\(Self.refused)\",\"message\":\"The demo shows recorded data: it can't \(method).\"}}")
        }
    }

    public func close() {
        let first = lock.withLock {
            defer { closed = true }
            return !closed
        }
        if first { sink.finish() }
    }

    private func reply(_ id: Int, result: Data) {
        var l = Data("{\"id\":\(id),\"result\":".utf8)
        l.append(result)
        l.append(Data("}".utf8))
        yield(l)
    }

    private func yield(_ s: String) {
        yield(Data(s.utf8))
    }

    private func yield(_ d: Data) {
        guard !lock.withLock({ closed }) else { return }
        sink.yield(d)
    }
}

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
///      "answers": {"memory.read": [{"params": {"path": "CONTEXT.md"}, "result": {...}}, ...]},
///      (an answer may have an "error" {"code", "message"} instead of a result)
///      "streams": {"projects.watch": {"result": {...}, "events": [{"event": "...", "data": {...}}]}},
///      "transcripts": {"<session id>": {"result": {...}, "events": [...]}},
///      "panes": {"<session id>": {"result": {...}, "events": [...]}},
///      "now": "2026-10-27T10:00:00Z"}
///
/// answers are a method's results by its params: the first whose params
/// the request's has, each the same, answers it, before calls'.
/// transcripts are transcript.watch's, by its params' session, and panes
/// pane.vt's the same way. now is when the recording was made: every
/// time in it (RFC 3339 in UTC, or a bare date) moves by as much as now
/// is past it, so its ages read the same on any day (the App Store's
/// screenshots, App Review).
public final class DemoTransport: BridgeTransport, @unchecked Sendable {
    public let lines: AsyncThrowingStream<Data, any Error>

    private let sink: AsyncThrowingStream<Data, any Error>.Continuation
    private let calls: [String: Data]
    private let answers: [String: [Answer]]
    private let streams: [String: Stream]
    private let transcripts: [String: Stream]
    private let panes: [String: Stream]
    private let lock = NSLock()
    private var closed = false

    private struct Answer {
        var params: [String: Any]
        var result: Data
        /// The error it answers instead, as the bridge sends one.
        var error: Data?

        func fits(_ p: [String: Any]) -> Bool {
            params.allSatisfy { k, v in (p[k] as? NSObject)?.isEqual(v) == true }
        }
    }

    private struct Stream {
        var result: Data
        /// Each event line's body: its event and data, without its id.
        var events: [Data]
    }

    /// The error code of what the recording has no answer for.
    public static let refused = "demo"

    public init(recording: Data, now: Date = Date()) throws {
        guard var r = try JSONSerialization.jsonObject(with: recording) as? [String: Any]
        else { throw BridgeError.badLine("a demo recording is an object with a hello") }
        if let then = (r["now"] as? String).flatMap(DemoClock.time) {
            r = DemoClock(by: now.timeIntervalSince(then)).shift(r) as? [String: Any] ?? r
        }
        guard let hello = r["hello"] as? [String: Any]
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
        answers = try (r["answers"] as? [String: [[String: Any]]] ?? [:]).mapValues { list in
            try list.map { a in
                if let error = a["error"] {
                    return Answer(params: a["params"] as? [String: Any] ?? [:], result: Data(), error: try json(error))
                }
                guard let result = a["result"] else { throw BridgeError.badLine("a demo answer has a result") }
                return Answer(params: a["params"] as? [String: Any] ?? [:], result: try json(result))
            }
        }
        streams = try (r["streams"] as? [String: Any] ?? [:]).mapValues(stream)
        transcripts = try (r["transcripts"] as? [String: Any] ?? [:]).mapValues(stream)
        panes = try (r["panes"] as? [String: Any] ?? [:]).mapValues(stream)
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
        let session = params["session"] as? String
        let s = switch method {
        case AppAPI.Methods.transcriptWatch.name: session.flatMap { transcripts[$0] }
        case AppAPI.Methods.paneVt.name: session.flatMap { panes[$0] }
        default: streams[method]
        }
        if let s {
            reply(id, result: s.result)
            for e in s.events {
                // {"event":…,"data":…} with the stream's id put first.
                var l = Data("{\"id\":\(id),".utf8)
                l.append(e.dropFirst())
                yield(l)
            }
        } else if let a = answers[method]?.first(where: { $0.fits(params) }) {
            if let error = a.error {
                var l = Data("{\"id\":\(id),\"error\":".utf8)
                l.append(error)
                l.append(Data("}".utf8))
                yield(l)
            } else {
                reply(id, result: a.result)
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

/// Moves a recording's times by a span: RFC 3339 times in UTC by it to
/// the second, bare dates (2026-10-27) by its whole days.
struct DemoClock {
    let by: TimeInterval

    static func time(_ s: String) -> Date? {
        try? Date(s, strategy: .iso8601)
    }

    func shift(_ v: Any) -> Any {
        switch v {
        case let o as [String: Any]: return o.mapValues(shift)
        case let a as [Any]: return a.map(shift)
        case let s as String: return shift(s)
        default: return v
        }
    }

    private func shift(_ s: String) -> String {
        if Self.shaped(s, "dddd-dd-ddTdd:dd:ddZ"), let t = Self.time(s) {
            return t.addingTimeInterval(by.rounded()).formatted(.iso8601)
        }
        if Self.shaped(s, "dddd-dd-dd"), let t = try? Date(s + "T00:00:00Z", strategy: .iso8601) {
            let days = (by / 86_400).rounded()
            return t.addingTimeInterval(days * 86_400).formatted(.iso8601.year().month().day())
        }
        return s
    }

    /// Whether s is shape's characters, each d a digit.
    private static func shaped(_ s: String, _ shape: String) -> Bool {
        s.utf8.count == shape.utf8.count && zip(s.utf8, shape.utf8).allSatisfy { c, d in
            d == UInt8(ascii: "d") ? (UInt8(ascii: "0")...UInt8(ascii: "9")).contains(c) : c == d
        }
    }
}

import Foundation
import Testing
@testable import TerminatrKit

/// A bridge on a script: lines the test feeds, and the requests the app
/// sent, each answered by reply when it returns lines.
final class ScriptTransport: BridgeTransport, @unchecked Sendable {
    let lines: AsyncThrowingStream<Data, any Error>
    private let sink: AsyncThrowingStream<Data, any Error>.Continuation
    private let lock = NSLock()
    private var sentLines: [String] = []
    var reply: @Sendable ([String: Any]) -> [String] = { _ in [] }

    init() {
        (lines, sink) = AsyncThrowingStream<Data, any Error>.makeStream()
    }

    var sent: [String] { lock.withLock { sentLines } }

    func feed(_ line: String) { sink.yield(Data(line.utf8)) }
    func end(throwing e: (any Error)? = nil) { sink.finish(throwing: e) }

    func send(_ line: Data) throws {
        let s = String(decoding: line, as: UTF8.self)
        lock.withLock { sentLines.append(s) }
        let req = try JSONSerialization.jsonObject(with: line) as! [String: Any]
        for l in reply(req) { feed(l) }
    }

    func close() { end() }
}

let helloLine = #"{"hello":{"app_api":1,"tm":"0.53.0","build":"abc","capabilities":["read","write","streams"],"methods":["projects.watch","session.list"],"server":{"version":"0.53.0","protocol":18,"pid":42,"caps":["pane-vt","transcript"]}}}"#

func started(_ t: ScriptTransport) async throws -> Bridge {
    let b = Bridge(transport: t)
    t.feed(helloLine)
    let h = try await b.start()
    #expect(h.appApi == 1)
    #expect(h.server?.protocol == 18)
    #expect(h.server?.caps == ["pane-vt", "transcript"])
    return b
}

@Test func helloError() async throws {
    let t = ScriptTransport()
    let b = Bridge(transport: t)
    t.feed(#"{"hello":{"app_api":1,"tm":"0.53.0","build":"abc","capabilities":[],"methods":[],"error":{"code":"server-mismatch","message":"the server is 0.52.0"}}}"#)
    await #expect(throws: BridgeError.hello(AppAPI.Error(code: "server-mismatch", message: "the server is 0.52.0"))) {
        try await b.start()
    }
}

@Test func callWithoutParams() async throws {
    let t = ScriptTransport()
    t.reply = { req in
        [#"{"id":\#(req["id"] as! Int),"result":{"sessions":null,"alerts":2,"notices":[]}}"#]
    }
    let b = try await started(t)
    let list = try await b.call(AppAPI.Methods.sessionList)
    #expect(list.alerts == 2)
    #expect(list.sessions.isEmpty)
    #expect(t.sent == [#"{"id":1,"method":"session.list"}"#])
}

@Test func callError() async throws {
    let t = ScriptTransport()
    t.reply = { req in
        [#"{"id":\#(req["id"] as! Int),"error":{"code":"refused","message":"no question open"}}"#]
    }
    let b = try await started(t)
    await #expect(throws: AppAPI.Error(code: "refused", message: "no question open")) {
        try await b.call(AppAPI.Methods.sessionAnswer, AppAPI.AnswerParams(id: "s1", index: 1, answer: "In memory"))
    }
    #expect(t.sent == [#"{"id":1,"method":"session.answer","params":{"answer":"In memory","id":"s1","index":1,"question":""}}"#])
}

@Test func subscription() async throws {
    let t = ScriptTransport()
    t.reply = { req in
        let id = req["id"] as! Int
        switch req["method"] as! String {
        case "projects.watch":
            return [
                #"{"id":\#(id),"result":{"projects":[{"slug":"a","active":true,"needs_you":null,"threads":null,"usage":{"turns":0,"input":0,"output":0,"cache_read":0,"cache_creation":0,"cost_usd":0}}]}}"#,
                #"{"id":\#(id),"event":"projects.changed","data":{"projects":[]}}"#,
            ]
        case "cancel":
            let target = (req["params"] as! [String: Any])["id"] as! Int
            return [#"{"id":\#(id),"result":{}}"#, #"{"id":\#(target),"end":true}"#]
        default:
            return []
        }
    }
    let b = try await started(t)
    let sub = try await b.subscribe(AppAPI.Methods.projectsWatch, AppAPI.ProjectsWatchParams(detail: true))
    #expect(sub.result.projects.map(\.slug) == ["a"])
    var it = sub.events.makeAsyncIterator()
    let e = try await it.next()
    #expect(e?.name == "projects.changed")
    #expect(e?.data.projects.isEmpty == true)
    await b.cancel(sub.id)
    #expect(try await it.next() == nil)
    #expect(t.sent.last == #"{"id":2,"method":"cancel","params":{"id":1}}"#)
}

@Test func streamBrokeWithError() async throws {
    let t = ScriptTransport()
    t.reply = { req in
        let id = req["id"] as! Int
        return [
            #"{"id":\#(id),"result":{"session":{"id":"s1","role":"thread"},"needs_you":0,"inbox":0,"queued_prompts":0}}"#,
            #"{"id":\#(id),"end":true,"error":{"code":"server","message":"connection lost"}}"#,
        ]
    }
    let b = try await started(t)
    let sub = try await b.subscribe(AppAPI.Methods.sessionWatch, AppAPI.SessionIDParams(id: "s1"))
    #expect(sub.result.session.role == "thread")
    await #expect(throws: AppAPI.Error(code: "server", message: "connection lost")) {
        for try await _ in sub.events {}
    }
}

@Test func bridgeEndFailsWhatIsOpen() async throws {
    let t = ScriptTransport()
    let b = try await started(t)
    let list = Task { try await b.call(AppAPI.Methods.sessionList) }
    while t.sent.isEmpty { await Task.yield() }
    t.end(throwing: BridgeError.closed("tm app bridge exited (1)."))
    await #expect(throws: BridgeError.closed("tm app bridge exited (1).")) { try await list.value }
    await #expect(throws: BridgeError.closed("tm app bridge exited (1).")) {
        try await b.call(AppAPI.Methods.sessionList)
    }
}

@Test func timesAndAbsentFields() throws {
    let json = #"""
    {"record":{"id":"t-0675","title":"Cache layer","agent":"claude","worktree":"/w","state":"working",
     "created":"2026-10-26T11:08:00.123456789+01:00","last_prompt":"2026-10-26T10:00:00Z","reports":0,"report_acked":0,
     "usage":{"turns":1,"input":2,"output":3,"cache_read":4,"cache_creation":5,"cost_usd":0.25}},
     "pr":801,"task":{"id":812,"ref":"T812","title":"Cache layer","status":"doing","steps":null}}
    """#
    let th = try AppAPI.decoder().decode(AppAPI.Thread.self, from: Data(json.utf8))
    #expect(th.record.id == "t-0675")
    #expect(th.record.model == "")
    #expect(th.record.reportAt == nil)
    #expect(th.record.lastPrompt == Date(timeIntervalSince1970: 1_793_008_800))
    #expect(abs(th.record.created.timeIntervalSince1970 - 1_793_009_280.123) < 0.001)
    #expect(th.status == nil)
    #expect(th.pr == 801)
    #expect(th.task?.steps == [])
    #expect(th.record.usage.costUsd == 0.25)
}

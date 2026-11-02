import Foundation
import Testing
@testable import TerminatrKit

// The apps' crash and hang reports (T957): MetricKit's JSON as the
// server's params, and the queue that waits for the link.

private let crashJSON = """
{"version":"1.0.0","callStackTree":{"callStackPerThread":true,"callStacks":[{"threadAttributed":true,"callStackRootFrames":[
 {"binaryUUID":"AAAA-LIB","binaryName":"libswiftCore.dylib","offsetIntoBinaryTextSegment":10,"sampleCount":1,"subFrames":[
  {"binaryUUID":"6F1C2D3E-0000-4000-8000-000000000001","binaryName":"Terminatr","offsetIntoBinaryTextSegment":99,"sampleCount":1}]}]}]},
 "diagnosticMetaData":{"appVersion":"0.73.1","appBuildVersion":"212","osVersion":"iPhone OS 26.1 (23B5)","deviceType":"iPhone17,1",
 "exceptionType":1,"exceptionCode":0,"signal":11,"terminationReason":"Namespace SIGNAL, Code 11"}}
"""

@Test func crashParams() {
    let p = CrashReports.params(kind: .crash, json: Data(crashJSON.utf8), at: Date(timeIntervalSince1970: 1_000), executable: "Terminatr", build: "999", ownUUID: "OWN")
    #expect(p.kind == "crash")
    #expect(p.appVersion == "0.73.1")
    #expect(p.build == "212")
    #expect(p.binaryUuid == "6F1C2D3E-0000-4000-8000-000000000001")
    #expect(p.os == "iPhone OS 26.1 (23B5)")
    #expect(p.model == "iPhone17,1")
    #expect(p.summary == "EXC_BAD_ACCESS, code 0, SIGSEGV, Namespace SIGNAL, Code 11")
    #expect(p.payload == crashJSON)
    #expect(p.time == Date(timeIntervalSince1970: 1_000))
}

@Test func hangWithoutAppFramesTakesOwnUUIDOfSameBuild() {
    let json = #"{"callStackTree":{"callStacks":[]},"diagnosticMetaData":{"appBuildVersion":"212","hangDuration":"2.5 sec"}}"#
    let same = CrashReports.params(kind: .hang, json: Data(json.utf8), at: nil, executable: "Terminatr", build: "212", ownUUID: "OWN")
    #expect(same.binaryUuid == "OWN")
    #expect(same.summary == "hang 2.5 sec")
    let other = CrashReports.params(kind: .hang, json: Data(json.utf8), at: nil, executable: "Terminatr", build: "213", ownUUID: "OWN")
    #expect(other.binaryUuid == "")
}

@Test func oversizedDiagnosticLosesItsStacks() throws {
    let big = #"{"callStackTree":{"pad":""# + String(repeating: "x", count: CrashReports.maxPayload) + #""},"diagnosticMetaData":{"appBuildVersion":"7"}}"#
    let p = CrashReports.params(kind: .cpuException, json: Data(big.utf8), at: nil, executable: "T", build: "7", ownUUID: nil)
    #expect(p.payload.utf8.count < 1000)
    let obj = try #require(try JSONSerialization.jsonObject(with: Data(p.payload.utf8)) as? [String: Any])
    #expect(obj["callStackTree"] == nil)
    #expect((obj["terminatrCut"] as? String)?.contains("left out") == true)
    #expect(p.build == "7")
}

@Test func queueSendsAndKeepsWhatFailed() async throws {
    let dir = FileManager.default.temporaryDirectory.appendingPathComponent("crq-\(UUID().uuidString)")
    defer { try? FileManager.default.removeItem(at: dir) }
    let q = CrashReportQueue(folder: dir, limit: 3)
    for i in 0..<5 {
        await q.add(AppAPI.AppReportAddParams(kind: "hang", appVersion: "1", build: "\(i)", payload: "{}"))
        try await Task.sleep(nanoseconds: 2_000_000)
    }
    #expect(await q.files().count == 3)
    // the server is gone: nothing is forgotten
    let none = await q.flush { _ in throw AppAPI.Error(code: "server", message: "down") }
    #expect(none == 0)
    #expect(await q.files().count == 3)
    // it refuses one as a report: that one goes; the rest are taken
    let builds = Box()
    let sent = await q.flush { p in
        builds.append(p.build)
        if p.build == "3" { throw AppAPI.Error(code: "bad-report", message: "no") }
        return AppAPI.AppReportAdded(id: "r\(p.build)", added: true)
    }
    #expect(sent == 2)
    #expect(builds.all == ["2", "3", "4"])
    #expect(await q.files().isEmpty)
}

private final class Box: @unchecked Sendable {
    private let lock = NSLock()
    private var items: [String] = []
    func append(_ s: String) { lock.withLock { items.append(s) } }
    var all: [String] { lock.withLock { items } }
}

@Test func ownUUIDIsAUUID() {
    #expect(CrashReports.ownUUID().flatMap(UUID.init(uuidString:)) != nil)
}

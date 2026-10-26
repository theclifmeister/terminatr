import Foundation
import Testing
@testable import TerminatrKit

/// A stand-in tm: a shell script that checks its arguments, says hello
/// and answers each request with an empty session list.
func fakeTM(_ body: String) throws -> URL {
    let dir = FileManager.default.temporaryDirectory.appendingPathComponent("tm-fake-\(UUID().uuidString)")
    try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
    let tm = dir.appendingPathComponent("tm")
    try ("#!/bin/sh\n[ \"$1 $2\" = \"app bridge\" ] || { echo \"bad args: $*\" >&2; exit 2; }\n" + body)
        .write(to: tm, atomically: true, encoding: .utf8)
    try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: tm.path)
    return tm
}

@Test func processBridge() async throws {
    let tm = try fakeTM("""
    echo '\(helloLine)'
    while IFS= read -r line; do
      id=$(printf '%s' "$line" | sed 's/^{"id":\\([0-9]*\\).*/\\1/')
      echo "{\\"id\\":$id,\\"result\\":{\\"sessions\\":[],\\"alerts\\":$id,\\"notices\\":[]}}"
    done
    """)
    let b = try await Bridge.launch(tm: tm)
    let a = try await b.call(AppAPI.Methods.sessionList)
    let c = try await b.call(AppAPI.Methods.sessionList)
    #expect(a.alerts == 1)
    #expect(c.alerts == 2)
    let hello = await b.hello
    #expect(hello?.tm == "0.53.0")
    await b.close()
}

@Test func processBridgeDies() async throws {
    let tm = try fakeTM("echo 'tm: no home' >&2\nexit 1\n")
    await #expect(throws: BridgeError.closed("tm app bridge exited (1): tm: no home")) {
        _ = try await Bridge.launch(tm: tm)
    }
}

@Test func processBridgeTooOld() async throws {
    let dir = FileManager.default.temporaryDirectory.appendingPathComponent("tm-old-\(UUID().uuidString)")
    try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
    let tm = dir.appendingPathComponent("tm")
    try "#!/bin/sh\necho 'tm: unknown command \"app\"' >&2\nexit 2\n".write(to: tm, atomically: true, encoding: .utf8)
    try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: tm.path)
    await #expect(throws: BridgeError.closed("This tm has no app bridge (tm app bridge): update it with tm update or brew upgrade terminatr.")) {
        _ = try await Bridge.launch(tm: tm)
    }
}

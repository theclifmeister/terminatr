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

/// An app opened from a tm session's pane: the bridge still runs as the
/// human, not on the agent socket as that session.
@Test func bridgeEnvironmentDropsTheSession() async throws {
    let inherited = [
        "PATH": "/usr/bin:/bin", "HOME": "/Users/u", "LANG": "en_GB.UTF-8",
        "TERMINATR": "1", "TERMINATR_SESSION": "s-913", "TERMINATR_SOCKET": "/Users/u/.terminatr/run/tm.sock",
        "TERMINATR_BIN": "/Users/u/.terminatr/run/bin/tm", "TERMINATR_ROLE": "coordinator",
        "TERMINATR_PROJECT": "terminatr", "TERMINATR_THREAD": "t-0680",
        "TERMINATR_HOME": "/Users/u/.terminatr",
    ]
    #expect(ProcessTransport.bridgeEnvironment(inherited) == [
        "PATH": "/usr/bin:/bin", "HOME": "/Users/u", "LANG": "en_GB.UTF-8",
        "TERMINATR_HOME": "/Users/u/.terminatr",
    ])
    #expect(ProcessTransport.bridgeEnvironment(["PATH": "/bin"]) == ["PATH": "/bin"])
    // Half a session still counts as one: its socket is dropped.
    #expect(ProcessTransport.bridgeEnvironment(["TERMINATR_SESSION": "s-1", "TERMINATR_SOCKET": "/s"]) == [:])
    #expect(ProcessTransport.bridgeEnvironment(["TERMINATR": "1", "TERMINATR_SOCKET": "/s"]) == [:])

    // The child gets that environment, whichever the app passed.
    let tm = try fakeTM("""
    env | grep '^TERMINATR' | sort | tr '\\n' ' ' >&2
    exit 3
    """)
    await #expect {
        _ = try await Bridge.launch(tm: tm, environment: inherited)
    } throws: { error in
        let why = "\(error)"
        return why.contains("TERMINATR_HOME=/Users/u/.terminatr") && !why.contains("TERMINATR_SOCKET") && !why.contains("TERMINATR_SESSION")
    }
}

/// An app opened by a human whose server runs on a socket of its own
/// (a dev server, say): the bridge dials that server, not a second,
/// empty one it would start beside it.
@Test func bridgeEnvironmentKeepsTheHumansSocket() async throws {
    let inherited = [
        "PATH": "/usr/bin:/bin", "TERMINATR_SOCKET": "/Users/u/dev/run/tm.sock",
        "TERMINATR_HOME": "/Users/u/dev",
    ]
    #expect(ProcessTransport.bridgeEnvironment(inherited) == inherited)

    let tm = try fakeTM("""
    env | grep '^TERMINATR_SOCKET' >&2
    exit 3
    """)
    await #expect {
        _ = try await Bridge.launch(tm: tm, environment: inherited)
    } throws: { error in
        "\(error)".contains("TERMINATR_SOCKET=/Users/u/dev/run/tm.sock")
    }
}

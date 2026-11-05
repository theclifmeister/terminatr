import Foundation
import Testing
@testable import TerminatrKit

/// The installed tm, on a Mac that has one signed by a developer; CI has none.
private let installed: URL? = ["/opt/homebrew/bin/tm", "/usr/local/bin/tm"]
    .map(URL.init(fileURLWithPath:))
    .first { TMSignature.team(of: $0) != nil }

private func scratch(_ name: String) throws -> URL {
    let dir = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
    try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
    return dir.appendingPathComponent(name)
}

@Test func aWrapperScriptHasNoTeam() throws {
    let wrapper = try scratch("tm")
    try "#!/bin/sh\nexec /opt/homebrew/bin/tm \"$@\"\n".write(to: wrapper, atomically: true, encoding: .utf8)
    try FileManager.default.setAttributes([.posixPermissions: 0o755], ofItemAtPath: wrapper.path)
    #expect(TMSignature.check(wrapper, team: "4689D3AT8J") == .noTeam)
}

@Test func aMissingFileHasNoTeam() {
    #expect(TMSignature.check(URL(fileURLWithPath: "/nowhere/tm"), team: "4689D3AT8J") == .noTeam)
}

@Test func applesOwnProgramHasNoTeam() {
    #expect(TMSignature.check(URL(fileURLWithPath: "/usr/bin/true"), team: "4689D3AT8J") == .noTeam)
}

@Test(.enabled(if: installed != nil)) func anInstalledTmPassesForItsOwnTeamOnly() throws {
    let tm = try #require(installed)
    let team = try #require(TMSignature.team(of: tm))
    #expect(TMSignature.check(tm, team: team) == .sameTeam)
    #expect(TMSignature.check(tm, team: "AAAAAAAAAA") == .otherTeam(team))
}

@Test(.enabled(if: installed != nil)) func aChangedCopyIsBroken() throws {
    let tm = try #require(installed)
    let team = try #require(TMSignature.team(of: tm))
    let copy = try scratch("tm")
    var bytes = try Data(contentsOf: tm.resolvingSymlinksInPath())
    bytes[bytes.count / 2] ^= 0xff
    try bytes.write(to: copy)
    #expect(TMSignature.check(copy, team: team) == .broken)
}

@Test func asksOnlyWhenItFailsOrThePathChanged() {
    let tm = "/opt/homebrew/bin/tm"
    // Passing, on a first run or at the same path: nothing to ask.
    #expect(TMAsk(path: tm, signature: .sameTeam, last: nil) == nil)
    #expect(TMAsk(path: tm, signature: .sameTeam, last: tm) == nil)
    // Passing at another path names the one before.
    let moved = TMAsk(path: tm, signature: .sameTeam, last: "/Users/u/.local/bin/tm")
    #expect(moved?.path == tm)
    #expect(moved?.why.contains("/Users/u/.local/bin/tm") == true)
    // Failing asks at any path, and each time.
    for s in [TMSignature.noTeam, .otherTeam("AAAAAAAAAA"), .broken] {
        #expect(TMAsk(path: tm, signature: s, last: nil)?.path == tm)
        #expect(TMAsk(path: tm, signature: s, last: tm)?.path == tm)
    }
    #expect(TMAsk(path: tm, signature: .otherTeam("AAAAAAAAAA"), last: nil)?.why.contains("AAAAAAAAAA") == true)
}

@Test func notRunSaysThePath() {
    #expect(TMNotRun(path: "/tmp/tm").localizedDescription.contains("/tmp/tm"))
}

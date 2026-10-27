import Foundation
import Testing
@testable import TerminatrKit

/// The iOS app's demo recording (ios/Terminatr/Demo/demo.json), played
/// through a Bridge: it must decode as this app API's lines, so a
/// schema change that breaks it fails here, not in App Review.
func demoBridge() async throws -> Bridge {
    let url = URL(fileURLWithPath: #filePath)
        .deletingLastPathComponent().deletingLastPathComponent().deletingLastPathComponent()
        .deletingLastPathComponent().appendingPathComponent("ios/Terminatr/Demo/demo.json")
    let b = Bridge(transport: try DemoTransport(recording: Data(contentsOf: url)))
    _ = try await b.start()
    return b
}

@Test func demoRecordingDecodes() async throws {
    let b = try await demoBridge()
    #expect(await b.hello?.appApi == AppAPI.version)
    let sessions = try await b.call(AppAPI.Methods.sessionList).sessions
    let projects = try await b.subscribe(AppAPI.Methods.projectsWatch, AppAPI.ProjectsWatchParams(detail: true, lean: true)).result.projects
    let peers = try await b.call(AppAPI.Methods.peerList).peers
    #expect(SidebarTree.active(projects).map(\.slug) == ["acme-web", "billing-api"])
    #expect(SidebarTree.inactive(projects).map(\.slug) == ["docs-site"])
    // What the start screen shows: one need of every kind it draws.
    let kinds = Set(Need.of(projects: projects, sessions: sessions, peers: peers).map(\.kind))
    #expect(kinds == [.approval, .question, .review, .blocked])
    // Every running session has its conversation.
    for s in sessions {
        let sub = try await b.subscribe(AppAPI.Methods.transcriptWatch, AppAPI.TranscriptParams(session: s.id))
        var t = Transcript()
        t.start(sub.result.status)
        for try await e in sub.events {
            t.apply(e.name, e.data)
            break
        }
        #expect(!t.entries.isEmpty, "\(s.id)")
    }
}

@Test func demoRefusesWrites() async throws {
    let b = try await demoBridge()
    await #expect(throws: AppAPI.Error.self) {
        _ = try await b.call(AppAPI.Methods.sessionPrompt, AppAPI.PromptParams(id: "s-101", text: "hi"))
    }
}

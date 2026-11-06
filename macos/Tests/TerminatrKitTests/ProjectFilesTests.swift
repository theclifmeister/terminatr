import Foundation
import Testing
@testable import TerminatrKit

private let usage = AppAPI.Usage(turns: 0, input: 0, output: 0, cacheRead: 0, cacheCreation: 0, costUsd: 0)

private func lib(_ thread: String, _ name: String, _ day: Int, peer: String = "", state: String = "running") -> AppAPI.LibraryFile {
    AppAPI.LibraryFile(thread: peer.isEmpty ? thread : "", task: peer.isEmpty ? "T1" : "", title: peer.isEmpty ? "Fix" : "from \(peer)",
                       state: state, name: name, size: 10, time: Date(timeIntervalSince1970: TimeInterval(day * 86400)),
                       peer: peer, message: peer.isEmpty ? "" : "M-1")
}

@Test func projectPagesNeedAPI21() {
    let caps = AppAPI.Server(version: "0.58.0", protocol: 23, caps: ["project-files"])
    var h = AppAPI.Hello(appApi: 21, tm: "0.58.0", build: "", methods: ["memory.list", "memory.read", "library.list", "library.read"], server: caps)
    #expect(ProjectPage.pages(h) == [.dashboard, .overview, .memory, .library])
    h.server = AppAPI.Server(version: "0.57.0", protocol: 23)
    #expect(ProjectPage.pages(h) == [.dashboard, .overview])
    h.server = caps
    h.appApi = 20
    #expect(!Compatibility.readsProjectFiles(h))
    #expect(ProjectPage.pages(nil) == [.dashboard, .overview])
}

@Test func memoryPageOrder() {
    let files = [AppAPI.MemoryFile(path: "MEMORY.md", title: "Memory", bytes: 1),
                 AppAPI.MemoryFile(path: "memory/build.md", title: "Building", bytes: 1),
                 AppAPI.MemoryFile(path: "CONTEXT.md", title: "Context", bytes: 1)]
    #expect(MemoryPage.project(files).map(\.path) == ["MEMORY.md", "CONTEXT.md"])
    #expect(MemoryPage.notes(files).map(\.path) == ["memory/build.md"])
    #expect(MemoryPage.first(files) == "CONTEXT.md")
    #expect(MemoryPage.first(Array(files.prefix(2))) == "MEMORY.md")
    #expect(MemoryPage.first([]) == nil)
}

@Test func libraryGroupsAsTheTUI() {
    let files = [lib("t-0001", "old.md", 1, state: "archived"), lib("t-0002", "a.png", 3), lib("", "sent.pdf", 2, peer: "web"),
                 lib("t-0001", "new.md", 4, state: "archived"), lib("", "more.txt", 1, peer: "web")]
    let g = LibraryPage.groups(files)
    #expect(g.map(\.id) == ["t-0001", "t-0002", "@web"])
    #expect(g[0].files.map(\.name) == ["new.md", "old.md"])
    #expect(g[0].title == "T1 · Fix")
    #expect(g[0].detail == "t-0001 · archived")
    #expect(g[2].title == "from web")
    #expect(g[2].detail == "2 files")
    #expect(LibraryPage.id(files[2]) == "M-1")
    #expect(LibraryPage.id(files[0]) == "t-0001")
}

private struct LibraryFixture: Decodable {
    struct File: Decodable {
        var thread: String?
        var task: String?
        var title: String
        var repo: String?
        var peer: String?
        var message: String?
        var name: String

        var file: AppAPI.LibraryFile {
            AppAPI.LibraryFile(thread: thread ?? "", task: task ?? "", title: title, repo: repo ?? "", state: "running", name: name, size: 1,
                               time: Date(timeIntervalSince1970: 0), peer: peer ?? "", message: message ?? "")
        }
    }

    struct Case: Decodable {
        let library: String
        let query: String
        let kind: String
        let names: [String]
        let chips: [Chip]
    }

    /// ["images", 2]: a kind, "" for all, and its count.
    struct Chip: Decodable, Equatable {
        let kind: String
        let count: Int

        init(from decoder: Decoder) throws {
            var c = try decoder.unkeyedContainer()
            kind = try c.decode(String.self)
            count = try c.decode(Int.self)
        }

        init(kind: String, count: Int) {
            self.kind = kind
            self.count = count
        }
    }

    struct Filter: Decodable {
        let files: Int
        let query: String
        let shows: Bool
    }

    let kinds: [String: String]
    let libraries: [String: [File]]
    let cases: [Case]
    let filter: [Filter]
}

/// The file the TUI's test reads too (internal/tui/libraryfilter_test.go):
/// change the rule on one side alone and one of the two fails.
@Test func libraryFiltersAsTheTUI() throws {
    let url = URL(fileURLWithPath: #filePath).deletingLastPathComponent()
        .appendingPathComponent("../../../internal/tui/testdata/library-filter.json").standardized
    let fx = try JSONDecoder().decode(LibraryFixture.self, from: Data(contentsOf: url))
    func kind(_ word: String) -> LibraryPage.Kind? {
        LibraryPage.Kind.allCases.first { $0.rawValue.lowercased() == word }
    }
    #expect(fx.cases.count >= 20)
    for (name, word) in fx.kinds {
        let got = LibraryPage.kind(name)
        #expect(got == kind(word), "kind of \(name)")
    }
    for c in fx.cases {
        let files = (fx.libraries[c.library] ?? []).map(\.file)
        let names = LibraryPage.shown(files, query: c.query, kind: kind(c.kind)).map(\.name)
        #expect(names == c.names, "\(c.library), \(c.query), \(c.kind)")
        let chips = LibraryPage.chips(files, query: c.query, kind: kind(c.kind))
            .map { LibraryFixture.Chip(kind: $0.kind?.rawValue.lowercased() ?? "", count: $0.count) }
        #expect(chips == c.chips, "\(c.library), \(c.query), \(c.kind)")
    }
    for c in fx.filter {
        let files = (0..<c.files).map { lib("t-0001", "\($0).md", 1) }
        let shows = LibraryPage.showsFilter(files, query: c.query)
        #expect(shows == c.shows, "\(c.files) files, \(c.query)")
    }
}

@Test func libraryFilterPromptNamesRepoOnlyWithOne() {
    var f = lib("t-0001", "a.md", 1)
    #expect(LibraryPage.filterPrompt([f]) == "Filter by name, task or thread")
    f.repo = "site"
    #expect(LibraryPage.filterPrompt([f]) == "Filter by name, task, thread or repo")
}

/// A library.read stand-in over data, chunk bytes at a time; changes
/// says after how many calls the file changes (its time).
private actor Chunks {
    var data: Data
    let chunk: Int
    var calls = 0
    var changes: Int?
    var params: [AppAPI.LibraryReadParams] = []

    init(_ data: Data, chunk: Int, changes: Int? = nil) {
        self.data = data
        self.chunk = chunk
        self.changes = changes
    }

    func read(_ p: AppAPI.LibraryReadParams) -> AppAPI.LibraryChunk {
        calls += 1
        params.append(p)
        let t = changes.map { calls > $0 ? 2.0 : 1.0 } ?? 1
        let end = min(p.offset + chunk, data.count)
        return AppAPI.LibraryChunk(name: p.name, size: data.count, mtime: Date(timeIntervalSince1970: t), offset: p.offset,
                                   data: data.subdata(in: p.offset..<end))
    }
}

@Test func libraryReadsInChunks() async {
    let data = Data((0..<10).map { UInt8($0) })
    let c = Chunks(data, chunk: 4)
    let out = await LibraryPage.read({ await c.read($0) }, project: "web", file: lib("t-0002", "a.bin", 1))
    #expect(out == .data(data, Date(timeIntervalSince1970: 1)))
    let ps = await c.params
    #expect(ps.map(\.offset) == [0, 4, 8])
    #expect(ps.allSatisfy { $0.project == "web" && $0.id == "t-0002" && $0.name == "a.bin" })

    // A file that changes between chunks is read again, once.
    let changing = Chunks(data, chunk: 4, changes: 1)
    let again = await LibraryPage.read({ await changing.read($0) }, project: "web", file: lib("t-0002", "a.bin", 1))
    #expect(again == .data(data, Date(timeIntervalSince1970: 2)))

    let empty = Chunks(Data(), chunk: 4)
    #expect(await LibraryPage.read({ await empty.read($0) }, project: "web", file: lib("t-0002", "e", 1)) == .data(Data(), Date(timeIntervalSince1970: 1)))
}

@Test func libraryReadRefused() async {
    let out = await LibraryPage.read({ _ in throw AppAPI.Error(code: "unknown-file", message: "gone") }, project: "web",
                                     file: lib("t-0002", "a.bin", 1))
    #expect(out == .unavailable("gone"))
}

@Test func overviewAsTheTUI() {
    let rec = { (id: String, agent: String) in
        AppAPI.Thread(record: AppAPI.ThreadRecord(id: id, title: id, agent: agent, worktree: "", state: "running", created: Date(),
                                                  reports: 0, reportAcked: 0, usage: usage))
    }
    let p = AppAPI.Project(slug: "web", active: true, threads: [rec("t-0002", "codex"), rec("t-0001", ""), rec("t-0003", "codex")],
                           goal: " Ship it \n", repos: ["/src/web", "/src/api"], counts: ["in_motion": 3, "on_deck": 5, "done": 9],
                           usage: usage, checkouts: ["/src/api": "behind origin by 2"])
    let c = AppAPI.Session(id: "s1", role: "coordinator", agent: "claude", project: "web", cols: 80, rows: 24, created: Date(), clients: 0,
                           state: "working")
    let o = ProjectOverview(p, coordinator: c, needs: 1)
    #expect(o.goal == "Ship it")
    #expect(o.repos == [.init(path: "/src/web", note: ""), .init(path: "/src/api", note: "behind origin by 2")])
    #expect(o.coordinator == "claude · working")
    #expect(o.agents == [.init(agent: "claude", threads: ["t-0001"]), .init(agent: "codex", threads: ["t-0002", "t-0003"])])
    #expect(o.tasks == "1 needs you · 3 in motion · 5 on deck · 9 done")
    #expect(ProjectOverview(p, coordinator: nil, needs: 2).coordinator == "")
}

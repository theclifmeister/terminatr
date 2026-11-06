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
        var task_status: String?
        var title: String
        var repo: String?
        var state: String?
        var peer: String?
        var message: String?
        var project: String?
        var server: String?
        var name: String
        var doc_title: String?
        var published: String?
        var size: Int?

        var file: AppAPI.LibraryFile {
            AppAPI.LibraryFile(thread: thread ?? "", task: task ?? "", taskStatus: task_status ?? "", title: title, repo: repo ?? "", state: state ?? "running", name: name, docTitle: doc_title ?? "", published: published ?? "", size: size ?? 0,
                               time: Date(timeIntervalSince1970: 0), peer: peer ?? "", message: message ?? "", project: project ?? "", server: server ?? "")
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

    struct Order: Decodable {
        let library: String
        let names: [String]
    }

    let kinds: [String: String]
    let libraries: [String: [File]]
    let cases: [Case]
    let filter: [Filter]
    let order: [Order]
    let heads: [Head]
    let fold: [Fold]
    let biggest: [Biggest]
    let sort: [Sort]

    struct Biggest: Decodable {
        let library: String
        let query: String
        let kind: String
        let names: [String]
    }

    struct Sort: Decodable {
        let files: Int
        let shows: Bool
    }

    let stacks: [Stack]
    let strips: [Strip]

    /// A group as tiles: titles, a stack being one, and what +N counts.
    struct Strip: Decodable {
        let why: String
        let names: [String]
        let strip: [String]
        let rows: [String]
        let more: Int
    }

    struct Stack: Decodable {
        let why: String
        var names: [String]?
        var library: String?
        let rows: [[String]]
    }

    struct Head: Decodable {
        let library: String
        let group: String
        let status: String
        let `where`: [String]
        let archived: Bool
        let kinds: [String]
        let bytes: Int
    }

    struct Fold: Decodable {
        let files: Int
        let open: Bool
        let query: String
        let rows: Int
        let more: Int
        let fewer: Bool
    }
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
    for c in fx.order {
        let names = LibraryPage.groups((fx.libraries[c.library] ?? []).map(\.file)).flatMap { $0.files.map(\.name) }
        #expect(names == c.names, "order of \(c.library)")
    }
    #expect(fx.heads.count >= 6)
    for c in fx.heads {
        let g = LibraryPage.groups((fx.libraries[c.library] ?? []).map(\.file)).first { $0.id == c.group }
        #expect(g?.status == c.status, "\(c.library)/\(c.group)")
        #expect(g?.origin == c.where, "\(c.library)/\(c.group)")
        #expect(g?.isArchived == c.archived, "\(c.library)/\(c.group)")
        #expect(g?.kinds == c.kinds, "\(c.library)/\(c.group)")
        #expect(g?.bytes == c.bytes, "\(c.library)/\(c.group)")
    }
    #expect(fx.fold.count >= 9)
    for c in fx.fold {
        let got = LibraryPage.fold(c.files, open: c.open, query: c.query)
        #expect(got.rows == c.rows && got.more == c.more && got.fewer == c.fewer, "\(c)")
    }
    #expect(fx.biggest.count >= 7)
    for c in fx.biggest {
        let files = (fx.libraries[c.library] ?? []).map(\.file)
        let names = LibraryPage.biggestFirst(LibraryPage.shown(files, query: c.query, kind: kind(c.kind))).map(\.name)
        #expect(names == c.names, "\(c.library), \(c.query), \(c.kind)")
    }
    #expect(fx.sort.count >= 4)
    for c in fx.sort {
        let files = (0..<c.files).map { lib("t-0001", "\($0).md", 1) }
        #expect(LibraryPage.showsSort(files) == c.shows, "\(c.files) files")
    }
    for c in fx.filter {
        let files = (0..<c.files).map { lib("t-0001", "\($0).md", 1) }
        let shows = LibraryPage.showsFilter(files, query: c.query)
        #expect(shows == c.shows, "\(c.files) files, \(c.query)")
    }
}

/// Biggest first says what a file came with, however little it has; equal
/// sizes stay in the list's order however many there are; and a sort only
/// holds while there is something to sort.
@Test func libraryBiggestFirst() {
    let linked = AppAPI.LibraryFile(thread: "", task: "", title: "from web", state: "received", name: "b.zip", size: 3, time: Date(), peer: "web", message: "M-1")
    #expect(LibraryPage.source(linked) == "from web")
    #expect(LibraryPage.source(lib("t-0002", "a.png", 1)) == "T1 · Fix")
    let bare = AppAPI.LibraryFile(thread: "t-0002", title: "", state: "stopped", name: "a.png", size: 1, time: Date())
    #expect(LibraryPage.source(bare) == "t-0002")
    let many = (0..<200).map { i -> AppAPI.LibraryFile in
        var f = lib("t-0001", "\(i).png", 1)
        f.size = i % 2 == 0 ? 5 : 9
        return f
    }
    let names = LibraryPage.biggestFirst(many).map(\.name)
    let want = many.filter { $0.size == 9 }.map(\.name) + many.filter { $0.size == 5 }.map(\.name)
    #expect(names == want)
    #expect(!LibraryPage.isFlat([lib("t-0001", "a.png", 1)], .biggest))
    #expect(!LibraryPage.isFlat(many, .newest))
    #expect(LibraryPage.isFlat(many, .biggest))
}

/// A stack comes apart in the flat list, each half its own row and rank, and
/// either half finds its way back to the stack's row: what the switch carries
/// the pick by.
@Test func libraryBiggestRowsPartAStack() {
    var dark = lib("t-0001", "site-dark.png", 1), light = lib("t-0001", "site-light.png", 1)
    var between = lib("t-0002", "x.png", 1)
    dark.size = 900
    between.size = 500
    light.size = 200
    let rows = LibraryPage.biggestRows([dark, light, between])
    #expect(rows.map(\.files) == [[dark], [between], [light]])
    #expect(rows.allSatisfy { !$0.isStack && $0.title == $0.files[0].name })
    #expect(Set(rows.map(\.id)).count == 3)
    let g = LibraryPage.Group(id: "t-0001", files: [dark, light])
    let l = g.listed(open: false, query: "")
    let stack = l.row(of: light)
    #expect(stack?.isStack == true)
    #expect(stack?.id == l.row(of: dark)?.id)
    #expect(stack?.id == rows[0].id)
    #expect(l.row(of: between) == nil)
}

/// T1093: a file over the cap isn't asked for, and says why; before and
/// after need both.
@Test func libraryReadOverTheCapAsksNothing() async {
    var f = lib("t-0001", "demo.mp4", 1)
    f.size = 175_100_000
    let asked = await LibraryPage.read({ _ in throw CancellationError() }, project: "p", file: f)
    #expect(asked == .unavailable(FileImage.unreadable(name: "demo.mp4", size: f.size) ?? ""))
    var small = lib("t-0001", "a-before.png", 1)
    small.size = 10
    #expect(LibraryPage.biggestRows([small])[0].readable)
    #expect(!LibraryPage.biggestRows([f])[0].readable)
}

/// A group's line under its title, its day pinned to one calendar.
@Test func libraryHeadingSaysItsFacts() throws {
    var utc = Calendar(identifier: .gregorian)
    utc.timeZone = try #require(TimeZone(identifier: "UTC"))
    let at = Date(timeIntervalSince1970: 1_780_000_000)
    let now = at.addingTimeInterval(86400)
    func file(_ name: String, repo: String = "", state: String = "running") -> AppAPI.LibraryFile {
        AppAPI.LibraryFile(thread: "t-0003", task: "T3", taskStatus: "done", title: "Ship", repo: repo, state: state, name: name, size: 1500, time: at)
    }
    func facts(_ fs: [AppAPI.LibraryFile], now: Date) -> String {
        LibraryPage.groups(fs).first?.facts(now: now, calendar: utc) ?? ""
    }
    let day = LibraryPage.day(at, now: now, calendar: utc)
    #expect(!day.contains("2026"))
    #expect(facts([file("a.png"), file("b.png")], now: now) == "t-0003 · \(day) · 2 images · 3 KB")
    #expect(facts([file("a.png", repo: "api")], now: now) == "t-0003 · api · \(day) · 1 image · 2 KB")
    #expect(facts([file("a.md", state: "archived")], now: now) == "t-0003 · \(day) · archived · 1 note · 2 KB")
    let later = at.addingTimeInterval(400 * 86400)
    #expect(LibraryPage.day(at, now: later, calendar: utc).contains("2026"))
    let linked = AppAPI.LibraryFile(title: "from web", state: "received", name: "a.pdf", size: 1500, time: at, peer: "web", message: "M-1")
    let g = try #require(LibraryPage.groups([linked]).first)
    #expect(g.status.isEmpty && g.origin.isEmpty)
    #expect(g.title == "from web")
    #expect(g.facts(now: now, calendar: utc) == "\(day) · 1 page · 2 KB")
}

/// A group of 37: six rows and +31 more, until it's opened; a filter opens it.
@Test func libraryGroupFolds() throws {
    let fs = (0..<37).map { lib("t-0001", "\($0).png", 37 - $0) }
    let g = try #require(LibraryPage.groups(fs).first)
    let shut = g.listed(open: false, query: "")
    #expect(shut.rows.map(\.title) == (0..<6).map { "\($0).png" } && shut.more == 31 && !shut.fewer)
    let open = g.listed(open: true, query: "")
    #expect(open.rows.count == 37 && open.more == 0 && open.fewer)
    #expect(g.listed(open: false, query: "1").rows.count == 37)
    #expect(g.kinds == ["37 images"])
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

/// A thumbnail stand-in that counts its calls.
private actor Thumbs {
    var names: [String] = []
    func thumb(_ name: String) -> AppAPI.LibraryThumbnail {
        names.append(name)
        return AppAPI.LibraryThumbnail(name: name, width: 4, height: 3, data: Data([0xff, 0xd8]))
    }
    func refuse(_ name: String) throws -> AppAPI.LibraryThumbnail {
        names.append(name)
        throw AppAPI.Error(code: "no-thumbnail", message: "\(name) has no thumbnail")
    }
}

@Test func thumbnailsNeedAPI53AndTheCap() {
    let caps = AppAPI.Server(version: "0.83.0", protocol: 38, caps: ["project-files", "peer-pages", "library-thumbnails"])
    let methods = ["library.read", "library.thumbnail", "peer.pages-read", "peer.library-read", "peer.library-thumbnail"]
    var h = AppAPI.Hello(appApi: 53, tm: "0.83.0", build: "", methods: methods, server: caps)
    #expect(Compatibility.readsThumbnails(h) && Compatibility.readsPeerThumbnails(h))
    h.appApi = 52
    #expect(!Compatibility.readsThumbnails(h) && !Compatibility.readsPeerThumbnails(h))
    h.appApi = 53
    // A newer tm on an older server: rows.
    h.server = AppAPI.Server(version: "0.82.0", protocol: 38, caps: ["project-files", "peer-pages"])
    #expect(!Compatibility.readsThumbnails(h) && !Compatibility.readsPeerThumbnails(h))
    h.server = caps
    // A device's list without the method: rows.
    h.methods = ["library.read"]
    #expect(!Compatibility.readsThumbnails(h))
}

@Test func onlyImagesAskForThumbnails() async {
    for (name, has) in [("a.png", true), ("B.PNG", true), ("c.jpg", true), ("d.JPEG", true), ("e.gif", true),
                        ("f.svg", false), ("g.md", false), ("h.webp", false), ("png", false), ("i.png.txt", false)] {
        #expect(LibraryPage.hasThumbnail(lib("t-0001", name, 1)) == has, "\(name)")
    }
    // A library with no images makes no call.
    let t = Thumbs()
    for f in [lib("t-0001", "notes.md", 1), lib("t-0001", "pic.svg", 1), lib("t-0001", "data.json", 1)] {
        let out = await LibraryPage.thumbnail({ p in await t.thumb(p.name) }, project: "web", file: f)
        let peer = await PeerPages.thumbnail({ p in await t.thumb(p.name) }, alias: "studio", project: "web", file: f)
        #expect(out == nil && peer == nil)
    }
    let none = await t.names
    #expect(none.isEmpty)
    // An image does, named as library.read names it: a linked project's by its message.
    let linked = await LibraryPage.thumbnail({ p in
        #expect(p.project == "web" && p.id == "M-1" && p.name == "shot.png")
        return await t.thumb(p.name)
    }, project: "web", file: lib("", "shot.png", 1, peer: "api"))
    #expect(linked?.width == 4)
    let paired = await PeerPages.thumbnail({ p in
        #expect(p.alias == "studio" && p.project == "web" && p.id == "t-0001" && p.name == "shot.png")
        return await t.thumb(p.name)
    }, alias: "studio", project: "web", file: lib("t-0001", "shot.png", 1))
    #expect(paired?.height == 3)
}

@Test func aRefusedThumbnailIsARow() async {
    let t = Thumbs()
    let f = lib("t-0001", "corrupt.png", 1)
    let out = await LibraryPage.thumbnail({ p in try await t.refuse(p.name) }, project: "web", file: f)
    #expect(out == nil)
    // A paired server on an older tm answers the same.
    let peer = await PeerPages.thumbnail({ p in try await t.refuse(p.name) }, alias: "old", project: "web", file: f)
    #expect(peer == nil)
    let names = await t.names
    #expect(names == ["corrupt.png", "corrupt.png"])
}

/// A published link opens only as an https address: nothing else is handed
/// to the system (T1061).
@Test func libraryPublishedIsHTTPSOnly() {
    func link(_ s: String) -> String? {
        LibraryPage.published(AppAPI.LibraryFile(title: "", state: "", name: "a.html", published: s, size: 0, time: Date(timeIntervalSince1970: 0)))?.absoluteString
    }
    #expect(link("https://claude.ai/artifact/abc") == "https://claude.ai/artifact/abc")
    for bad in ["", "http://claude.ai/a", "file:///etc/passwd", "javascript:alert(1)", "tm://x", "/tmp/a.html", "https://"] {
        #expect(link(bad) == nil, "\(bad)")
    }
}

/// The stack rule (T1057), on the table the TUI's test reads too.
@Test func libraryStacksAsTheTUI() throws {
    let url = URL(fileURLWithPath: #filePath).deletingLastPathComponent()
        .appendingPathComponent("../../../internal/tui/testdata/library-filter.json").standardized
    let fx = try JSONDecoder().decode(LibraryFixture.self, from: Data(contentsOf: url))
    #expect(fx.stacks.count >= 25)
    for c in fx.stacks {
        var files = (c.library.flatMap { fx.libraries[$0] } ?? []).map(\.file)
        files += (c.names ?? []).map { lib("t-0001", $0, 1) }
        let rows = LibraryPage.groups(files).flatMap(\.rows).map { r in
            r.isStack ? [r.title, r.files[0].name, r.files[1].name, r.words[0], r.words[1]] : [r.files[0].name]
        }
        #expect(rows == c.rows, "\(c.why)")
    }
}

/// A stack is one tile, and one in +N (T1057, T1059), by the shared table.
@Test func libraryStacksAreOneTile() throws {
    let url = URL(fileURLWithPath: #filePath).deletingLastPathComponent()
        .appendingPathComponent("../../../internal/tui/testdata/library-filter.json").standardized
    let fx = try JSONDecoder().decode(LibraryFixture.self, from: Data(contentsOf: url))
    #expect(fx.strips.count >= 6)
    for c in fx.strips {
        let g = LibraryPage.groups(c.names.map { lib("t-0001", $0, 1) })[0]
        let l = g.listed(open: false, query: "", tiles: LibraryPage.hasThumbnail)
        #expect(l.strip.map(\.title) == c.strip, "\(c.why)")
        #expect(l.rows.map(\.title) == c.rows, "\(c.why)")
        #expect(l.more == c.more, "\(c.why)")
    }
    // One half with no thumbnail: the stack is a row, still with its switch.
    let pair = LibraryPage.groups([lib("t-0001", "a-dark.png", 1), lib("t-0001", "a-light.png", 1)])[0]
    let failed = pair.listed(open: false, query: "") { $0.name != "a-light.png" }
    #expect(failed.strip.isEmpty && failed.rows.map(\.isStack) == [true])
}

@Test func libraryStackFoldsByRowsAndCountsFiles() {
    let files = (0..<7).flatMap { i in ["dark", "light"].map { lib("t-0001", "s\(i)-\($0).png", 1) } }
    let g = LibraryPage.groups(files)[0]
    let l = g.listed(open: false, query: "")
    #expect(l.rows.count == 6 && l.more == 1)
    #expect(g.kinds == ["14 images"])
    // A filter that leaves one of the two lists it as itself.
    let one = LibraryPage.groups(LibraryPage.shown(files, query: "s0-light", kind: nil))[0]
    #expect(one.rows.map(\.isStack) == [false])
}

@Test func libraryBeforeIsOnTheLeft() throws {
    let r = try #require(LibraryPage.rows([lib("t-0001", "x-after.png", 2), lib("t-0001", "x-before.png", 1)]).first)
    #expect(r.opensSideBySide)
    #expect(r.sideBySide.map(\.word) == ["before", "after"])
    #expect(r.sideBySide.map(\.file.name) == ["x-before.png", "x-after.png"])
    let d = try #require(LibraryPage.rows([lib("t-0001", "x-dark.png", 2), lib("t-0001", "x-light.png", 1)]).first)
    #expect(!d.opensSideBySide)
}

/// A Library group with images (T1059): a strip of four, the rest rows, one
/// +N for what's left of both.
@Test func aLibraryGroupListsItsImagesAsAStrip() {
    let at = Date(timeIntervalSince1970: 1_780_000_000)
    let files = { (names: [String]) -> LibraryPage.Group in
        LibraryPage.Group(id: "t-1", files: names.map { AppAPI.LibraryFile(thread: "t-1", title: "x", state: "running", name: $0, size: 1, time: at) })
    }
    let images = { (f: AppAPI.LibraryFile) in LibraryPage.hasThumbnail(f) }
    let names = { (rs: [LibraryPage.Row]) in rs.map(\.files[0].name) }

    // No images: as before.
    let notes = files((0..<8).map { "\($0).md" })
    let none = notes.listed(open: false, query: "", tiles: images)
    #expect(none.strip.isEmpty && none.rows.count == 6 && none.more == 2 && !none.fewer)

    // One image.
    let one = files(["a.png"]).listed(open: false, query: "", tiles: images)
    #expect(names(one.strip) == ["a.png"] && one.rows.isEmpty && !one.folds)

    // Mixed: images strip, others rows, in their order, an SVG a row.
    let mixed = files(["a.png", "b.md", "c.jpg", "d.svg"]).listed(open: false, query: "", tiles: images)
    #expect(names(mixed.strip) == ["a.png", "c.jpg"] && names(mixed.rows) == ["b.md", "d.svg"] && !mixed.folds)
    #expect(names(mixed.all) == ["a.png", "c.jpg", "b.md", "d.svg"])

    // Four images and six rows stay unfolded; one over either folds.
    let full = files((0..<4).map { "\($0).png" } + (0..<6).map { "\($0).md" })
    #expect(!full.listed(open: false, query: "", tiles: images).folds)
    let over = files((0..<5).map { "\($0).png" })
    let folded = over.listed(open: false, query: "", tiles: images)
    #expect(folded.strip.count == 4 && folded.more == 1 && !folded.fewer)
    let big = files((0..<26).map { "\($0).png" } + (0..<9).map { "\($0).md" })
    let bigFolded = big.listed(open: false, query: "", tiles: images)
    #expect(bigFolded.strip.count == 4 && bigFolded.rows.count == 6 && bigFolded.more == 25)

    // Opened: everything, and it can fold again. Words open it, with no way back.
    let opened = big.listed(open: true, query: "", tiles: images)
    #expect(opened.strip.count == 26 && opened.rows.count == 9 && opened.more == 0 && opened.fewer)
    let found = big.listed(open: false, query: "png", tiles: images)
    #expect(found.strip.count == 26 && found.more == 0 && !found.fewer)

    // An image with no thumbnail becomes a row, and the next takes its place.
    let failed = over.listed(open: false, query: "") { images($0) && $0.name != "0.png" }
    #expect(names(failed.strip) == ["1.png", "2.png", "3.png", "4.png"] && names(failed.rows) == ["0.png"] && !failed.folds)

    // No tiles at all (a server too old): today's rows.
    let plain = over.listed(open: false, query: "")
    #expect(plain.strip.isEmpty && plain.rows.count == 5 && !plain.folds)
}

@Test func aLibraryStripRowsByItsWidth() {
    #expect(LibraryPage.stripColumns(width: 900) == 4)
    #expect(LibraryPage.stripColumns(width: 300) == 2)
    let at = Date(timeIntervalSince1970: 1_780_000_000)
    let files = (0..<5).map { AppAPI.LibraryFile(thread: "t", title: "", state: "", name: "\($0).png", size: 1, time: at) }
    #expect(LibraryPage.stripRows(files, columns: 4).map(\.count) == [4, 1])
    #expect(LibraryPage.stripRows(files, columns: 2).map(\.count) == [2, 2, 1])
    #expect(LibraryPage.stripRows([], columns: 4).isEmpty)
}

@Test func libraryThumbsKeepTheLatestUsed() throws {
    let png = Data(base64Encoded: "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M/wHwAEBgIApD5fRAAAAABJRU5ErkJggg==")
    guard case .drawn(let d) = FileImage.decode(try #require(png)) else {
        Issue.record("the test image didn't decode")
        return
    }
    var keep = LibraryThumbs(budget: 2)
    keep.add("a", d)
    keep.add("b", d)
    keep.touch("a")
    keep.add("c", d)
    #expect(keep["a"] != nil && keep["b"] == nil && keep["c"] != nil)
    keep.removeAll()
    #expect(keep["a"] == nil)
}

/// What a server sends is data: damaged bytes make a plain row, not a crash.
@Test func aDamagedThumbnailDrawsNothing() async {
    let bad = AppAPI.LibraryThumbnail(name: "a.png", width: 1, height: 1, data: Data("not an image".utf8))
    #expect(await LibraryPage.drawn(bad) == nil)
}

@Test func aSeatsOwnFilesHeadTheirGroup() {
    func seat(_ name: String, _ title: String, project: String = "") -> AppAPI.LibraryFile {
        AppAPI.LibraryFile(thread: LibraryPage.seatID, title: title, state: "kept", name: name, size: 1, time: Date(timeIntervalSince1970: 1), project: project)
    }
    let own = LibraryPage.groups([seat("a.md", "Coordinator's files"), seat("b.md", "Coordinator's files")])
    #expect(own.first?.title == "Coordinator's files")
    #expect(own.first?.detail == "2 files")
    #expect(own.first?.origin == [])
    let member = seat("c.md", "Coordinator's files", project: "web")
    let sections = LibraryPage.sections([seat("p.md", "Director's files"), member])
    #expect(sections.map(\.title) == ["", "web"])
    #expect(sections[1].groups[0].id == "/web/seat")
    #expect(OpenedFile(.attached(member)).id == "a:/web/seat/c.md")
    #expect(OpenedFile(.attached(seat("c.md", "Director's files"))).id == "a:seat/c.md")
}

@Test func aProgramLooksInItsOwnFormOnly() async {
    let c = Chunks(Data("hi".utf8), chunk: 10)
    let f = AppAPI.LibraryFile(thread: LibraryPage.seatID, title: "Director's files", state: "kept", name: "p.md", size: 2, time: Date())
    guard case .data(let d, _) = await LibraryPage.read({ p in await c.read(p) }, project: "", program: "acme", file: f) else {
        Issue.record("no data")
        return
    }
    #expect(String(decoding: d, as: UTF8.self) == "hi")
    let p = await c.params.first
    #expect(p?.program?.slug == "acme" && p?.project == "" && p?.id == LibraryPage.seatID)
}

import Foundation

/// A project's read-only pages beside its dashboard (T794), as the TUI's
/// project popup shows them: Overview (goal, repositories, coordinator
/// and threads by agent, tasks), Memory (CONTEXT.md, MEMORY.md and the
/// notes, Markdown) and Library (the files threads attached to their
/// reports, and those linked projects sent). Nothing here edits; no path
/// but a memory file's in the project folder crosses.
public enum ProjectPage: String, CaseIterable, Identifiable, Sendable {
    case dashboard = "Dashboard"
    case overview = "Overview"
    case memory = "Memory"
    case library = "Library"
    /// The project's mods (T869): on or off for it, and their settings
    /// for it, as the TUI's project Mods tab.
    case mods = "Mods"

    public var id: String { rawValue }

    /// The pages hello's tm has: Memory and Library need project-files,
    /// Mods project-mods.
    public static func pages(_ hello: AppAPI.Hello?) -> [ProjectPage] {
        guard let hello else { return [.dashboard, .overview] }
        var out: [ProjectPage] = [.dashboard, .overview]
        if Compatibility.readsProjectFiles(hello) { out += [.memory, .library] }
        if Compatibility.listsMods(hello) { out.append(.mods) }
        return out
    }
}

/// The Memory page's files: the project's own two first, then the notes.
public enum MemoryPage {
    public static func project(_ files: [AppAPI.MemoryFile]) -> [AppAPI.MemoryFile] {
        files.filter { !isNote($0) }
    }

    public static func notes(_ files: [AppAPI.MemoryFile]) -> [AppAPI.MemoryFile] {
        files.filter(isNote)
    }

    public static func isNote(_ f: AppAPI.MemoryFile) -> Bool { f.path.hasPrefix("memory/") }

    /// The file shown first: CONTEXT.md, else the first listed.
    public static func first(_ files: [AppAPI.MemoryFile]) -> String? {
        files.first { $0.path == "CONTEXT.md" }?.path ?? files.first?.path
    }
}

/// The Library page's files grouped as the TUI's Library tab groups
/// them: by thread, or by the linked project that sent them, the seat's own
/// group first, then the group with the newest file first and each group's
/// files newest first. A program's list keeps its projects in the order they
/// came, each ordered so (sections).
public enum LibraryPage {
    /// The id of a coordinator's or director's own files, where a thread's
    /// goes (thread.SeatID, app API 55).
    public static let seatID = "seat"

    /// Whether f is a coordinator's or director's own.
    public static func isSeat(_ f: AppAPI.LibraryFile) -> Bool { f.thread == seatID }

    /// A project's groups in a program's list: the director's own have no
    /// title, each member's its name and its paired server's.
    public struct Section: Identifiable, Sendable, Equatable {
        public let id: String
        public let project: String
        public let server: String
        public let groups: [Group]

        /// The heading over a member's files, "" for the director's own or a
        /// project's own list.
        public var title: String {
            server.isEmpty ? project : "\(project) on \(server)"
        }
    }

    public struct Group: Identifiable, Sendable, Equatable {
        /// The thread's id, or "@" and the linked project's.
        public let id: String
        public let files: [AppAPI.LibraryFile]

        /// "T12 · Fix the build", or "from web" for a linked project's.
        public var title: String {
            guard let f = files.first else { return id }
            if !f.peer.isEmpty { return "from \(f.peer)" }
            if LibraryPage.isSeat(f) { return f.title }
            return [f.task, f.title].filter { !$0.isEmpty }.joined(separator: " · ")
        }

        /// "t-0003 · archived" (its thread and state), or how many a
        /// linked project sent.
        public var detail: String {
            guard let f = files.first else { return "" }
            if !f.peer.isEmpty || LibraryPage.isSeat(f) { return files.count == 1 ? "1 file" : "\(files.count) files" }
            return f.state == "archived" ? "\(f.thread) · archived" : f.thread
        }
    }

    /// A file's group: its thread, or "@" and the linked project, in its
    /// project in a program's list.
    public static func key(_ f: AppAPI.LibraryFile) -> String {
        place(f) + (f.peer.isEmpty ? f.thread : "@" + f.peer)
    }

    /// What tells a member's file from another member's in a program's list,
    /// "" for the director's own or in a project's own list.
    public static func place(_ f: AppAPI.LibraryFile) -> String {
        f.project.isEmpty ? "" : f.server + "/" + f.project + "/"
    }

    public static func groups(_ files: [AppAPI.LibraryFile]) -> [Group] {
        sections(files).flatMap(\.groups)
    }

    /// The files by project, in the order the projects came, each project's
    /// groups as groups orders them.
    public static func sections(_ files: [AppAPI.LibraryFile]) -> [Section] {
        var places: [String] = []
        var byPlace: [String: [AppAPI.LibraryFile]] = [:]
        for f in files {
            let p = place(f)
            if byPlace[p] == nil { places.append(p) }
            byPlace[p, default: []].append(f)
        }
        return places.map { p in
            let inPlace = byPlace[p] ?? []
            var order: [String] = []
            var by: [String: [AppAPI.LibraryFile]] = [:]
            for f in inPlace.sorted(by: { $0.time > $1.time }) {
                let k = key(f)
                if by[k] == nil { order.append(k) }
                by[k, default: []].append(f)
            }
            let own = order.filter { by[$0]?.first.map(isSeat) == true }
            let rest = order.filter { by[$0]?.first.map(isSeat) != true }
            let first = inPlace.first
            return Section(id: p, project: first?.project ?? "", server: first?.server ?? "",
                           groups: (own + rest).map { Group(id: $0, files: by[$0] ?? []) })
        }
    }

    /// A file's kind, from its name's extension (T1054). The TUI's
    /// libKinds and libKindOf: internal/tui/testdata/library-filter.json
    /// pins both.
    public enum Kind: String, CaseIterable, Sendable {
        case images = "Images", pages = "Pages", notes = "Notes", data = "Data"
        case archives = "Archives", video = "Video", code = "Code", other = "Other"
    }

    private static let kindOf: [String: Kind] = {
        var out: [String: Kind] = [:]
        let lists: [(Kind, String)] = [
            (.images, "png jpg jpeg gif webp heic heif svg bmp tif tiff ico avif"),
            (.pages, "html htm pdf"),
            (.notes, "md markdown txt rtf"),
            (.data, "json jsonl ndjson csv tsv xml yaml yml toml sql sqlite db log"),
            (.archives, "zip tar gz tgz bz2 xz zst 7z rar dmg"),
            (.video, "mp4 mov m4v webm mkv avi"),
            (.code, "go swift py js jsx ts tsx sh zsh bash rb rs c h cc cpp hpp java kt php lua pl css diff patch"),
        ]
        for (kind, exts) in lists {
            for e in exts.split(separator: " ") { out[String(e)] = kind }
        }
        return out
    }()

    public static func kind(_ name: String) -> Kind {
        guard let dot = name.lastIndex(of: "."), dot != name.startIndex else { return .other }
        return kindOf[name[name.index(after: dot)...].lowercased()] ?? .other
    }

    /// A chip: a kind (nil for all) and how many files the filter leaves
    /// of it.
    public struct Chip: Identifiable, Sendable, Equatable {
        public let kind: Kind?
        public let count: Int
        public var id: String { kind?.rawValue ?? "All" }
        public var word: String { kind?.rawValue ?? "All" }
    }

    private static func words(_ query: String) -> [String] {
        query.lowercased().split(whereSeparator: \.isWhitespace).map(String.init)
    }

    /// Whether every word is in the file's name, task, title, thread,
    /// repository or linked project, or a program's member and its server.
    private static func matches(_ f: AppAPI.LibraryFile, _ words: [String]) -> Bool {
        if words.isEmpty { return true }
        let hay = [f.name, f.task, f.title, f.thread, f.repo, f.peer, f.project, f.server].joined(separator: "\n").lowercased()
        return words.allSatisfy(hay.contains)
    }

    /// The kind that holds: pick, unless the library has fewer than two
    /// kinds or none of it (say, its last file went).
    public static func picked(_ files: [AppAPI.LibraryFile], _ pick: Kind?) -> Kind? {
        guard let pick else { return nil }
        let kinds = Set(files.map { kind($0.name) })
        return kinds.contains(pick) && kinds.count >= 2 ? pick : nil
    }

    /// The files the filter and the kind leave, in their order.
    public static func shown(_ files: [AppAPI.LibraryFile], query: String, kind pick: Kind?) -> [AppAPI.LibraryFile] {
        let words = words(query)
        let pick = picked(files, pick)
        if words.isEmpty, pick == nil { return files }
        return files.filter { matches($0, words) && (pick == nil || kind($0.name) == pick) }
    }

    /// The chips: all, then the kinds the filter leaves files of, and the
    /// picked kind even with none. Under two kinds, none at all, unless
    /// one is picked, so it can be let go.
    public static func chips(_ files: [AppAPI.LibraryFile], query: String, kind pick: Kind?) -> [Chip] {
        let words = words(query)
        let pick = picked(files, pick)
        var count: [Kind: Int] = [:]
        var all = 0
        for f in files where matches(f, words) {
            count[kind(f.name), default: 0] += 1
            all += 1
        }
        let kinds = Kind.allCases.filter { (count[$0] ?? 0) > 0 || $0 == pick }
        if kinds.count < 2, pick == nil { return [] }
        return [Chip(kind: nil, count: all)] + kinds.map { Chip(kind: $0, count: count[$0] ?? 0) }
    }

    /// Whether the filter shows: from 7 files, and for as long as it has
    /// words.
    public static func showsFilter(_ files: [AppAPI.LibraryFile], query: String) -> Bool {
        files.count >= 7 || !query.isEmpty
    }

    /// The filter field's words: the repository only where a file names one.
    public static func filterPrompt(_ files: [AppAPI.LibraryFile]) -> String {
        if files.contains(where: { !$0.project.isEmpty }) { return "Filter by name, task, thread or project" }
        return files.contains { !$0.repo.isEmpty } ? "Filter by name, task, thread or repo" : "Filter by name, task or thread"
    }

    /// What library.read names a file by: its thread, else the message a
    /// linked project sent it with.
    public static func id(_ f: AppAPI.LibraryFile) -> String {
        f.thread.isEmpty ? f.message : f.thread
    }

    /// Whether the server makes a thumbnail of a file of this name: PNG,
    /// JPEG and GIF (internal/thumb Supported). Anything else stays a row
    /// and asks nothing, so a library with no images makes no call.
    public static func hasThumbnail(_ f: AppAPI.LibraryFile) -> Bool {
        let name = f.name.lowercased()
        return [".png", ".jpg", ".jpeg", ".gif"].contains { name.hasSuffix($0) }
    }

    /// The file's thumbnail, or nil for a plain row: a file that isn't an
    /// image, without asking; one that makes none, a server too old, a
    /// link down, whatever the answer was.
    public static func thumbnail(_ fetch: @Sendable (AppAPI.LibraryThumbnailParams) async throws -> AppAPI.LibraryThumbnail,
                                 project: String, file f: AppAPI.LibraryFile) async -> AppAPI.LibraryThumbnail? {
        guard hasThumbnail(f) else { return nil }
        return try? await fetch(AppAPI.LibraryThumbnailParams(project: project, id: id(f), name: f.name))
    }

    /// library.read, as the bridge answers it.
    public typealias Fetch = @Sendable (AppAPI.LibraryReadParams) async throws -> AppAPI.LibraryChunk

    /// The file's bytes, a chunk at a time; again from the start, once,
    /// when it changed between chunks.
    /// With program, a director's own file of that program.
    public static func read(_ fetch: Fetch, project: String, program: String? = nil, file f: AppAPI.LibraryFile) async -> FileOutcome {
        let ref = program.map { AppAPI.LibraryProgram(slug: $0) }
        let params = { (off: Int) in AppAPI.LibraryReadParams(project: project, program: ref, id: id(f), name: f.name, offset: off) }
        for _ in 0..<2 {
            var out = Data()
            var size = -1
            var mtime: Date?
            var changed = false
            do {
                repeat {
                    try Task.checkCancellation()
                    let c = try await fetch(params(out.count))
                    if size >= 0, c.size != size || c.mtime != mtime {
                        changed = true
                        break
                    }
                    (size, mtime) = (c.size, c.mtime)
                    if c.data.isEmpty { break }
                    out.append(c.data)
                } while out.count < size
            } catch {
                return FileProblem(error).outcome
            }
            if !changed {
                return out.count == size ? .data(out, mtime) : .gone
            }
        }
        return .unavailable("the file kept changing while it was read")
    }
}

/// The Overview page's lines, as the TUI's A › overview: the goal, the
/// repositories (with a checkout's note), the coordinator's agent and
/// state, the threads by agent and the tasks' counts. Settings stay the
/// TUI's.
public struct ProjectOverview: Equatable, Sendable {
    public struct Repo: Equatable, Sendable {
        public let path: String
        /// A checkout's warning ("behind origin by 3"), empty for none.
        public let note: String
    }

    public struct Agent: Equatable, Sendable {
        public let agent: String
        public let threads: [String]
    }

    public let goal: String
    public let repos: [Repo]
    /// A paired server's project: how many repositories it has, whose
    /// paths stay there (repos is empty then); nil for this server's.
    public let repoCount: Int?
    /// PROJECT.md's body, its standing instructions: a paired server's
    /// project's, "" for this server's (the TUI shows those).
    public let instructions: String
    /// The coordinator's agent and state ("claude · working"); empty
    /// when none runs.
    public let coordinator: String
    public let agents: [Agent]
    /// "2 need you · 3 in motion · 5 on deck · 9 done".
    public let tasks: String

    public init(_ p: AppAPI.Project, coordinator: AppAPI.Session?, needs: Int) {
        goal = p.goal.trimmingCharacters(in: .whitespacesAndNewlines)
        repos = p.repos.map { Repo(path: $0, note: p.checkouts[$0] ?? "") }
        repoCount = nil
        instructions = ""
        if let c = coordinator {
            self.coordinator = [c.agent.isEmpty ? "claude" : c.agent, c.state].filter { !$0.isEmpty }.joined(separator: " · ")
        } else {
            self.coordinator = ""
        }
        var by: [String: [String]] = [:]
        for t in p.threads {
            by[t.record.agent.isEmpty ? "claude" : t.record.agent, default: []].append(t.record.id)
        }
        agents = by.keys.sorted().map { Agent(agent: $0, threads: by[$0] ?? []) }
        let needWords = needs == 1 ? "1 needs you" : "\(needs) need you"
        tasks = "\(needWords) · \(p.counts["in_motion"] ?? 0) in motion · \(p.counts["on_deck"] ?? 0) on deck · \(p.counts["done"] ?? 0) done"
    }

    /// A paired server's project (T886): its pages, and from peer.list
    /// its coordinator and threads (project) and what waits on that
    /// server's user.
    public init(_ pages: AppAPI.PeerPages, project p: AppAPI.PeerProject) {
        goal = pages.goal.trimmingCharacters(in: .whitespacesAndNewlines)
        repos = []
        repoCount = pages.repos
        instructions = pages.body.trimmingCharacters(in: .whitespacesAndNewlines)
        if let c = p.sessions.first(where: { $0.role == "coordinator" }) {
            coordinator = [c.agent.isEmpty ? "claude" : c.agent, c.state].filter { !$0.isEmpty }.joined(separator: " · ")
        } else {
            coordinator = ""
        }
        var by: [String: [String]] = [:]
        for t in p.sessions where t.role == "thread" {
            by[t.agent.isEmpty ? "claude" : t.agent, default: []].append(t.thread)
        }
        agents = by.keys.sorted().map { Agent(agent: $0, threads: by[$0] ?? []) }
        let needs = p.needsYou
        let needWords = needs == 1 ? "1 needs you" : "\(needs) need you"
        let motion = pages.tasks.filter { PeerPages.inMotion.contains($0.status) }.count
        tasks = "\(needWords) · \(motion) in motion · \(PeerPages.onDeck(pages).count) on deck · \(PeerPages.done(pages).count) done"
    }
}

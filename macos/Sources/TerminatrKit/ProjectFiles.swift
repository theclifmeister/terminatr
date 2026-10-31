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
/// them: by thread, or by the linked project that sent them, the group
/// with the newest file first and each group's files newest first.
public enum LibraryPage {
    public struct Group: Identifiable, Sendable, Equatable {
        /// The thread's id, or "@" and the linked project's.
        public let id: String
        public let files: [AppAPI.LibraryFile]

        /// "T12 · Fix the build", or "from web" for a linked project's.
        public var title: String {
            guard let f = files.first else { return id }
            if !f.peer.isEmpty { return "from \(f.peer)" }
            return [f.task, f.title].filter { !$0.isEmpty }.joined(separator: " · ")
        }

        /// "t-0003 · archived" (its thread and state), or how many a
        /// linked project sent.
        public var detail: String {
            guard let f = files.first else { return "" }
            if !f.peer.isEmpty { return files.count == 1 ? "1 file" : "\(files.count) files" }
            return f.state == "archived" ? "\(f.thread) · archived" : f.thread
        }
    }

    /// A file's group: its thread, or "@" and the linked project.
    public static func key(_ f: AppAPI.LibraryFile) -> String {
        f.peer.isEmpty ? f.thread : "@" + f.peer
    }

    public static func groups(_ files: [AppAPI.LibraryFile]) -> [Group] {
        var order: [String] = []
        var by: [String: [AppAPI.LibraryFile]] = [:]
        for f in files.sorted(by: { $0.time > $1.time }) {
            let k = key(f)
            if by[k] == nil { order.append(k) }
            by[k, default: []].append(f)
        }
        return order.map { Group(id: $0, files: by[$0] ?? []) }
    }

    /// What library.read names a file by: its thread, else the message a
    /// linked project sent it with.
    public static func id(_ f: AppAPI.LibraryFile) -> String {
        f.thread.isEmpty ? f.message : f.thread
    }

    /// library.read, as the bridge answers it.
    public typealias Fetch = @Sendable (AppAPI.LibraryReadParams) async throws -> AppAPI.LibraryChunk

    /// The file's bytes, a chunk at a time; again from the start, once,
    /// when it changed between chunks.
    public static func read(_ fetch: Fetch, project: String, file f: AppAPI.LibraryFile) async -> FileOutcome {
        let params = { (off: Int) in AppAPI.LibraryReadParams(project: project, id: id(f), name: f.name, offset: off) }
        for _ in 0..<2 {
            var out = Data()
            var size = -1
            var mtime: Date?
            var changed = false
            do {
                repeat {
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

import Foundation

/// The right panel's two views (the file browser design): one panel, one
/// saved width. Files and Inspector switch it; the lit one's button hides
/// it.
public enum RightPanel: String, Sendable, CaseIterable {
    case files
    case inspector

    /// What the panel is after view's button is pressed.
    public static func pressed(_ view: RightPanel, now: RightPanel?) -> RightPanel? {
        now == view ? nil : view
    }

    /// What it shows: what was left, but Files gives way to the Inspector
    /// where there are no files (a director, a program).
    public static func shown(_ wanted: RightPanel?, hasFiles: Bool) -> RightPanel? {
        wanted == .files && !hasFiles ? .inspector : wanted
    }

    /// What the saved choice says. The key before Files had the Inspector
    /// on or off.
    public static func saved(_ word: String?, inspector: Bool?) -> RightPanel? {
        if let word { return RightPanel(rawValue: word) }
        return inspector ?? true ? .inspector : nil
    }
}

/// What Files can browse for what the window shows.
public struct FilesPlace: Hashable, Sendable {
    public let project: String
    /// The paired server whose project it is, "" for this server's.
    public let server: String
    /// One thread's attached files; nil for the whole project's.
    public let thread: String?
    /// The session whose folder the Worktree tab lists, nil when none
    /// answers: why says why not.
    public let session: String?
    /// Why the Worktree tab has nothing, when it can't.
    public let why: String?
    /// What it says when the server answers that the session it had in
    /// hand has stopped.
    public let stopped: String
    /// The name in the panel's header: a thread's task and id, or nothing
    /// beside a project.
    public let label: String
    /// The session whose composer Mention writes in.
    public let mentions: SessionRef?
    /// Whether a mentioned path is relative: the thread's agent works in
    /// its worktree, a coordinator elsewhere.
    public let relativeMention: Bool

    /// What the selection may browse, or nil when Files stays off: a
    /// program or a director, which have no repo, nothing selected, or a
    /// tm without a Library.
    public static func of(_ selection: Selection?, sessionID: String?, threadDone: Bool, task: String? = nil, readsWorktree: Bool, hasLibrary: Bool) -> FilesPlace? {
        guard let selection, hasLibrary else { return nil }
        let project = selection.project, server = selection.server
        switch selection {
        case .program:
            return nil
        case .session(let ref) where ref.director:
            return nil
        case .project, .session:
            break
        }
        var thread: String?
        var label = ""
        if case .session(let ref) = selection, !ref.isCoordinator {
            thread = ref.thread
            label = [task ?? "", ref.thread].filter { !$0.isEmpty }.joined(separator: " ")
        }
        let mentions: SessionRef? = server.isEmpty ? SessionRef(project: project, thread: thread ?? "") : nil
        let whole = thread == nil
        let keeps = whole ? "What threads attached stays here." : "What it attached stays here."
        let stopped = whole ? "The coordinator isn't running, so tm can't read the project's repo. \(keeps)"
            : threadDone ? "This thread is done, so its worktree is gone. \(keeps)"
            : "This thread's agent isn't running, so tm can't read its worktree. \(keeps)"
        var why: String?
        if !server.isEmpty {
            why = "\(server)'s worktrees can't be browsed from here. The Attached tab shows what \(whole ? "threads" : "this thread") attached, when \(server) shares the project's pages."
        } else if !readsWorktree {
            why = "This tm can't read a worktree: update it. \(keeps)"
        } else if sessionID == nil {
            why = stopped
        }
        return FilesPlace(project: project, server: server, thread: thread, session: why == nil ? sessionID : nil, why: why, stopped: stopped,
                          label: label, mentions: mentions, relativeMention: !whole)
    }
}

/// A row of the Worktree tab's tree.
public struct FileRow: Hashable, Sendable, Identifiable {
    public let entry: AppAPI.FileEntry
    public let depth: Int

    public var id: String { entry.path }
    public var isFolder: Bool { entry.kind == "dir" }

    public init(entry: AppAPI.FileEntry, depth: Int) {
        self.entry = entry
        self.depth = depth
    }
}

/// The Worktree tab's tree, as the TUI's Files view walks it: the folders
/// as last listed and the unfolded ones.
public enum FileTree {
    /// How many folders one stamp looks at (proto.MaxFilesStamped).
    public static let maxStamped = 256

    public static func rows(dirs: [String: [AppAPI.FileEntry]], open: Set<String>) -> [FileRow] {
        var out: [FileRow] = []
        func walk(_ dir: String, _ depth: Int) {
            for e in dirs[dir] ?? [] {
                out.append(FileRow(entry: e, depth: depth))
                if open.contains(e.path) { walk(e.path, depth + 1) }
            }
        }
        walk("", 0)
        return out
    }

    /// Files.list's changed result as folders, each listed as a folder is:
    /// folders first, by name, as changedTree in the TUI. The folders come
    /// from the paths, not the disk, so a deleted file sits under a folder
    /// that's gone. A row the result has itself keeps its own marks: it
    /// comes before what's under it. A key is a folder with rows under it.
    public static func changedDirs(_ found: [AppAPI.FileEntry]) -> [String: [AppAPI.FileEntry]] {
        var tree: [String: [AppAPI.FileEntry]] = [:]
        var seen: Set<String> = []
        func add(_ dir: String, _ e: AppAPI.FileEntry) {
            if seen.insert(e.path).inserted { tree[dir, default: []].append(e) }
        }
        for e in found {
            let names = e.path.split(separator: "/", omittingEmptySubsequences: false).map(String.init)
            var dir = ""
            for name in names.dropLast() {
                let p = dir.isEmpty ? name : dir + "/" + name
                add(dir, AppAPI.FileEntry(name: name, path: p, kind: "dir", changed: true))
                dir = p
            }
            add(dir, e)
        }
        return tree.mapValues { list in
            list.sorted { a, b in
                if (a.kind == "dir") != (b.kind == "dir") { return a.kind == "dir" }
                return Array(a.name.lowercased().utf8).lexicographicallyPrecedes(Array(b.name.lowercased().utf8))
            }
        }
    }

    /// The folders to list: the top first, then the unfolded ones that
    /// show, then one just unfolded and not listed yet.
    public static func folders(dirs: [String: [AppAPI.FileEntry]], open: Set<String>) -> [String] {
        var out = [""]
        for r in rows(dirs: dirs, open: open) where r.isFolder && open.contains(r.id) && out.count < maxStamped {
            out.append(r.id)
        }
        for d in open.sorted() where !out.contains(d) && out.count < maxStamped {
            out.append(d)
        }
        return out
    }

    /// The folder a path is in, "" at the top.
    public static func parent(_ path: String) -> String {
        guard let slash = path.lastIndex(of: "/") else { return "" }
        return String(path[..<slash])
    }

    /// What ← or → does on a row.
    public enum Fold: Equatable, Sendable {
        case open(String)
        case close(String)
        case pick(String)
        case nothing
    }

    /// → unfolds a folder, then steps into it; ← folds it, then goes up to
    /// its parent, from a file too.
    public static func fold(_ rows: [FileRow], at path: String?, right: Bool, open: Set<String>) -> Fold {
        guard let path, let i = rows.firstIndex(where: { $0.id == path }) else { return .nothing }
        let r = rows[i]
        if right {
            guard r.isFolder else { return .nothing }
            if !open.contains(path) { return .open(path) }
            return i + 1 < rows.count && rows[i + 1].depth > r.depth ? .pick(rows[i + 1].id) : .nothing
        }
        if r.isFolder, open.contains(path) { return .close(path) }
        let up = parent(path)
        return up.isEmpty || !rows.contains(where: { $0.id == up }) ? .nothing : .pick(up)
    }
}

/// A file shown from either tab: the id the preview and a pane's tab know
/// it by.
public enum FileSource: Hashable, Sendable {
    case worktree(AppAPI.FileEntry, ignored: Bool)
    case attached(AppAPI.LibraryFile)
}

public struct OpenedFile: Hashable, Sendable, Identifiable {
    public let source: FileSource
    /// The repo a coordinator's worktree file is in, nil for the first: a
    /// tab stays on its own repo when the panel moves to another.
    public let repo: AppAPI.FilesRepoRef?

    public init(_ source: FileSource, repo: AppAPI.FilesRepoRef? = nil) {
        self.source = source
        self.repo = repo
    }

    public var id: String {
        switch source {
        case .worktree(let e, _): "w:" + (repo.map { $0.path + ":" } ?? "") + e.path
        case .attached(let f): "a:" + LibraryPage.id(f) + "/" + f.name
        }
    }

    /// The last part of its name.
    public var name: String {
        switch source {
        case .worktree(let e, _): e.name
        case .attached(let f): (f.name as NSString).lastPathComponent
        }
    }

    /// What the preview's head says: a worktree file's path.
    public var path: String {
        switch source {
        case .worktree(let e, _): e.path
        case .attached(let f): f.name
        }
    }

    public var size: Int {
        switch source {
        case .worktree(let e, _): e.size
        case .attached(let f): f.size
        }
    }

    /// What may hold a secret says so while its text shows.
    public var caution: String? {
        switch source {
        case .worktree(let e, _): FileCaution.of(name: e.name, ignored: e.ignored)
        case .attached(let f): FileCaution.of(name: f.name, ignored: false)
        }
    }
}

/// The file tabs Open in pane adds to the main pane, beside what the title
/// row has of its own: as many as fit a title row, the oldest going.
public struct PaneFiles: Hashable, Sendable {
    public static let limit = 6
    public private(set) var tabs: [OpenedFile] = []
    /// The tab the pane shows, nil for the session's or project's own.
    public var shown: String?

    public init() {}

    public var current: OpenedFile? { tabs.first { $0.id == shown } }

    public mutating func open(_ f: OpenedFile) {
        if !tabs.contains(where: { $0.id == f.id }) {
            tabs.append(f)
            if tabs.count > Self.limit { tabs.removeFirst(tabs.count - Self.limit) }
        }
        shown = f.id
    }

    /// Closes a tab: the one after it shows when it was showing, else the
    /// one before, else the pane's own.
    public mutating func close(_ id: String) {
        guard let i = tabs.firstIndex(where: { $0.id == id }) else { return }
        tabs.remove(at: i)
        if shown == id {
            shown = tabs.indices.contains(i) ? tabs[i].id : tabs.last?.id
        }
    }
}

/// What the viewer warns of: the same words and rule as the TUI's viewer
/// (fileCaution in internal/tui/filespanel.go). A hint for the eye, not a
/// rule: nothing is refused by it.
public enum FileCaution {
    public static func of(name: String, ignored: Bool) -> String? {
        if ignored { return "Git-ignored: may hold secrets" }
        return secretName(name) ? "Its name says it may hold secrets" : nil
    }

    public static func secretName(_ name: String) -> Bool {
        let n = name.lowercased()
        if n == ".env" || n.hasPrefix(".env.") {
            return ![".example", ".sample", ".template", ".dist"].contains { n.hasSuffix($0) }
        }
        if [".netrc", ".npmrc", ".pypirc", ".git-credentials", "credentials", "credentials.json",
            "id_rsa", "id_dsa", "id_ecdsa", "id_ed25519"].contains(n) {
            return true
        }
        if [".pem", ".key", ".p12", ".pfx", ".keystore", ".jks", ".kdbx"].contains(where: { n.hasSuffix($0) }) {
            return true
        }
        return n.hasPrefix("secrets.") || n.hasPrefix("secret.")
    }

    /// Whether Open would run the file rather than show it: the system
    /// launches these from a double click, and a thread's folder is
    /// whatever its agent wrote. Quick Look and Save As stay.
    public static func launches(_ name: String) -> Bool {
        let ext = (name.lowercased() as NSString).pathExtension
        return launching.contains(ext)
    }

    private static let launching: Set<String> = [
        "app", "command", "tool", "terminal", "sh", "bash", "zsh", "csh", "ksh", "py", "rb", "pl", "php",
        "scpt", "scptd", "applescript", "workflow", "action", "shortcut", "osascript",
        "pkg", "mpkg", "dmg", "jar", "jnlp", "webloc", "inetloc", "fileloc", "url", "mobileconfig", "prefpane", "qlgenerator",
    ]
}

/// A file's bytes as the preview and a pane's tab show them.
public enum FileText {
    /// How much of a file shows, as the TUI's viewer.
    public static let cap = 1 << 20

    /// As internal/filetree's IsBinary: a NUL, or not UTF-8 but for a
    /// character cut at the end, in its first 8 KiB.
    public static func isBinary(_ d: Data) -> Bool {
        let head = Array(d.prefix(8 << 10))
        if head.contains(0) { return true }
        return String(bytes: dropCutEnd(head), encoding: .utf8) == nil
    }

    /// Without a multi-byte character the end cut in two.
    private static func dropCutEnd(_ bytes: [UInt8]) -> ArraySlice<UInt8> {
        var end = bytes.count
        var back = 0
        while end > 0, back < 4, bytes[end - 1] & 0xC0 == 0x80 {
            end -= 1
            back += 1
        }
        guard end > 0 else { return bytes[...] }
        let lead = bytes[end - 1]
        let wants = lead >= 0xF0 ? 4 : lead >= 0xE0 ? 3 : lead >= 0xC0 ? 2 : 1
        return wants > back + 1 ? bytes[..<(end - 1)] : bytes[...]
    }

    /// The text to draw. Whatever a folder holds is data: control and
    /// direction characters, which could make it read as something it
    /// isn't, don't reach the screen. Tabs and line ends stay.
    public static func text(_ d: Data) -> String {
        let s = String(decoding: dropCutEnd(Array(d)), as: UTF8.self)
            .replacingOccurrences(of: "\r\n", with: "\n")
        var out = String.UnicodeScalarView()
        out.reserveCapacity(s.unicodeScalars.count)
        for u in s.unicodeScalars {
            switch u.value {
            case 0x09, 0x0A: out.append(u)
            case 0x00...0x1F, 0x7F...0x9F, 0x200E, 0x200F, 0x202A...0x202E, 0x2066...0x2069: continue
            default: out.append(u)
            }
        }
        return String(out)
    }

    /// The line saying a file was cut, nil when all of it shows.
    public static func cutNote(size: Int, shown: Int) -> String? {
        size > shown ? "Shows the first \(Attachment.size(shown)) of \(Attachment.size(size))." : nil
    }

    /// Each line's start, in UTF-16 offsets, for the line numbers.
    public static func lineStarts(_ s: String) -> [Int] {
        var out = [0]
        var at = 0
        for u in s.utf16 {
            at += 1
            if u == 0x0A { out.append(at) }
        }
        return out
    }

    /// The number of the line at UTF-16 offset, 1 up.
    public static func line(at offset: Int, in starts: [Int]) -> Int {
        var lo = 0, hi = starts.count
        while lo < hi {
            let mid = (lo + hi) / 2
            if starts[mid] <= offset { lo = mid + 1 } else { hi = mid }
        }
        return max(lo, 1)
    }
}

/// A file's text with its changes laid over it (files.diff): its lines in
/// order, the removed ones where they went. The TUI's viewer does the same
/// (mergeDiff in internal/tui/filesdiff.go).
public struct FileMarks: Equatable, Sendable {
    public enum Kind: UInt8, Sendable {
        case same
        case added
        case removed
    }

    /// What shows, one line a row.
    public let text: String
    public let kinds: [Kind]
    /// Each row's number: the file's own, a removed line's in the base.
    public let numbers: [Int]
    /// The row the first change is on.
    public var firstChange: Int? { kinds.firstIndex { $0 != .same } }

    /// The marks of a file's text, nil when nothing shows as changed: no
    /// change, or one with no lines to mark. A hunk past the text's end (it
    /// changed since it was read) marks what there is.
    public static func of(_ text: String, _ diff: AppAPI.FilesDiff) -> FileMarks? {
        guard diff.status == "A" || !diff.hunks.isEmpty, !diff.binary, !diff.tooBig else { return nil }
        var file = text.split(separator: "\n", omittingEmptySubsequences: false)
        if file.last?.isEmpty == true { file.removeLast() }
        var rows: [Substring] = []
        var kinds: [Kind] = []
        var numbers: [Int] = []
        rows.reserveCapacity(file.count)
        var at = 0
        func take(to end: Int, as kind: Kind) {
            while at < min(end, file.count) {
                rows.append(file[at])
                kinds.append(kind)
                at += 1
                numbers.append(at)
            }
        }
        if diff.status == "A" {
            take(to: file.count, as: .added)
        } else {
            for h in diff.hunks {
                take(to: h.new - 1, as: .same)
                for (i, line) in h.removed.enumerated() {
                    rows.append(Substring(line))
                    kinds.append(.removed)
                    numbers.append(h.old + i)
                }
                take(to: h.new - 1 + h.added, as: .added)
            }
            take(to: file.count, as: .same)
        }
        guard !rows.isEmpty else { return nil }
        return FileMarks(text: rows.joined(separator: "\n"), kinds: kinds, numbers: numbers)
    }
}

/// What the line above a changed file's text says.
public enum FileChange {
    public struct Words: Equatable, Sendable {
        /// What's first: New, Deleted, Renamed from a path, or nil.
        public let lead: String?
        public let added: Int?
        public let deleted: Int?
        /// Since which ref, nil when it has no name.
        public let since: String?
    }

    /// Nil for a file with no change.
    public static func words(_ d: AppAPI.FilesDiff) -> Words? {
        var lead: String?
        var added: Int? = d.added > 0 ? d.added : nil
        var deleted: Int? = d.deleted > 0 ? d.deleted : nil
        switch d.status {
        case "A":
            lead = "New"
            deleted = nil
        case "D":
            lead = "Deleted"
            added = nil
        case "M":
            if !d.from.isEmpty {
                lead = "Renamed from \(d.from)"
            } else if added == nil, deleted == nil {
                lead = "Changed"
            }
        default:
            return nil
        }
        return Words(lead: lead, added: added, deleted: deleted, since: d.base.isEmpty ? nil : "since \(d.base)")
    }

    /// What a deleted file with no lines to show says instead.
    public static func deleted(_ name: String, _ d: AppAPI.FilesDiff) -> String? {
        if d.binary { return "\(name) · \(Attachment.size(d.size)) is deleted in this worktree. It wasn't text, so nothing shows." }
        if d.tooBig { return "\(name) · \(Attachment.size(d.size)) is deleted in this worktree, and it's too big to show what it held." }
        return nil
    }
}

/// What a worktree file's read came to.
public enum WorktreeRead {
    public typealias Fetch = @Sendable (AppAPI.FilesReadParams) async throws -> AppAPI.FilesChunk

    /// A read's offset past any file's end: only its size and time answer.
    public static let statOffset = 1 << 40

    /// What the preview shows: up to FileText.cap of a text file.
    public enum Body: Equatable, Sendable {
        case text(String, size: Int, shown: Int, mtime: Date?)
        case binary(size: Int, mtime: Date?)
        case gone
        case unavailable(String)
    }

    public static func body(_ fetch: Fetch, session: String, repo: AppAPI.FilesRepoRef? = nil, path: String, ignored: Bool) async -> Body {
        var out = Data()
        var size = 0
        var mtime: Date?
        do {
            repeat {
                let c = try await fetch(AppAPI.FilesReadParams(session: session, repo: repo, path: path, offset: out.count, ignored: ignored))
                (size, mtime) = (c.size, c.mtime)
                if c.binary { return .binary(size: c.size, mtime: c.mtime) }
                if c.data.isEmpty { break }
                out.append(c.data)
            } while out.count < min(size, FileText.cap)
        } catch {
            return problem(error)
        }
        let shown = out.prefix(FileText.cap)
        return .text(FileText.text(shown), size: size, shown: shown.count, mtime: mtime)
    }

    /// The whole file's bytes, for Quick Look, Open and Save As; again
    /// from the start, once, when it changed between chunks. Over what a
    /// read gives, it says so: a part of it would open as a broken file.
    public static func bytes(_ fetch: Fetch, session: String, repo: AppAPI.FilesRepoRef? = nil, path: String, ignored: Bool) async -> FileOutcome {
        for _ in 0..<2 {
            var out = Data()
            var size = -1
            var mtime: Date?
            var changed = false
            do {
                repeat {
                    try Task.checkCancellation()
                    let c = try await fetch(AppAPI.FilesReadParams(session: session, repo: repo, path: path, offset: out.count, ignored: ignored, binary: true))
                    if c.truncated { return .unavailable("It's over the \(Attachment.size(c.size)) tm reads. Reveal in Finder shows it.") }
                    if c.binary, c.data.isEmpty, c.size > 0 { return .unavailable("This tm doesn't send a binary file's bytes: update it.") }
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

    /// Size and time alone, for a shown file's refresh.
    public static func stat(_ fetch: Fetch, session: String, repo: AppAPI.FilesRepoRef? = nil, path: String, ignored: Bool) async -> Body {
        do {
            let c = try await fetch(AppAPI.FilesReadParams(session: session, repo: repo, path: path, offset: statOffset, ignored: ignored))
            return .binary(size: c.size, mtime: c.mtime)
        } catch {
            return problem(error)
        }
    }

    /// An attached file's bytes as a body.
    public static func body(attached outcome: FileOutcome) -> Body {
        switch outcome {
        case .gone: return .gone
        case .unavailable(let why): return .unavailable(why)
        case .data(let d, let mtime):
            if FileText.isBinary(d) { return .binary(size: d.count, mtime: mtime) }
            let shown = d.prefix(FileText.cap)
            return .text(FileText.text(shown), size: d.count, shown: shown.count, mtime: mtime)
        }
    }

    private static func problem(_ error: any Error) -> Body {
        switch FileProblem(error) {
        case .gone: .gone
        case .unavailable(let why): .unavailable(why)
        }
    }
}

/// What Mention in the message puts in the composer.
public enum FileMention {
    public static func text(_ source: FileSource, root: String, relative: Bool) -> String {
        switch source {
        case .worktree(let e, _):
            return relative || root.isEmpty ? e.path : root + "/" + e.path
        case .attached(let f):
            return f.thread.isEmpty ? f.name : "\(f.name) (attached by \(f.thread))"
        }
    }
}

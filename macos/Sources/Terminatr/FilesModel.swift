import Foundation
import Observation
import TerminatrKit

/// What the right panel's Files view knows of one selection: the tab, the
/// filter, the tree and the files open from it, and the file tabs Open in
/// pane added to the main pane. The app keeps it while the selection is
/// shown and for a few after (AppModel.filesModel). Everything comes through
/// the server's read-only files methods and the Library's: this never reads
/// a worktree path itself.
@MainActor
@Observable
final class FilesModel {
    enum Scope: String, CaseIterable, Identifiable {
        case worktree = "Worktree"
        case attached = "Attached"
        var id: Self { self }
    }

    /// A file's text as last read.
    struct Shown: Equatable {
        var body: WorktreeRead.Body?
        /// It was there when read, and isn't now: what shows is old.
        var deleted = false
        /// Its change since the base, nil from a tm that can't say or for
        /// an attached file.
        var diff: AppAPI.FilesDiff?
        /// Its lines with that change laid over them, when there are any.
        var marks: FileMarks?
    }

    private(set) var scope = Scope.worktree
    var filter = ""
    private(set) var changedOnly = false
    private(set) var showIgnored = false
    private(set) var open: Set<String> = []
    private(set) var dirs: [String: [AppAPI.FileEntry]] = [:]
    private(set) var found: [AppAPI.FileEntry] = []
    /// The top's answer: its root and totals.
    private(set) var summary: AppAPI.FilesList?
    private(set) var cut = false
    /// Why the Worktree tab has nothing, the server's words or the place's.
    private(set) var problem: String?
    private(set) var loaded = false
    private(set) var library: [AppAPI.LibraryFile]?
    /// A coordinator's repos, the first the default; none for a thread, or
    /// a tm without the list.
    private(set) var repos: [AppAPI.FilesRepo] = []
    /// The repo chosen, nil for the first: what the server is told.
    private(set) var chosen: AppAPI.FilesRepoRef?
    private(set) var libraryProblem: String?
    /// The row the arrows are on: a worktree path, or an attached file's id.
    private(set) var cursor: String?
    /// The file previewing under the tree.
    private(set) var picked: OpenedFile?
    private(set) var shown: [String: Shown] = [:]
    private(set) var busy = false
    var actionProblem: String?
    /// The tree takes the keys once: Files was just asked for. Moving
    /// through the sidebar with it showing leaves the keys there.
    var wantsFocus = false
    /// The file tabs Open in pane added.
    var pane = PaneFiles()
    var place: FilesPlace?

    @ObservationIgnored private var stamp = ""
    @ObservationIgnored private var generation = 0
    @ObservationIgnored private var opened = false
    /// It opened on Attached for want of a folder, not by the user's pick:
    /// a session that turns up (the list comes after the window, at launch)
    /// brings it back to Worktree.
    @ObservationIgnored private var fellBack = false
    @ObservationIgnored private var beats = 0
    /// How often the panel asks whether what it shows changed, as the TUI.
    private static let every = Duration.seconds(2)

    var searching: Bool { !filter.isEmpty || changedOnly }

    /// The repo the Worktree tab shows, to name it.
    var repo: AppAPI.FilesRepo? {
        repos.first { $0.path == chosen?.path } ?? repos.first
    }

    var rows: [FileRow] {
        searching ? found.map { FileRow(entry: $0, depth: 0) } : FileTree.rows(dirs: dirs, open: open)
    }

    var attached: [AppAPI.LibraryFile] {
        let all = library ?? []
        let q = filter.lowercased()
        return q.isEmpty ? all : all.filter { $0.name.lowercased().contains(q) }
    }

    /// The rows the arrows walk, top to bottom.
    var order: [String] {
        scope == .worktree
            ? rows.map(\.id)
            : LibraryPage.groups(attached).flatMap { $0.files.map { OpenedFile(.attached($0)).id } }
    }

    func askFocus() { wantsFocus = true }

    // MARK: the list

    func pick(scope s: Scope, _ app: AppModel) {
        guard s != scope else { return }
        fellBack = false
        scope = s
        cursor = nil
        picked = nil
        Task { await refresh(app) }
    }

    /// Shows another of the coordinator's repos: nothing of the last tree
    /// stays. A tab open in the pane stays on its own repo.
    func choose(repo r: AppAPI.FilesRepo, _ app: AppModel) {
        guard r.path != repo?.path else { return }
        chosen = r.path == repos.first?.path ? nil : AppAPI.FilesRepoRef(path: r.path)
        resetTree()
        Task { await refresh(app) }
    }

    private func resetTree() {
        stamp = ""
        open = []
        dirs = [:]
        found = []
        summary = nil
        cut = false
        problem = nil
        loaded = false
        cursor = nil
        picked = nil
    }

    func setChangedOnly(_ on: Bool, _ app: AppModel) {
        changedOnly = on
        cursor = nil
        Task { await refresh(app) }
    }

    func setShowIgnored(_ on: Bool, _ app: AppModel) {
        showIgnored = on
        Task { await refresh(app) }
    }

    func toggle(folder path: String, _ app: AppModel) {
        if open.contains(path) {
            open.remove(path)
        } else {
            open.insert(path)
            Task { await refresh(app) }
        }
    }

    /// The arrows, or a click: the row, its file previewing.
    func moveCursor(to id: String) {
        cursor = id
        if scope == .worktree {
            guard let r = rows.first(where: { $0.id == id }) else { return }
            picked = r.isFolder ? nil : file(r.entry)
        } else {
            picked = (library ?? []).map { OpenedFile(.attached($0)) }.first { $0.id == id }
        }
    }

    func file(_ e: AppAPI.FileEntry) -> OpenedFile {
        OpenedFile(.worktree(e, ignored: showIgnored || e.ignored), repo: chosen)
    }

    /// ← and →.
    func fold(right: Bool, _ app: AppModel) {
        guard scope == .worktree, !searching else { return }
        switch FileTree.fold(rows, at: cursor, right: right, open: open) {
        case .open(let p), .close(let p): toggle(folder: p, app)
        case .pick(let p): moveCursor(to: p)
        case .nothing: break
        }
    }

    // MARK: what the server says

    /// While the panel shows: a first listing, then a look every couple of
    /// seconds at whether what it shows changed.
    func watch(_ app: AppModel) async {
        await refresh(app)
        while !Task.isCancelled {
            try? await Task.sleep(for: Self.every)
            if Task.isCancelled { return }
            beats += 1
            if scope == .attached {
                // The Library's, a few stats: less often than a stamp.
                if beats % 5 == 0 { await loadLibrary(app) }
            } else if let session = place?.session, let b = app.bridge {
                // The project's list moves without the folder's stamp moving.
                await loadRepos(session, b)
                let folders = searching ? [""] : FileTree.folders(dirs: dirs, open: open)
                let now = try? await b.call(AppAPI.Methods.filesStamp, AppAPI.FilesStampParams(session: session, repo: chosen, folders: folders))
                if now?.stamp != stamp { await refresh(app) }
            }
            if let picked { await reread(picked, app) }
        }
    }

    func refresh(_ app: AppModel) async {
        guard place != nil else { return }
        generation += 1
        let g = generation
        await loadLibrary(app)
        guard g == generation else { return }
        guard let session = place?.session, let b = app.bridge else {
            noFolder(place?.why ?? "Not connected to terminatr.")
            return
        }
        if fellBack {
            fellBack = false
            scope = .worktree
        }
        await loadRepos(session, b)
        guard g == generation else { return }
        let wanted = searching ? [""] : FileTree.folders(dirs: dirs, open: open)
        let stamped = try? await b.call(AppAPI.Methods.filesStamp, AppAPI.FilesStampParams(session: session, repo: chosen, folders: wanted))
        stamp = stamped?.stamp ?? stamp
        guard g == generation else { return }
        do {
            if searching {
                let r = try await b.call(AppAPI.Methods.filesList, AppAPI.FilesListParams(session: session, repo: chosen, ignored: showIgnored, query: filter, changed: changedOnly))
                guard g == generation else { return }
                summary = r
                found = r.entries
                cut = r.truncated
            } else {
                var out: [String: [AppAPI.FileEntry]] = [:]
                var top: AppAPI.FilesList?
                var over = false
                for (i, d) in wanted.enumerated() {
                    do {
                        let r = try await b.call(AppAPI.Methods.filesList, AppAPI.FilesListParams(session: session, repo: chosen, path: d, ignored: showIgnored))
                        if i == 0 { top = r }
                        out[d] = r.entries
                        over = over || r.truncated
                    } catch where i > 0 {
                        continue // gone since: it drops off the tree
                    }
                }
                guard g == generation else { return }
                dirs = out
                open.formIntersection(out.keys)
                summary = top
                found = []
                cut = over
            }
            problem = nil
            loaded = true
            opened = true
        } catch {
            guard g == generation else { return }
            let e = error as? AppAPI.Error
            if e?.code == "unknown-session" {
                noFolder(place?.stopped ?? "")
            } else if e?.code == "no-files" {
                noFolder(e?.errorDescription ?? error.localizedDescription)
            } else {
                problem = e?.errorDescription ?? error.localizedDescription
                dirs = [:]
                found = []
                loaded = true
            }
        }
    }

    /// The coordinator's repos. A tm without the list leaves none, and the
    /// first repo unnamed. A repo chosen that left the project's list goes
    /// back to the first.
    private func loadRepos(_ session: String, _ b: Bridge) async {
        guard place?.thread == nil,
              let r = try? await b.call(AppAPI.Methods.filesRepos, AppAPI.FilesReposParams(session: session)) else { return }
        if repos != r.repos { repos = r.repos }
        if let c = chosen, !r.repos.contains(where: { $0.path == c.path }) {
            chosen = nil
            resetTree()
        }
    }

    /// With no folder at all the panel opens on Attached, and Worktree
    /// says why.
    private func noFolder(_ why: String) {
        problem = why
        dirs = [:]
        found = []
        summary = nil
        cut = false
        loaded = true
        if !opened, chosen == nil {
            opened = true
            fellBack = true
            scope = .attached
        }
    }

    private func loadLibrary(_ app: AppModel) async {
        guard let place else { return }
        do {
            let all = place.server.isEmpty ? try await app.library(place.project) : try await app.peerLibrary(place.server, place.project)
            library = place.thread.map { t in all.filter { $0.thread == t } } ?? all
            libraryProblem = nil
        } catch {
            libraryProblem = error.localizedDescription
            if library == nil { library = [] }
        }
    }

    // MARK: a file's text

    /// Reads f for the preview or a pane's tab, once.
    func load(_ f: OpenedFile, _ app: AppModel) async {
        guard shown[f.id] == nil else { return }
        await read(f, app)
    }

    private func read(_ f: OpenedFile, _ app: AppModel) async {
        switch f.source {
        case .worktree(let e, let ignored):
            if e.linkOut || e.kind == "other" {
                shown[f.id] = Shown(body: .unavailable("\(e.name) is a link that doesn't open: it leads out of the worktree, or nowhere."))
            } else if let session = place?.session, let b = app.bridge {
                shown[f.id] = await worktreeFile(e, ignored: ignored, repo: f.repo, session: session, b)
            } else {
                shown[f.id] = Shown(body: .unavailable(place?.why ?? "Not connected to terminatr."))
            }
        case .attached(let a):
            shown[f.id] = Shown(body: WorktreeRead.body(attached: await attachedBytes(a, app)))
        }
    }

    /// A worktree file's text and what changed in it. The change is asked
    /// first: a row's mark is as old as the listing, and a deleted file has
    /// no text to read. A tm that can't say gets the text alone.
    private func worktreeFile(_ e: AppAPI.FileEntry, ignored: Bool, repo: AppAPI.FilesRepoRef?, session: String, _ b: Bridge) async -> Shown {
        let diff = try? await b.call(AppAPI.Methods.filesDiff, AppAPI.FilesDiffParams(session: session, repo: repo, path: e.path, ignored: ignored))
        if diff?.status == "D" || (diff == nil && e.status == "D") {
            guard let diff else { return Shown(body: .unavailable("\(e.name) is deleted in this worktree.")) }
            if let why = FileChange.deleted(e.name, diff) { return Shown(body: .unavailable(why), diff: diff) }
            return Shown(body: .text("", size: diff.size, shown: diff.size, mtime: nil), deleted: true, diff: diff, marks: FileMarks.of("", diff))
        }
        let body = await WorktreeRead.body({ p in try await b.call(AppAPI.Methods.filesRead, p) }, session: session, repo: repo, path: e.path, ignored: ignored)
        var marks: FileMarks?
        if case .text(let text, _, _, _) = body, let diff { marks = FileMarks.of(text, diff) }
        return Shown(body: body, diff: diff, marks: marks)
    }

    /// A shown file again, when it changed; what's gone stays and says so.
    /// An unreadable moment (the session stopped) leaves what shows.
    func reread(_ f: OpenedFile, _ app: AppModel) async {
        guard case .worktree(let e, let ignored) = f.source, let session = place?.session, let b = app.bridge,
              let old = shown[f.id]?.body else { return }
        let stat = await WorktreeRead.stat({ p in try await b.call(AppAPI.Methods.filesRead, p) }, session: session, repo: f.repo, path: e.path, ignored: ignored)
        switch stat {
        case .gone:
            if case .text = old { shown[f.id]?.deleted = true }
        case .binary(let size, let mtime):
            var same = false
            switch old {
            case .text(_, let s, _, let m), .binary(let s, let m): same = s == size && m == mtime
            default: break
            }
            if !same || shown[f.id]?.deleted == true { await read(f, app) }
        default:
            break
        }
    }

    // MARK: the file itself

    /// The file's bytes, for Quick Look, Open and Save As.
    func bytes(_ f: OpenedFile, _ app: AppModel) async -> Data? {
        busy = true
        defer { busy = false }
        let outcome: FileOutcome
        switch f.source {
        case .worktree(let e, let ignored):
            guard let session = place?.session, let b = app.bridge else {
                actionProblem = place?.why ?? "Not connected to terminatr."
                return nil
            }
            outcome = await WorktreeRead.bytes({ p in try await b.call(AppAPI.Methods.filesRead, p) }, session: session, repo: f.repo, path: e.path, ignored: ignored)
        case .attached(let a):
            outcome = await attachedBytes(a, app)
        }
        switch outcome {
        case .data(let d, _):
            actionProblem = nil
            return d
        case .gone:
            actionProblem = "\(f.name) is no longer there."
        case .unavailable(let why):
            actionProblem = why
        }
        return nil
    }

    private func attachedBytes(_ a: AppAPI.LibraryFile, _ app: AppModel) async -> FileOutcome {
        guard let place else { return .unavailable("Not connected to terminatr.") }
        return place.server.isEmpty ? await app.libraryData(place.project, a) : await app.peerLibraryData(place.server, place.project, a)
    }

    /// A worktree path's place on this Mac. The server's root and path:
    /// for Copy path and Reveal in Finder, which read nothing.
    func absolute(_ path: String) -> String? {
        guard let root = summary?.root, !root.isEmpty else { return nil }
        return root + "/" + path
    }
}

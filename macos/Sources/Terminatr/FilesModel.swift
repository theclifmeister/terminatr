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
        /// An image file's picture, or why it has none; nil for any other.
        var picture: FileImage.Outcome?
    }

    private(set) var scope = Scope.worktree
    var filter = "" {
        // What it finds shows, not inside a folded folder.
        didSet { if filter != oldValue { shut = [] } }
    }
    private(set) var changedOnly = false
    private(set) var showIgnored = false
    private(set) var open: Set<String> = []
    private(set) var dirs: [String: [AppAPI.FileEntry]] = [:]
    private(set) var found: [AppAPI.FileEntry] = [] {
        didSet { changedDirs = FileTree.changedDirs(found) }
    }
    /// Changed only's tree of what was found.
    private(set) var changedDirs: [String: [AppAPI.FileEntry]] = [:]
    /// The folders Changed only's tree has folded: its folders start open,
    /// and don't touch the tree's own.
    private(set) var shut: Set<String> = []
    /// The top's answer: its root and totals.
    private(set) var summary: AppAPI.FilesList?
    private(set) var cut = false
    /// Why the Worktree tab has nothing, the server's words or the place's.
    private(set) var problem: String?
    private(set) var loaded = false
    private(set) var library: [AppAPI.LibraryFile]?
    /// The kind a program's list is narrowed to.
    var kind: LibraryPage.Kind?
    /// What a program's list leaves out for now.
    private(set) var libraryNote: String?
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
    var place: FilesPlace? {
        // A program has only Attached.
        didSet { if place?.isProgram == true { scope = .attached } }
    }
    /// A paired server's link is down: what shows is as of this. Nothing new
    /// can be read.
    private(set) var offlineAsOf: Date?
    /// A paired server didn't answer in time: the view keeps what it has.
    private(set) var slow = false
    /// The files a read found aren't text, on a paired server.
    private(set) var notText: Set<String> = []

    @ObservationIgnored private var reading: Set<String> = []
    @ObservationIgnored private var keep = FileImageKeep()
    @ObservationIgnored private var stamp = ""
    @ObservationIgnored private var generation = 0
    @ObservationIgnored private var opened = false
    /// It opened on Attached for want of a folder, not by the user's pick:
    /// a session that turns up (the list comes after the window, at launch)
    /// brings it back to Worktree.
    @ObservationIgnored private var fellBack = false
    @ObservationIgnored private var beats = 0
    /// When a paired server last answered, for what offline shows.
    @ObservationIgnored private var readAt: Date?
    /// What showed of a paired server's left, and the words stay until it
    /// shares again.
    @ObservationIgnored private var forgotten = false
    /// How often the panel asks whether what it shows changed, as the TUI:
    /// slower on a paired server, where each is a request to the other machine.
    var every: Duration { far ? .seconds(5) : .seconds(2) }

    /// It shows a paired server's files, read only.
    var far: Bool { place?.far.isEmpty == false }

    /// Why the row under the cursor shows and doesn't open.
    var withheldNote: String? {
        guard scope == .worktree, let c = cursor, let e = rows.first(where: { $0.id == c })?.entry, e.withheld else { return nil }
        return Self.withheldWords(e, far: place?.far ?? "")
    }

    private static func withheldWords(_ e: AppAPI.FileEntry, far: String) -> String {
        e.kind == "dir" ? "\(e.name) may hold secrets, or be a checkout of its own, so it stays on \(far)."
            : "It may hold secrets, so it stays on \(far)."
    }

    /// What shows comes from one search of the whole folder, not from the
    /// folders' listings.
    var searching: Bool { !filter.isEmpty || changedOnly }

    /// A filter's flat list. Changed only stays a tree, with a filter too:
    /// of the changed files it matches.
    var flat: Bool { !filter.isEmpty && !changedOnly }

    /// The folders that show what's in them.
    var unfolded: Set<String> {
        changedOnly ? Set(changedDirs.keys).subtracting(shut) : open
    }

    /// The repo the Worktree tab shows, to name it.
    var repo: AppAPI.FilesRepo? {
        repos.first { $0.path == chosen?.path } ?? repos.first
    }

    var rows: [FileRow] {
        flat ? found.map { FileRow(entry: $0, depth: 0) } : FileTree.rows(dirs: changedOnly ? changedDirs : dirs, open: unfolded)
    }

    var attached: [AppAPI.LibraryFile] {
        let all = library ?? []
        if place?.isProgram == true { return LibraryPage.shown(all, query: filter, kind: kind) }
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
        shut = []
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
        shut = []
        cursor = nil
        Task { await refresh(app) }
    }

    func setShowIgnored(_ on: Bool, _ app: AppModel) {
        showIgnored = on
        Task { await refresh(app) }
    }

    func toggle(folder path: String, _ app: AppModel) {
        if rows.first(where: { $0.id == path })?.entry.withheld == true { return }
        if changedOnly {
            shut.formSymmetricDifference([path])
        } else if open.contains(path) {
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
            picked = r.isFolder || r.entry.withheld ? nil : file(r.entry)
        } else {
            picked = (library ?? []).map { OpenedFile(.attached($0)) }.first { $0.id == id }
        }
    }

    func file(_ e: AppAPI.FileEntry) -> OpenedFile {
        OpenedFile(.worktree(e, ignored: showIgnored || e.ignored), repo: chosen)
    }

    /// ← and →.
    func fold(right: Bool, _ app: AppModel) {
        guard scope == .worktree, !flat else { return }
        switch FileTree.fold(rows, at: cursor, right: right, open: unfolded) {
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
            try? await Task.sleep(for: every)
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
        if place?.isProgram == true {
            loaded = true
            return
        }
        guard let session = place?.session, let b = app.bridge else {
            if place?.farOff == true, readAt != nil {
                stoppedSharing()
            } else if !forgotten {
                noFolder(place?.why ?? "Not connected to terminatr.")
            }
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
                    } catch where i > 0 && !Self.farHolds(error) {
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
            if far {
                readAt = .now
                slow = false
                forgotten = false
                if offlineAsOf != nil {
                    offlineAsOf = nil
                    // What couldn't be read may be read now.
                    shown = shown.filter { if case .unavailable = $0.value.body { false } else { true } }
                }
            }
        } catch {
            guard g == generation else { return }
            let e = error as? AppAPI.Error
            if far, e?.code == "files-off" || e?.code == "not-shared" {
                stoppedSharing()
            } else if far, e?.code == "peer-offline" {
                offlineAsOf = readAt
                slow = false
                if readAt == nil { problem = e?.errorDescription }
                loaded = true
            } else if far, e?.code == "rate-limited" {
                slow = true
                loaded = true
            } else if e?.code == "unknown-session" {
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

    /// A paired server's refusal that says nothing of the folder asked for:
    /// the rest of the tree mustn't drop for it.
    private static func farHolds(_ error: Error) -> Bool {
        guard let code = (error as? AppAPI.Error)?.code else { return false }
        return ["peer-offline", "rate-limited", "files-off", "not-shared", "no-peer-files", "unknown-session"].contains(code)
    }

    /// The paired server stopped sharing: tree, preview and the pane's tabs
    /// of its files all go. Nothing of them is on disk here. Attached stays.
    private func stoppedSharing() {
        let name = place?.far.isEmpty == false ? place?.far ?? "" : place?.server ?? "That server"
        resetTree()
        repos = []
        chosen = nil
        offlineAsOf = nil
        slow = false
        readAt = nil
        notText = []
        problem = "\(name) stopped sharing \(place?.project ?? "the project")'s files. What showed here is gone."
        loaded = true
        for t in pane.tabs { if case .worktree = t.source { pane.close(t.id) } }
        shown = shown.filter { $0.key.hasPrefix("a:") }
        keep = FileImageKeep()
        forgotten = true
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
            if place.isProgram {
                let l = try await app.programLibrary(place.project)
                library = l.files
                libraryNote = l.note.isEmpty ? nil : l.note
                libraryProblem = nil
                return
            }
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

    /// One read of a file at a time: the preview and a pane's tab of the same
    /// image would each hold its bytes. Whoever stops wanting it drops what it
    /// read; a shown file's next look reads it again for whoever still does.
    private func read(_ f: OpenedFile, _ app: AppModel) async {
        guard reading.insert(f.id).inserted else { return }
        defer { reading.remove(f.id) }
        var s: Shown
        switch f.source {
        case .worktree(let e, let ignored):
            if e.withheld {
                s = Shown(body: .unavailable(Self.withheldWords(e, far: place?.far ?? "")))
            } else if e.linkOut || e.kind == "other" {
                s = Shown(body: .unavailable("\(e.name) is a link that doesn't open: it leads out of the worktree, or nowhere."))
            } else if let session = place?.session, let b = app.bridge {
                s = await worktreeFile(e, ignored: ignored, repo: f.repo, session: session, b)
            } else {
                s = Shown(body: .unavailable(place?.why ?? "Not connected to terminatr."))
            }
        case .attached(let a):
            s = await attachedFile(a, app)
        }
        guard !Task.isCancelled else { return }
        shown[f.id] = s
        guard case .drawn(let p) = s.picture else {
            keep.remove(f.id)
            return
        }
        let showing = Set(pane.tabs.map(\.id) + (picked.map { [$0.id] } ?? []))
        for id in keep.add(f.id, pixels: p.image.width * p.image.height, showing: showing) { shown[id] = nil }
    }

    /// An attached file, as its bytes say. An image over what tm reads isn't
    /// asked for: only the size in the list is needed to know.
    private func attachedFile(_ a: AppAPI.LibraryFile, _ app: AppModel) async -> Shown {
        let image = FileImage.named(a.name)
        if image, let why = FileImage.tooBig(a.size) {
            return Shown(body: .binary(size: a.size, mtime: nil), picture: .notDrawn(why))
        }
        let outcome = await attachedBytes(a, app)
        var s = Shown(body: WorktreeRead.body(attached: outcome))
        if image, case .binary = s.body, case .data(let d, _) = outcome {
            s.picture = await FileImageDecoder.shared.decode(d)
        }
        return s
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
        var s = Shown(body: body, diff: diff, marks: marks)
        if far, case .binary = body {
            // Its bytes don't cross.
            notText.insert(e.path)
            return Shown(body: .unavailable("\(e.name) isn't text, so only its name and size show."), diff: diff)
        }
        guard case .binary(let size, _) = body, FileImage.named(e.name) else { return s }
        if let why = FileImage.tooBig(size) {
            s.picture = .notDrawn(why)
            return s
        }
        switch await WorktreeRead.bytes({ p in try await b.call(AppAPI.Methods.filesRead, p) }, session: session, repo: repo, path: e.path, ignored: ignored) {
        case .data(let d, let mtime):
            s.body = .binary(size: d.count, mtime: mtime)
            s.picture = await FileImageDecoder.shared.decode(d)
        case .gone:
            s.body = .gone
        case .unavailable(let why):
            s.picture = .notDrawn(why)
        }
        return s
    }

    /// A shown file again, when it changed; what's gone stays and says so.
    /// An unreadable moment (the session stopped) leaves what shows. One not
    /// read yet, as its first read was dropped or its image let go, is read.
    func reread(_ f: OpenedFile, _ app: AppModel) async {
        if shown[f.id] == nil {
            await load(f, app)
            return
        }
        guard case .worktree(let e, let ignored) = f.source, let session = place?.session, let b = app.bridge,
              let old = shown[f.id]?.body else { return }
        if far, case .unavailable = old { return }
        let stat = await WorktreeRead.stat({ p in try await b.call(AppAPI.Methods.filesRead, p) }, session: session, repo: f.repo, path: e.path, ignored: ignored)
        switch stat {
        case .gone:
            if case .text = old {
                shown[f.id]?.deleted = true
            } else if case .drawn = shown[f.id]?.picture {
                shown[f.id]?.deleted = true
            }
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
            // Nothing of a paired server's is written to this Mac.
            if far {
                actionProblem = "It stays on \(place?.far ?? "that server"): only its text reads here."
                return nil
            }
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
        if place.isProgram { return await app.programLibraryData(place.project, a) }
        return place.server.isEmpty ? await app.libraryData(place.project, a) : await app.peerLibraryData(place.server, place.project, a)
    }

    /// A worktree path's place on this Mac. The server's root and path:
    /// for Copy path and Reveal in Finder, which read nothing.
    func absolute(_ path: String) -> String? {
        guard let root = summary?.root, !root.isEmpty else { return nil }
        return root + "/" + path
    }
}

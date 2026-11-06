import AppKit
import QuickLook
import SwiftUI
import TerminatrKit
import TerminatrUI
import UniformTypeIdentifiers

/// A project of this server in the main pane: the tab the title row picks.
/// Overview, its dashboard and what the TUI's Overview says; Board, its
/// tasks; Memory and Library (T794), read only; Designs, as the TUI's
/// Designs tab; and Mods (T869), its mods on or off for it and their
/// settings for it, as the TUI's project Mods tab.
struct ProjectPages: View {
    @Environment(AppModel.self) private var app
    let project: AppAPI.Project
    /// The note a search hit opened (AppModel.noteShown), for Memory.
    @State private var note: String?

    var body: some View {
        Group {
            switch app.projectTab {
            case .overview, .board:
                ProjectDashboard(project: project, board: app.projectTab == .board)
            case .memory:
                MemoryPane(project: project.slug, note: note)
            case .library:
                LibraryPane(project: project.slug)
            case .designs:
                DesignsPane { DesignsSection(project: project) }
            case .mods:
                ModsPane(project: project.slug)
            }
        }
        .task(id: app.noteShown) {
            if let path = app.takeNote(project.slug), app.projectTabs.contains(.memory) {
                note = path
                app.projectTab = .memory
            }
        }
    }
}

/// The project's memory: CONTEXT.md, MEMORY.md and the notes in a list,
/// the picked one's Markdown beside it, read again each time it shows.
struct MemoryPane: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let project: String
    /// The file to show first: a search hit's note.
    var note: String?
    @State private var files: [AppAPI.MemoryFile]?
    @State private var picked: String?
    @State private var text: String?
    @State private var failure: String?
    @AppStorage("memoryListWidth") private var listWidth = 260.0

    var body: some View {
        PickSplit(width: $listWidth, range: 200...360) {
            let own = MemoryPage.project(files ?? []), notes = MemoryPage.notes(files ?? [])
            PickList(selection: $picked, order: (own + notes).map(\.path), name: "Memory files") {
                if files != nil {
                    if !own.isEmpty {
                        PaneLabel("Project").padding(.horizontal, 8).padding(.vertical, 4)
                        ForEach(own, id: \.path) { f in
                            PickRow(id: f.path, selection: $picked) { MemoryFileRow(f) }
                        }
                    }
                    PaneLabel("Notes · \(notes.count)").padding(.horizontal, 8).padding(.vertical, 4)
                    ForEach(notes, id: \.path) { f in
                        PickRow(id: f.path, selection: $picked) { MemoryFileRow(f) }
                    }
                }
            }
        } detail: {
            ScrollView {
                Group {
                    if let failure {
                        Text(failure).foregroundStyle(theme.attention)
                    } else if let text {
                        MarkdownView(text: text)
                    } else if files?.isEmpty == true {
                        Text("No memory yet: the coordinator writes CONTEXT.md, MEMORY.md and its notes.")
                            .foregroundStyle(theme.muted)
                    } else {
                        ProgressView().controlSize(.small)
                    }
                }
                .font(theme.sized(Flat.text))
                .frame(maxWidth: Flat.reading, alignment: .leading)
                .padding(.horizontal, 24)
                .padding(.vertical, 22)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .scrollEdges()
        }
        .task(id: project) { await list() }
        .task(id: picked) { await read() }
        .onChange(of: note) {
            if let note, files?.contains(where: { $0.path == note }) != false { picked = note }
        }
    }

    private func list() async {
        do {
            let fs = try await app.memoryFiles(project)
            files = fs
            if let note, fs.contains(where: { $0.path == note }) {
                picked = note
            } else if picked == nil || !fs.contains(where: { $0.path == picked }) {
                picked = MemoryPage.first(fs)
            }
        } catch {
            failure = error.localizedDescription
        }
    }

    private func read() async {
        guard let picked else { return }
        text = nil
        do {
            text = try await app.memoryText(project, path: picked)
            failure = nil
        } catch {
            failure = error.localizedDescription
        }
    }
}

/// The project's library: the files threads attached to their reports
/// (resolved and archived threads' too) and the files linked projects
/// sent, grouped by thread, newest first, with a filter from 7 files and
/// chips for its kinds (T1054). A heading says the group's task status,
/// kinds and size, and a group over six files folds (T1055). A group's
/// images show as a strip of thumbnails, four until it opens, each asked for
/// when it comes into view; other files stay rows, and so does an image with
/// no thumbnail (T1059). A file opens in Quick Look (space, or a double
/// click), its app (Open), or is saved (Save As…).
struct LibraryPane: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let project: String
    /// The paired server whose project it is (T886); nil for this
    /// server's.
    var peer: String?
    @State private var files: [AppAPI.LibraryFile]?
    @State private var picked: String?
    @State private var looking: URL?
    @State private var failure: String?
    @State private var busy = false
    @State private var query = ""
    @State private var kind: LibraryPage.Kind?
    /// The groups opened past their fold.
    @State private var opened: Set<String> = []
    /// The thumbnails drawn so far, and the images whose answer was no
    /// thumbnail: those are rows from then on.
    @State private var thumbs = LibraryThumbs()
    @State private var noThumb: Set<String> = []
    /// The list's width, which says how many thumbnails go across.
    @State private var width = 0.0

    /// The files the filter and the kind leave.
    private var shown: [AppAPI.LibraryFile] {
        LibraryPage.shown(files ?? [], query: query, kind: kind)
    }

    /// The groups the filter and the kind leave.
    private var groups: [LibraryPage.Group] { LibraryPage.groups(shown) }

    private func listed(_ g: LibraryPage.Group) -> LibraryPage.Listing {
        let draws = app.drawsThumbnails(peer: peer)
        return g.listed(open: opened.contains(g.id), query: query) { f in
            draws && LibraryPage.hasThumbnail(f) && !noThumb.contains(Self.key(f))
        }
    }

    private var columns: Int { LibraryPage.stripColumns(width: width) }

    /// Whether a group's fold sits at the end of its strip: when it's the
    /// last thing in the group and there's room beside four tiles.
    private func foldBeside(_ l: LibraryPage.Listing) -> Bool {
        l.more > 0 && l.rows.isEmpty && columns == LibraryPage.stripAt
    }

    /// The rows top to bottom, a fold's too: what ↑ and ↓ walk.
    private var order: [String] {
        groups.flatMap { g in
            let l = listed(g)
            return l.files.map(Self.key) + (l.folds ? [Self.foldKey(g)] : [])
        }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if let failure {
                Text(failure)
                    .foregroundStyle(theme.attention)
                    .padding(.horizontal, 16)
                    .padding(.vertical, 8)
            }
            narrowing
            if files?.isEmpty == true {
                Text("No files: a thread's report attaches them (tm report --attach).")
                    .font(theme.sized(Flat.text))
                    .foregroundStyle(theme.muted)
                    .padding(.horizontal, 24)
                    .padding(.vertical, 22)
                Spacer()
            } else if files != nil, shown.isEmpty {
                VStack(alignment: .leading, spacing: 8) {
                    Text("Nothing matches.")
                        .font(theme.sized(Flat.text))
                        .foregroundStyle(theme.muted)
                    Button("Show all files") {
                        query = ""
                        kind = nil
                    }
                }
                .padding(.horizontal, 16)
                .padding(.vertical, 22)
                Spacer()
            } else {
                PickList(selection: $picked, order: order, name: "Files", across: true) {
                    ForEach(groups) { g in
                        let l = listed(g)
                        LibraryHeading(g)
                            .padding(.horizontal, 8)
                            .padding(.top, 10)
                            .padding(.bottom, 3)
                        let rows = LibraryPage.stripRows(l.strip, columns: columns)
                        // By its first file: a LazyVStack drops a row whose id
                        // another group's row has too, as an offset's would.
                        ForEach(rows, id: \.first) { row in
                            stripRow(g, row, fold: row == rows.first && foldBeside(l) ? l.more : nil)
                        }
                        ForEach(l.rows, id: \.self) { f in
                            PickRow(id: Self.key(f), selection: $picked, open: { look(f) }) { LibraryFileRow(f) }
                                .contextMenu { actions(f) }
                        }
                        if l.folds, !foldBeside(l) {
                            PickRow(id: Self.foldKey(g), selection: $picked, act: { toggle(g) }) {
                                Text(l.fewer ? "Show fewer" : "+\(l.more) more").foregroundStyle(theme.muted)
                            }
                            .help(l.fewer ? "Folds this group to its newest files" : "Shows the rest of this group")
                        }
                    }
                }
                .onGeometryChange(for: Double.self) { Double($0.size.width) } action: { width = $0 }
                .onKeyPress(.space) {
                    if let g = groups.first(where: { Self.foldKey($0) == picked }) {
                        toggle(g)
                        return .handled
                    }
                    guard let f = file(picked) else { return .ignored }
                    look(f)
                    return .handled
                }
                if busy || file(picked) != nil {
                    HStack(spacing: 7) {
                        if busy { ProgressView().controlSize(.small) }
                        Spacer()
                        if let f = file(picked) { actions(f) }
                    }
                    .buttonStyle(.flat)
                    .padding(.horizontal, 12)
                    .frame(minHeight: 44)
                    .overlay(alignment: .top) { theme.line.frame(height: 1) }
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .quickLookPreview($looking)
        .task(id: project) { await list() }
        .onChange(of: project) {
            query = ""
            kind = nil
            opened = []
            thumbs.removeAll()
            noThumb = []
        }
    }

    /// One row of a group's strip, as wide as any other's, so tiles line up
    /// from group to group. Each is a row of the lazy list: a tile asks for
    /// its picture when it comes into view.
    private func stripRow(_ g: LibraryPage.Group, _ row: [AppAPI.LibraryFile], fold: Int?) -> some View {
        HStack(alignment: .top, spacing: 5) {
            ForEach(row, id: \.self) { f in
                PickRow(id: Self.key(f), selection: $picked, open: { look(f) }, tile: true) {
                    LibraryTile(f, image: thumbs[Self.thumbKey(project, peer, f)])
                }
                .help("\(f.name) · \(Attachment.size(f.size))")
                .contextMenu { actions(f) }
                .task(id: Self.thumbKey(project, peer, f)) { await draw(f) }
            }
            ForEach(row.count..<columns, id: \.self) { _ in
                Color.clear.frame(maxWidth: .infinity, maxHeight: 1)
            }
            if columns == LibraryPage.stripAt {
                Group {
                    if let fold {
                        PickRow(id: Self.foldKey(g), selection: $picked, act: { toggle(g) }, tile: true) { LibraryStripFold(more: fold) }
                            .help("Shows the rest of this group")
                    } else {
                        Color.clear.frame(maxHeight: 1)
                    }
                }
                .frame(width: LibraryStripFold.width)
            }
        }
        .padding(.horizontal, 5)
    }

    /// Asks for a tile's picture, once it has stayed in view a moment: a
    /// quick scroll past asks for nothing. No thumbnail makes the file a row.
    private func draw(_ f: AppAPI.LibraryFile) async {
        let key = Self.thumbKey(project, peer, f)
        if thumbs[key] != nil {
            thumbs.touch(key)
            return
        }
        try? await Task.sleep(for: .milliseconds(120))
        guard !Task.isCancelled else { return }
        let answer = await app.libraryThumbnail(project, peer: peer, f)
        guard !Task.isCancelled else { return }
        let image = if let answer { await LibraryPage.drawn(answer) } else { FileImage.Drawn?.none }
        guard !Task.isCancelled else { return }
        if let image {
            thumbs.add(key, image)
        } else {
            noThumb.insert(Self.key(f))
        }
    }

    /// Opens or folds a group, from its fold's row. Opening moves the pick
    /// to the first file that came out; folding leaves it on the row.
    private func toggle(_ g: LibraryPage.Group) {
        let was = listed(g)
        if was.more > 0 {
            opened.insert(g.id)
            picked = listed(g).files.first { !was.files.contains($0) }.map(Self.key)
        } else {
            opened.remove(g.id)
        }
    }

    /// The filter and the chips above the list, each when it has a use.
    @ViewBuilder
    private var narrowing: some View {
        let all = files ?? []
        let chips = LibraryPage.chips(all, query: query, kind: kind)
        let filters = LibraryPage.showsFilter(all, query: query)
        if filters || !chips.isEmpty {
            VStack(alignment: .leading, spacing: 8) {
                if filters {
                    PaneFilter(prompt: LibraryPage.filterPrompt(all), label: "Filter the library",
                               help: "Shows the files whose name, task, thread or repository has these words", text: $query)
                }
                if !chips.isEmpty {
                    LibraryChips(chips, kind: Binding { LibraryPage.picked(all, kind) } set: { kind = $0 })
                }
            }
            .padding(.horizontal, 16)
            .padding(.top, 10)
            .padding(.bottom, 4)
        }
    }

    @ViewBuilder
    private func actions(_ f: AppAPI.LibraryFile) -> some View {
        Button("Quick Look") { look(f) }
        Button("Open") {
            guard !FileCaution.launches(f.name) else { return }
            Task { if let url = await copy(f) { NSWorkspace.shared.open(url) } }
        }
        .disabled(FileCaution.launches(f.name))
        .help(FileCaution.launches(f.name) ? "Open would run this file: Quick Look it or Save As…" : "")
        Button("Save As…") { Task { await saveAs(f) } }
    }

    static func key(_ f: AppAPI.LibraryFile) -> String { LibraryPage.id(f) + "/" + f.name }

    /// What a drawn thumbnail is kept by: the file as it is now, wherever
    /// it lives, so one that changed is asked for again.
    static func thumbKey(_ project: String, _ peer: String?, _ f: AppAPI.LibraryFile) -> String {
        [peer ?? "", project, key(f), String(f.size), String(f.time.timeIntervalSince1970)].joined(separator: "\n")
    }

    /// A group's fold row in the list; no file's key, which has a name after
    /// its slash.
    static func foldKey(_ g: LibraryPage.Group) -> String { "fold:" + g.id }

    /// The listed file of this key: one under a fold has no action bar.
    private func file(_ key: String?) -> AppAPI.LibraryFile? {
        for g in groups {
            if let f = listed(g).files.first(where: { Self.key($0) == key }) { return f }
        }
        return nil
    }

    private func list() async {
        do {
            if let peer {
                files = try await app.peerLibrary(peer, project)
            } else {
                files = try await app.library(project)
            }
            failure = nil
        } catch {
            failure = error.localizedDescription
        }
    }

    private func look(_ f: AppAPI.LibraryFile) {
        Task { if let url = await copy(f) { looking = url } }
    }

    /// The file's bytes, read through tm and written to a temporary
    /// folder of the app's own: Quick Look and Open read that copy.
    private func copy(_ f: AppAPI.LibraryFile) async -> URL? {
        guard let d = await bytes(f) else { return nil }
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("terminatr-files/\(UUID().uuidString)")
        let url = dir.appendingPathComponent((f.name as NSString).lastPathComponent)
        do {
            try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
            try d.write(to: url, options: .atomic)
            return url
        } catch {
            failure = error.localizedDescription
            return nil
        }
    }

    private func saveAs(_ f: AppAPI.LibraryFile) async {
        guard let d = await bytes(f) else { return }
        let panel = NSSavePanel()
        panel.nameFieldStringValue = (f.name as NSString).lastPathComponent
        guard panel.runModal() == .OK, let url = panel.url else { return }
        do {
            try d.write(to: url, options: .atomic)
        } catch {
            NSAlert(error: error).runModal()
        }
    }

    private func bytes(_ f: AppAPI.LibraryFile) async -> Data? {
        busy = true
        defer { busy = false }
        let outcome = if let peer { await app.peerLibraryData(peer, project, f) } else { await app.libraryData(project, f) }
        switch outcome {
        case .data(let d, _):
            failure = nil
            return d
        case .gone:
            failure = "\(f.name) is no longer there."
        case .unavailable(let why):
            failure = why
        }
        return nil
    }
}

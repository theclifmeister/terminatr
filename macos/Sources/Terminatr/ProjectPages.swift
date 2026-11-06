import AppKit
import QuickLook
import SwiftUI
import TerminatrKit
import TerminatrUI
import UniformTypeIdentifiers

/// A project of this server in the main pane: the tab the title row picks.
/// Overview, its dashboard and what the TUI's Overview says; Board, its
/// tasks; Memory and Library (T794), read only; and Mods (T869), its mods
/// on or off for it and their settings for it, as the TUI's project Mods
/// tab.
struct ProjectPages: View {
    @Environment(AppModel.self) private var app
    let project: AppAPI.Project
    /// The note a search hit opened (AppModel.noteShown), for Memory.
    @State private var note: String?

    var body: some View {
        Group {
            let tabs = app.projectTabs
            switch tabs.contains(app.projectTab) ? app.projectTab : .overview {
            case .overview, .board:
                ProjectDashboard(project: project, board: app.projectTab == .board)
            case .memory:
                MemoryPane(project: project.slug, note: note)
            case .library:
                LibraryPane(project: project.slug)
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
/// chips for its kinds (T1054). A file opens in Quick Look (space, or a
/// double click), its app (Open), or is saved (Save As…).
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

    /// The files the filter and the kind leave.
    private var shown: [AppAPI.LibraryFile] {
        LibraryPage.shown(files ?? [], query: query, kind: kind)
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
                .padding(.horizontal, 24)
                .padding(.vertical, 22)
                Spacer()
            } else {
                let groups = LibraryPage.groups(shown)
                PickList(selection: $picked, order: groups.flatMap { $0.files.map(Self.key) }, name: "Files") {
                    ForEach(groups) { g in
                        HStack(alignment: .firstTextBaseline) {
                            PaneLabel(g.title).lineLimit(1)
                            Spacer()
                            Text(g.detail).font(theme.sized(Flat.label)).foregroundStyle(theme.muted)
                        }
                        .padding(.horizontal, 8)
                        .padding(.top, 8)
                        .padding(.bottom, 2)
                        ForEach(g.files, id: \.self) { f in
                            PickRow(id: Self.key(f), selection: $picked, open: { look(f) }) { LibraryFileRow(f) }
                                .contextMenu { actions(f) }
                        }
                    }
                }
                .onKeyPress(.space) {
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
        .quickLookPreview($looking)
        .task(id: project) { await list() }
        .onChange(of: project) {
            query = ""
            kind = nil
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

    private func file(_ key: String?) -> AppAPI.LibraryFile? {
        shown.first { Self.key($0) == key }
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

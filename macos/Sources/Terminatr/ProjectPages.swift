import AppKit
import QuickLook
import SwiftUI
import TerminatrKit
import TerminatrUI
import UniformTypeIdentifiers

/// A project of this server in the window's detail: its dashboard, and
/// beside it its pages, picked above it: Overview, Memory and Library
/// (T794), read only, and Mods (T869), its mods on or off for it and
/// their settings for it, as the TUI's project Mods tab.
struct ProjectPages: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let project: AppAPI.Project
    @State private var page: ProjectPage = .dashboard
    /// The note a search hit opened (AppModel.noteShown), for Memory.
    @State private var note: String?

    var body: some View {
        VStack(spacing: 0) {
            let pages = app.projectPages
            Picker("Page", selection: $page) {
                ForEach(pages) { Text($0.rawValue).tag($0) }
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            .fixedSize()
            .padding(.top, 10)
            switch pages.contains(page) ? page : .dashboard {
            case .dashboard:
                ProjectDashboard(project: project)
            case .overview:
                ScrollView {
                    OverviewView(ProjectOverview(project, coordinator: app.coordinatorSession(project.slug),
                                                 needs: app.needs(of: project.slug).count))
                        .padding(28)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
                .scrollEdges()
            case .memory:
                MemoryPane(project: project.slug, note: note)
            case .library:
                LibraryPane(project: project.slug)
            case .mods:
                ModsPane(project: project.slug)
            }
        }
        .task(id: app.noteShown) {
            if let path = app.takeNote(project.slug), app.projectPages.contains(.memory) {
                note = path
                page = .memory
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

    var body: some View {
        HSplitView {
            List(selection: $picked) {
                if let files {
                    let own = MemoryPage.project(files), notes = MemoryPage.notes(files)
                    if !own.isEmpty {
                        Section("Project") { ForEach(own, id: \.path) { MemoryFileRow($0).tag($0.path) } }
                    }
                    Section("Notes · \(notes.count)") { ForEach(notes, id: \.path) { MemoryFileRow($0).tag($0.path) } }
                }
            }
            .frame(minWidth: 200, idealWidth: 240, maxWidth: 360)
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
                .padding(24)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .scrollEdges()
            .frame(minWidth: 320)
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
/// sent, grouped by thread, newest first. A file opens in Quick Look
/// (space, or a double click), its app (Open), or is saved (Save As…).
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

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if let failure {
                Text(failure)
                    .foregroundStyle(theme.attention)
                    .padding(.horizontal, 28)
                    .padding(.vertical, 8)
            }
            if files?.isEmpty == true {
                Text("No files: a thread's report attaches them (tm report --attach).")
                    .foregroundStyle(theme.muted)
                    .padding(28)
                Spacer()
            } else {
                List(selection: $picked) {
                    ForEach(LibraryPage.groups(files ?? [])) { g in
                        Section {
                            ForEach(g.files, id: \.self) { f in
                                LibraryFileRow(f)
                                    .tag(Self.key(f))
                                    .contextMenu { actions(f) }
                            }
                        } header: {
                            HStack {
                                Text(g.title).lineLimit(1)
                                Spacer()
                                Text(g.detail).font(theme.mono(.caption)).foregroundStyle(theme.muted)
                            }
                        }
                    }
                }
                .contextMenu(forSelectionType: String.self) { _ in } primaryAction: { keys in
                    if let f = file(keys.first) { look(f) }
                }
                .onKeyPress(.space) {
                    guard let f = file(picked) else { return .ignored }
                    look(f)
                    return .handled
                }
                HStack {
                    if busy { ProgressView().controlSize(.small) }
                    Spacer()
                    if let f = file(picked) { actions(f) }
                }
                .padding(.horizontal, 16)
                .padding(.vertical, 8)
            }
        }
        .quickLookPreview($looking)
        .task(id: project) { await list() }
    }

    @ViewBuilder
    private func actions(_ f: AppAPI.LibraryFile) -> some View {
        Button("Quick Look") { look(f) }
        Button("Open") { Task { if let url = await copy(f) { NSWorkspace.shared.open(url) } } }
        Button("Save As…") { Task { await saveAs(f) } }
    }

    static func key(_ f: AppAPI.LibraryFile) -> String { LibraryPage.id(f) + "/" + f.name }

    private func file(_ key: String?) -> AppAPI.LibraryFile? {
        files?.first { Self.key($0) == key }
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

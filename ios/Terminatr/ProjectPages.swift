import QuickLook
import SwiftUI
import TerminatrKit
import TerminatrUI
import UIKit

/// A project's Overview (T794), as the Mac's page and the TUI's
/// A › overview: its goal, repositories, coordinator, threads by agent
/// and tasks. Read only.
struct OverviewScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    let slug: String

    var body: some View {
        ScrollView {
            if let p = model.project(slug) {
                OverviewView(ProjectOverview(p, coordinator: model.coordinatorSession(slug),
                                             needs: Need.count(model.needs, project: slug)))
                    .font(theme.sized(Flat.text))
                    .padding(Spacing.gutter)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .refreshable { await model.refresh() }
        .pageBar("Overview")
        .backdrop()
    }
}

/// A project's memory: CONTEXT.md and MEMORY.md, then the notes, each
/// opening its Markdown (memory.read). Read again each time it shows.
struct MemoryScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    let slug: String
    @State private var files: [AppAPI.MemoryFile]?
    @State private var failure: String?

    var body: some View {
        PanelPage {
            if let failure { Failure(text: failure) }
            if let files {
                let own = MemoryPage.project(files), notes = MemoryPage.notes(files)
                if files.isEmpty {
                    Text("No memory yet: the coordinator writes CONTEXT.md, MEMORY.md and its notes.")
                        .font(theme.sized(Flat.text))
                        .foregroundStyle(theme.muted)
                }
                if !own.isEmpty {
                    SectionHeading("Project")
                    PanelList { rows(own) }
                }
                if !notes.isEmpty {
                    SectionHeading("Notes · \(notes.count)")
                    PanelList { rows(notes) }
                }
            } else if failure == nil {
                ProgressView()
            }
        }
        .task(id: model.generation) { await list() }
        .refreshable { await list() }
        .keepsPlace(route: Route.memory(slug))
        .pageBar("Memory")
        .backdrop()
    }

    private func rows(_ fs: [AppAPI.MemoryFile]) -> some View {
        ForEach(fs, id: \.path) { f in
            RowLink(value: Route.memoryFile(slug, path: f.path)) { MemoryFileRow(f) }
        }
    }

    private func list() async {
        do {
            files = try await model.memoryFiles(slug)
            failure = nil
        } catch {
            failure = error.localizedDescription
        }
    }
}

/// One memory file's Markdown.
struct MemoryFileScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    let slug: String
    let path: String
    @State private var text: String?
    @State private var failure: String?

    var body: some View {
        ScrollView {
            Group {
                if let failure {
                    Failure(text: failure)
                } else if let text {
                    MarkdownView(text: text)
                        .font(theme.sized(Flat.text))
                        .foregroundStyle(theme.ink)
                } else {
                    ProgressView()
                }
            }
            .padding(Spacing.gutter)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .task(id: model.generation) { await read() }
        .refreshable { await read() }
        .pageBar((path as NSString).lastPathComponent)
        .backdrop()
    }

    private func read() async {
        do {
            text = try await model.memoryText(slug, path: path)
            failure = nil
        } catch {
            failure = error.localizedDescription
        }
    }
}

/// A project's library: the files threads attached to their reports
/// and those linked projects sent, grouped by thread, newest first
/// (LibraryPage), with a filter from 7 files and chips for its kinds
/// (T1054). A tap opens a file in Quick Look, whose share button
/// sends it on; Share… in its menu goes straight to the share sheet.
struct LibraryScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    let slug: String
    /// The paired server whose project it is (T886); nil for this
    /// server's.
    var peer: String?
    @State private var files: [AppAPI.LibraryFile]?
    @State private var failure: String?
    @State private var looking: URL?
    @State private var sharing: SharedFile?
    @State private var reading: AppAPI.LibraryFile?
    @State private var query = ""
    @State private var kind: LibraryPage.Kind?

    var body: some View {
        PanelPage {
            if let failure { Failure(text: failure) }
            if let files {
                let shown = LibraryPage.shown(files, query: query, kind: kind)
                let chips = LibraryPage.chips(files, query: query, kind: kind)
                if !chips.isEmpty {
                    LibraryChips(chips, kind: Binding { LibraryPage.picked(files, kind) } set: { kind = $0 })
                }
                if files.isEmpty {
                    Text("No files: a thread's report attaches them (tm report --attach).")
                        .font(theme.sized(Flat.text))
                        .foregroundStyle(theme.muted)
                } else if shown.isEmpty {
                    Text("Nothing matches.")
                        .font(theme.sized(Flat.text))
                        .foregroundStyle(theme.muted)
                    Button("Show all files") {
                        query = ""
                        kind = nil
                    }
                    .font(theme.sized(Flat.text))
                }
                ForEach(LibraryPage.groups(shown)) { g in
                    VStack(alignment: .leading, spacing: 1) {
                        Text(g.title).font(theme.sized(12)).lines(1)
                        Text(g.detail).font(theme.monoSized(11))
                    }
                    .foregroundStyle(theme.muted)
                    .padding(.top, Spacing.item)
                    PanelList {
                        ForEach(g.files, id: \.self) { row($0) }
                    }
                }
            } else if failure == nil {
                ProgressView()
            }
        }
        .task(id: model.generation) { await list() }
        .refreshable { await list() }
        .quickLookPreview($looking)
        .sheet(item: $sharing) { ShareSheet(url: $0.url).presentationDetents([.medium, .large]) }
        // In the screen, under its title (T928, FilterField).
        .safeAreaInset(edge: .top, spacing: 0) {
            if let files, LibraryPage.showsFilter(files, query: query) {
                FilterField(prompt: LibraryPage.filterPrompt(files), text: $query)
            }
        }
        .pageBar("Library")
        .backdrop()
    }

    private func row(_ f: AppAPI.LibraryFile) -> some View {
        Button {
            Task { if let url = await copy(f) { looking = url } }
        } label: {
            ListRow {
                HStack {
                    LibraryFileRow(f)
                    if reading == f { ProgressView().controlSize(.small) }
                }
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(reading != nil)
        .accessibilityHint("Opens it in Quick Look")
        .contextMenu {
            Button("Quick Look", systemImage: "eye") {
                Task { if let url = await copy(f) { looking = url } }
            }
            Button("Share…", systemImage: "square.and.arrow.up") {
                Task { if let url = await copy(f) { sharing = SharedFile(url: url) } }
            }
        }
    }

    private func list() async {
        do {
            if let peer {
                files = try await model.peerLibrary(peer, slug)
            } else {
                files = try await model.library(slug)
            }
            failure = nil
        } catch {
            failure = error.localizedDescription
        }
    }

    /// The file's bytes, read through the link and written to a
    /// temporary folder of the app's own: Quick Look and the share sheet
    /// read that copy.
    private func copy(_ f: AppAPI.LibraryFile) async -> URL? {
        reading = f
        defer { reading = nil }
        let outcome = if let peer { await model.peerLibraryData(peer, slug, f) } else { await model.libraryData(slug, f) }
        switch outcome {
        case .data(let d, _):
            let dir = FileManager.default.temporaryDirectory.appendingPathComponent("terminatr-files/\(UUID().uuidString)")
            let url = dir.appendingPathComponent((f.name as NSString).lastPathComponent)
            do {
                try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
                try d.write(to: url, options: .atomic)
                failure = nil
                return url
            } catch {
                failure = error.localizedDescription
            }
        case .gone:
            failure = "\(f.name) is no longer there."
        case .unavailable(let why):
            failure = why
        }
        return nil
    }
}

/// A library file's copy, for the share sheet.
struct SharedFile: Identifiable {
    let id = UUID()
    let url: URL
}

/// The system's share sheet for a file.
struct ShareSheet: UIViewControllerRepresentable {
    let url: URL

    func makeUIViewController(context: Context) -> UIActivityViewController {
        UIActivityViewController(activityItems: [url], applicationActivities: nil)
    }

    func updateUIViewController(_ vc: UIActivityViewController, context: Context) {}
}

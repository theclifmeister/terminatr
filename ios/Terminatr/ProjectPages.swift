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
    let slug: String

    var body: some View {
        ScrollView {
            if let p = model.project(slug) {
                OverviewView(ProjectOverview(p, coordinator: model.coordinatorSession(slug),
                                             needs: Need.count(model.needs, project: slug)))
                    .padding(20)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .refreshable { await model.refresh() }
        .navigationTitle("Overview")
        .navigationBarTitleDisplayMode(.inline)
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
        List {
            if let failure { Failure(text: failure) }
            if let files {
                let own = MemoryPage.project(files), notes = MemoryPage.notes(files)
                if files.isEmpty {
                    Text("No memory yet: the coordinator writes CONTEXT.md, MEMORY.md and its notes.")
                        .foregroundStyle(theme.muted)
                }
                if !own.isEmpty {
                    Section("Project") { rows(own) }
                }
                if !notes.isEmpty {
                    Section("Notes · \(notes.count)") { rows(notes) }
                }
            } else if failure == nil {
                ProgressView()
            }
        }
        .task(id: model.generation) { await list() }
        .refreshable { await list() }
        .keepsPlace(Route.memory(slug).listKey)
        .navigationTitle("Memory")
        .backdrop()
    }

    private func rows(_ fs: [AppAPI.MemoryFile]) -> some View {
        ForEach(fs, id: \.path) { f in
            RouteLink(value: Route.memoryFile(slug, path: f.path)) { MemoryFileRow(f) }
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
                } else {
                    ProgressView()
                }
            }
            .padding(20)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .task(id: model.generation) { await read() }
        .refreshable { await read() }
        .navigationTitle((path as NSString).lastPathComponent)
        .navigationBarTitleDisplayMode(.inline)
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
/// (LibraryPage). A tap opens a file in Quick Look, whose share button
/// sends it on; Share… in its menu goes straight to the share sheet.
struct LibraryScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    let slug: String
    @State private var files: [AppAPI.LibraryFile]?
    @State private var failure: String?
    @State private var looking: URL?
    @State private var sharing: SharedFile?
    @State private var reading: AppAPI.LibraryFile?

    var body: some View {
        List {
            if let failure { Failure(text: failure) }
            if let files {
                if files.isEmpty {
                    Text("No files: a thread's report attaches them (tm report --attach).")
                        .foregroundStyle(theme.muted)
                }
                ForEach(LibraryPage.groups(files)) { g in
                    Section {
                        ForEach(g.files, id: \.self) { row($0) }
                    } header: {
                        VStack(alignment: .leading, spacing: 1) {
                            Text(g.title).lines(1)
                            Text(g.detail).font(theme.mono(.caption2))
                        }
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
        .navigationTitle("Library")
        .backdrop()
    }

    private func row(_ f: AppAPI.LibraryFile) -> some View {
        Button {
            Task { if let url = await copy(f) { looking = url } }
        } label: {
            HStack {
                LibraryFileRow(f)
                if reading == f { ProgressView().controlSize(.small) }
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
            files = try await model.library(slug)
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
        switch await model.libraryData(slug, f) {
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

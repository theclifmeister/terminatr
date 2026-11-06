import SwiftUI
import TerminatrKit
import TerminatrUI

/// The paired server's task the window's paired task view shows
/// (AppModel.peerTaskShown).
struct PeerTaskShown: Identifiable, Hashable {
    let alias: String
    let project: String
    let task: String
    var id: String { "\(alias)/\(project)/\(task)" }
}

/// A task of a paired server's board, read only, as that server shares it
/// (peer.pages-read): its title, status and steps. Its notes stay there,
/// and nothing here writes.
struct PeerTaskSheet: View {
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @Environment(\.theme) private var theme
    let shown: PeerTaskShown
    @State private var state: PeerPages.State<AppAPI.Task> = .loading

    private var server: String {
        app.peers.first { $0.alias == shown.alias }.map(PeerPages.server) ?? shown.alias
    }

    private var shared: AppAPI.Task? {
        if case .shared(let t) = state { return t }
        return nil
    }

    var body: some View {
        FlatSheet(width: 640) {
            if let t = shared {
                VStack(alignment: .leading, spacing: 6) {
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        Text(t.ref)
                            .font(theme.monoSized(Flat.text, .medium))
                            .foregroundStyle(theme.action)
                        Text(t.title)
                            .font(theme.title(.headline, .semibold))
                            .fixedSize(horizontal: false, vertical: true)
                            .textSelection(.enabled)
                            .accessibilityAddTraits(.isHeader)
                        Spacer(minLength: 8)
                        Chip(TaskDetail.words(t.status))
                    }
                    Text("\(shown.project) on \(server) · read only")
                        .font(theme.sized(Flat.label))
                        .foregroundStyle(theme.muted)
                }
            } else {
                SheetTitle(shown.task, detail: "\(shown.project) on \(server)")
            }
        } content: {
            if let t = shared {
                ScrollView {
                    VStack(alignment: .leading, spacing: 12) {
                        TaskDetailView(TaskDetail(project: shown.project, task: t)) { EmptyView() }
                        Text("Its notes stay on \(server).")
                            .font(theme.sized(Flat.label))
                            .foregroundStyle(theme.muted)
                    }
                    .padding(16)
                    .frame(maxWidth: .infinity, alignment: .leading)
                }
                .frame(maxHeight: 520)
                .fixedSize(horizontal: false, vertical: true)
            } else {
                PeerPageEmpty(page: .board, server: server, state: state)
                    .frame(height: 200)
            }
        } footer: {
            let thread = shared.flatMap { $0.thread.isEmpty ? nil : SessionRef(project: shown.project, thread: $0.thread, server: shown.alias) }
            if let thread, thread.peerThread(in: app.peers) != nil {
                Button("Show Thread") {
                    dismiss()
                    app.select(.session(thread))
                }
                .fixedSize()
            }
            Button("Show Project") {
                dismiss()
                app.select(.project(shown.project, server: shown.alias))
            }
            .fixedSize()
            Spacer()
            Button("Close") { dismiss() }
                .keyboardShortcut(.cancelAction)
        }
        .guardingLinks()
        .task(id: shown) {
            do {
                state = PeerPages.task(shown.task, in: try await app.peerPages(shown.alias, shown.project), server: server)
            } catch {
                state = PeerPages.state(error)
            }
        }
    }
}

extension AppModel {
    /// Shows a paired server's task over the window, read only.
    func showPeerTask(alias: String, project: String, task: String) {
        palette = false
        peerTaskShown = PeerTaskShown(alias: alias, project: project, task: task)
        showWindow?()
    }
}

import SwiftUI
import TerminatrKit
import TerminatrUI

/// A paired server's shared project in the window's detail (T886): the
/// same pages as one of this server's, picked above it. Its dashboard
/// (its coordinator and threads, and its board when shared), Overview,
/// Memory and Library, read through this server's link while that
/// server's project pages switch for this one is on. A page it doesn't
/// share shows "Not shared by <server>"; Memory always does. Nothing on
/// them writes.
struct RemoteProjectPages: View {
    @Environment(AppModel.self) private var app
    let peer: AppAPI.Peer
    let project: AppAPI.PeerProject
    @State private var page: ProjectPage = .dashboard
    @State private var pages: PeerPages.State<AppAPI.PeerPages> = .loading

    var body: some View {
        VStack(spacing: 0) {
            Picker("Page", selection: $page) {
                // A paired server's mods are its own: never here.
                ForEach(ProjectPage.allCases.filter { $0 != .mods }) { Text($0.rawValue).tag($0) }
            }
            .pickerStyle(.segmented)
            .labelsHidden()
            .fixedSize()
            .padding(.top, 10)
            let server = PeerPages.server(peer)
            switch page {
            case .mods:
                EmptyView()
            case .dashboard:
                RemoteProjectDashboard(peer: peer, project: project, pages: pages)
            case .overview:
                if case .shared(let p) = pages {
                    ScrollView {
                        OverviewView(ProjectOverview(p, project: project))
                            .padding(28)
                            .frame(maxWidth: .infinity, alignment: .leading)
                    }
                    .scrollEdges()
                } else {
                    PeerPageEmpty(page: .overview, server: server, state: pages)
                }
            case .memory:
                PeerPageEmpty(page: .memory, server: server, state: PeerPages.State<AppAPI.PeerPages>.notShared)
            case .library:
                if case .shared = pages {
                    LibraryPane(project: project.project, peer: peer.alias)
                } else {
                    PeerPageEmpty(page: .library, server: server, state: pages)
                }
            }
        }
        .task(id: PagesRead(alias: peer.alias, project: project.project, pages: project.pages, reads: app.readsPeerPages)) { await read() }
    }

    private func read() async {
        guard app.readsPeerPages else {
            pages = .notShared
            return
        }
        do {
            pages = .shared(try await app.peerPages(peer.alias, project.project))
        } catch {
            pages = PeerPages.state(error)
        }
    }

    /// When the pages are read again: another project, its switch as
    /// that server last said it, or a tm that reads them.
    private struct PagesRead: Equatable {
        let alias: String
        let project: String
        let pages: Bool
        let reads: Bool
    }
}

/// A page a paired server doesn't share, or couldn't be read.
struct PeerPageEmpty<V: Equatable & Sendable>: View {
    @Environment(\.theme) private var theme
    let page: PeerPages.Page
    let server: String
    let state: PeerPages.State<V>

    var body: some View {
        VStack {
            switch state {
            case .loading:
                ProgressView().controlSize(.small)
            case .failed(let why):
                ContentUnavailableView(page.rawValue, systemImage: "exclamationmark.triangle", description: Text(why))
            default:
                ContentUnavailableView(PeerPages.notShared(by: server), systemImage: "eye.slash",
                                       description: Text(PeerPages.notSharedDetail(page, server: server)))
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .foregroundStyle(theme.muted)
    }
}

/// A paired server's board, read only: on deck shown, in motion, the
/// backlog and the done tasks folded; a task shows its steps.
struct PeerBoard: View {
    @Environment(\.theme) private var theme
    let pages: AppAPI.PeerPages
    @State private var unfolded: Set<String> = []

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            group("On deck", PeerPages.onDeck(pages), folds: false)
            group("In motion", PeerPages.moving(pages), folds: false)
            group("Backlog", PeerPages.backlog(pages), folds: true)
            group("Done", PeerPages.done(pages), folds: true)
            if pages.truncated {
                Text("The board was too long to cross whole: some done tasks stay there.")
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.muted)
            }
        }
    }

    @ViewBuilder
    private func group(_ title: String, _ ts: [AppAPI.Task], folds: Bool) -> some View {
        if !ts.isEmpty {
            let open = !folds || unfolded.contains(title)
            Button {
                if unfolded.contains(title) { unfolded.remove(title) } else { unfolded.insert(title) }
            } label: {
                HStack(spacing: 6) {
                    if folds { Image(systemName: open ? "chevron.down" : "chevron.right").font(.caption) }
                    Text("\(title) · \(ts.count)").font(theme.body(.subheadline, .medium))
                }
                .foregroundStyle(theme.muted)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .disabled(!folds)
            if open {
                CardGrid { ForEach(ts) { PeerBoardRow(task: $0) } }
            }
        }
    }
}

/// A task of a paired server's board: its id, title and steps.
private struct PeerBoardRow: View {
    @Environment(\.theme) private var theme
    let task: AppAPI.Task
    @State private var open = false

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Button { open.toggle() } label: {
                HStack(alignment: .center, spacing: 8) {
                    Text(task.ref)
                        .font(theme.mono(.subheadline, .medium))
                        .foregroundStyle(theme.action)
                        .frame(minWidth: 44, alignment: .leading)
                    Text(task.title)
                        .font(theme.body(.body))
                        .lineLimit(1)
                    Spacer(minLength: 8)
                    if !task.steps.isEmpty {
                        Text("\(task.steps.filter(\.done).count)/\(task.steps.count)")
                            .font(theme.mono(.caption))
                            .foregroundStyle(theme.muted)
                    }
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .disabled(task.steps.isEmpty)
            if open {
                ForEach(task.steps, id: \.n) { s in
                    Label(s.text, systemImage: s.done ? "checkmark.circle.fill" : "circle")
                        .font(theme.body(.callout))
                        .foregroundStyle(s.done ? theme.muted : theme.ink)
                }
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 6)
        .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 9))
        .overlay(RoundedRectangle(cornerRadius: 9).strokeBorder(theme.edge, lineWidth: 1))
        .accessibilityElement(children: .combine)
    }
}

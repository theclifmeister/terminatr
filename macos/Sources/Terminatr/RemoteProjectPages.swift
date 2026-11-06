import SwiftUI
import TerminatrKit
import TerminatrUI

/// A paired server's shared project in the main pane (T886): the same
/// tabs as one of this server's, picked in the title row. Overview (its
/// coordinator and threads, and what it shares of its overview), Board,
/// Memory, Library and Designs, read through this server's link while that
/// server's project pages switch for this one is on. A page it doesn't
/// share shows "Not shared by <server>"; Memory always does. Nothing on
/// them writes.
struct RemoteProjectPages: View {
    @Environment(AppModel.self) private var app
    let peer: AppAPI.Peer
    let project: AppAPI.PeerProject
    @State private var pages: PeerPages.State<AppAPI.PeerPages> = .loading

    var body: some View {
        Group {
            let server = PeerPages.server(peer)
            switch app.projectTab {
            case .mods:
                EmptyView()
            case .overview, .board:
                RemoteProjectDashboard(peer: peer, project: project, pages: pages, board: app.projectTab == .board)
            case .memory:
                PeerPageEmpty(page: .memory, server: server, state: PeerPages.State<AppAPI.PeerPages>.notShared)
            case .library:
                if case .shared = pages {
                    LibraryPane(project: project.project, peer: peer.alias)
                } else {
                    PeerPageEmpty(page: .library, server: server, state: pages)
                }
            case .designs:
                DesignsPane { DesignsSection(peer: peer, project: project) }
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
        VStack(alignment: .leading, spacing: 22) {
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
            VStack(alignment: .leading, spacing: 7) {
                Button {
                    if unfolded.contains(title) { unfolded.remove(title) } else { unfolded.insert(title) }
                } label: {
                    HStack(spacing: 5) {
                        if folds {
                            Image(systemName: "chevron.right")
                                .rotationEffect(.degrees(open ? 90 : 0))
                                .font(theme.body(.glyph, .semibold))
                        }
                        Text("\(title) · \(ts.count)").font(theme.sized(Flat.label))
                    }
                    .foregroundStyle(theme.muted)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .disabled(!folds)
                .accessibilityValue(folds ? (open ? "open" : "folded") : "")
                if open {
                    HairlineList { ForEach(ts) { PeerBoardRow(task: $0) } }
                }
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
      ListRow {
        VStack(alignment: .leading, spacing: 6) {
            Button { open.toggle() } label: {
                HStack(alignment: .center, spacing: 10) {
                    Text(task.ref)
                        .font(theme.mono(.subheadline))
                        .foregroundStyle(theme.muted)
                        .lineLimit(1)
                        .fixedSize()
                    Text(task.title)
                        .lineLimit(1)
                        .frame(minWidth: ThreadCard.titleMin, alignment: .leading)
                    Spacer(minLength: 8)
                    if !task.steps.isEmpty {
                        Text("\(task.steps.filter(\.done).count)/\(task.steps.count)")
                            .font(theme.sized(Flat.label))
                            .foregroundStyle(theme.muted)
                    }
                    if task.status == "done" {
                        FinishedLabel(task: task)
                    }
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .disabled(task.steps.isEmpty)
            if open {
                ForEach(task.steps, id: \.n) { s in
                    Label(s.text, systemImage: s.done ? "checkmark.circle.fill" : "circle")
                        .font(theme.sized(Flat.label))
                        .foregroundStyle(s.done ? theme.muted : theme.ink)
                }
            }
        }
      }
      .accessibilityElement(children: .combine)
    }
}

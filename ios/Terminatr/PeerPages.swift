import QuickLook
import SwiftUI
import TerminatrKit
import TerminatrUI

/// A paired server's shared project (T846, T886), drawn as one of this
/// server's (ProjectScreen): its header, its board's counts as
/// StatTiles, its coordinator, running there or not, its pages as
/// PageTiles, its designs, then its threads (SidebarTree.sessions). The
/// counts and the pages come from that server while its project pages
/// switch for this one is on (peer.pages-read); otherwise the counts
/// show – and each page "Not shared by <server>"; Memory always does.
/// Its designs (T888) read the same way, read-only, without their pages
/// or open questions. Nothing here writes.
struct PeerProjectScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.navigate) private var navigate
    @Environment(\.theme) private var theme
    let alias: String
    let project: String
    @State private var pages: PeerPages.State<AppAPI.PeerPages> = .loading

    var body: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: Spacing.row + 2) {
                let peer = model.peers.first(where: { $0.alias == alias })
                let p = peer?.projects.first(where: { $0.project == project })
                ProjectHeader(project, goal: p?.goal ?? "")
                stats
                if let peer, let p {
                    let sessions = SidebarTree.sessions(of: p, alias: alias)
                    let coordinator = sessions.filter(\.ref.isCoordinator), threads = sessions.filter { !$0.ref.isCoordinator }
                    if !coordinator.isEmpty {
                        tiles(coordinator)
                    }
                    pageTiles(server: PeerPages.server(peer))
                    if model.listsDesigns {
                        DesignsTiles(alias: alias, project: p, generation: model.generation)
                    }
                    SectionHeading("Threads", detail: threads.isEmpty ? "" : "\(threads.count)")
                    if threads.isEmpty {
                        notShared("No threads")
                    } else {
                        tiles(threads)
                    }
                }
            }
            .padding(.horizontal, Spacing.gutter)
            .padding(.bottom, Spacing.gutter)
        }
        .task(id: PagesRead(alias: alias, project: project, generation: model.generation, shared: shared)) {
            pages = await model.peerPagesState(alias, project)
        }
        .refreshable {
            await model.refresh()
            pages = await model.peerPagesState(alias, project)
        }
        .keepsPlace(route: Route.peerProject(alias: alias, project: project))
        .navigationTitle(project)
        .headerTitle()
        .backdrop()
    }

    /// Whether that server's last project list says the pages are on: a
    /// change reads them again.
    private var shared: Bool {
        model.peers.first(where: { $0.alias == alias })?.projects.first(where: { $0.project == project })?.pages ?? false
    }

    /// The board's counts, each opening the board; – while not shared.
    private var stats: some View {
        let counts: AppAPI.PeerPages? = if case .shared(let p) = pages { p } else { nil }
        return TileRow {
            ForEach(PeerPages.stats(counts), id: \.1) { n, word in
                PlainRouteLink(value: Route.peerPage(alias: alias, project: project, page: .board)) { StatTile(n, word) }
                    .buttonStyle(.plain)
                    .accessibilityHint("Opens the board")
            }
        }
    }

    /// The pages, as a project of this server's shows them: Memory and
    /// Library as its tm reads those.
    private func pageTiles(server: String) -> some View {
        let tiles: [(PeerPages.Page, String)] = [
            (.overview, "info.circle"), (.board, "list.bullet.rectangle"), (.memory, "brain"), (.library, "books.vertical"),
        ]
        return TileRow {
            ForEach(tiles, id: \.0) { page, symbol in
                let route = Route.peerPage(alias: alias, project: project, page: page)
                PlainRouteLink(value: route) { PageTile(page.rawValue, symbol: symbol, selected: navigate.opens(route, on: model)) }
                    .buttonStyle(.plain)
            }
        }
    }

    private func tiles(_ sessions: [(session: AppAPI.PeerSession?, ref: SessionRef, task: String, title: String)]) -> some View {
        TileGroup {
            ForEach(Array(sessions.enumerated()), id: \.element.ref) { i, e in
                let route = Route.session(e.ref)
                PlainRouteLink(value: route) {
                    SessionTile(e.ref.isCoordinator ? "Coordinator" : e.title.isEmpty ? e.ref.thread : e.title,
                                ref: e.task.isEmpty ? nil : e.task, state: .of(peer: e.session), rule: i > 0,
                                selected: navigate.opens(route, on: model))
                }
                .buttonStyle(.plain)
            }
        }
    }

    private func notShared(_ text: String) -> some View {
        Text(text)
            .font(theme.body(.subheadline))
            .foregroundStyle(theme.muted)
            .padding(Spacing.card)
            .frame(maxWidth: .infinity, alignment: .leading)
            .surface(.card, in: RoundedRectangle(cornerRadius: Radius.tile, style: .continuous))
    }

    private struct PagesRead: Equatable {
        let alias: String
        let project: String
        let generation: Int
        let shared: Bool
    }
}

/// One page of a paired server's project (T886): its Overview, Board,
/// Library or Designs (T888) while that server shares them, else "Not
/// shared by <server>"; Memory always so.
struct PeerPageScreen: View {
    @Environment(PhoneModel.self) private var model
    let alias: String
    let project: String
    let page: PeerPages.Page
    @State private var pages: PeerPages.State<AppAPI.PeerPages> = .loading

    var body: some View {
        let server = model.peers.first(where: { $0.alias == alias }).map(PeerPages.server) ?? alias
        Group {
            switch (page, pages) {
            case (.memory, _):
                PeerNotShared(page: page, server: server)
            case (.designs, _):
                ScrollView {
                    if let p = model.peers.first(where: { $0.alias == alias })?.projects.first(where: { $0.project == project }) {
                        DesignsTiles(alias: alias, project: p, generation: model.generation)
                            .padding(.horizontal, Spacing.gutter)
                    }
                }
            case (.library, .shared):
                LibraryScreen(slug: project, peer: alias)
            case (.overview, .shared(let p)):
                PeerOverviewPage(pages: p, alias: alias, project: project)
            case (.board, .shared(let p)):
                PeerBoardPage(pages: p)
            case (_, .loading):
                ProgressView().frame(maxWidth: .infinity, maxHeight: .infinity)
            case (_, .failed(let why)):
                ContentUnavailableView(page.rawValue, systemImage: "exclamationmark.triangle", description: Text(why))
            default:
                PeerNotShared(page: page, server: server)
            }
        }
        .task(id: Read(generation: model.generation)) {
            guard page.shareable, page != .designs else { return }
            pages = await model.peerPagesState(alias, project)
        }
        .navigationTitle(page.rawValue)
        .navigationBarTitleDisplayMode(.inline)
        .backdrop()
    }

    private struct Read: Equatable {
        let generation: Int
    }
}

/// "Not shared by studio", and what shares it.
struct PeerNotShared: View {
    let page: PeerPages.Page
    let server: String

    var body: some View {
        ContentUnavailableView(PeerPages.notShared(by: server), systemImage: "eye.slash",
                               description: Text(PeerPages.notSharedDetail(page, server: server)))
            .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

/// A paired server's project's Overview: its goal, how many
/// repositories (never their paths), its coordinator and threads, its
/// tasks, and PROJECT.md.
private struct PeerOverviewPage: View {
    @Environment(PhoneModel.self) private var model
    let pages: AppAPI.PeerPages
    let alias: String
    let project: String

    var body: some View {
        ScrollView {
            if let p = model.peers.first(where: { $0.alias == alias })?.projects.first(where: { $0.project == project }) {
                OverviewView(ProjectOverview(pages, project: p))
                    .padding(20)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
    }
}

/// A paired server's board, read only, as this server's: on deck shown,
/// in motion, the backlog and the done tasks folded, a filter; a task
/// opens its steps.
private struct PeerBoardPage: View {
    @Environment(\.theme) private var theme
    let pages: AppAPI.PeerPages
    @State private var filter = ""
    @State private var unfolded: Set<String> = []
    @State private var open: AppAPI.Task?

    var body: some View {
        List {
            let filtering = !filter.trimmingCharacters(in: .whitespaces).isEmpty
            let deck = Dashboard.filtered(PeerPages.onDeck(pages), by: filter)
            let moving = Dashboard.filtered(PeerPages.moving(pages), by: filter)
            let backlog = Dashboard.filtered(PeerPages.backlog(pages), by: filter)
            let done = Dashboard.filtered(PeerPages.done(pages), by: filter)
            if !deck.isEmpty { Section("On deck · \(deck.count)") { rows(deck) } }
            if !moving.isEmpty { Section("In motion · \(moving.count)") { rows(moving) } }
            if !backlog.isEmpty || !done.isEmpty {
                Section {
                    if !backlog.isEmpty {
                        DisclosureGroup("Backlog · \(backlog.count)", isExpanded: fold("backlog", filtering)) { rows(backlog) }
                    }
                    if !done.isEmpty {
                        DisclosureGroup("Done · \(done.count)", isExpanded: fold("done", filtering)) { rows(done) }
                    }
                }
            }
            if deck.isEmpty && moving.isEmpty && backlog.isEmpty && done.isEmpty {
                Text(filtering ? "No task on the board matches “\(filter.trimmingCharacters(in: .whitespaces))”." : "Nothing on the board.")
                    .foregroundStyle(theme.muted)
            }
            if pages.truncated {
                Text("The board was too long to cross whole: some done tasks stay there.")
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.muted)
            }
        }
        .searchable(text: $filter, prompt: "Filter by id or title")
        .sheet(item: $open) { t in PeerTaskSheet(task: t).presentationDetents([.medium, .large]) }
    }

    private func fold(_ key: String, _ filtering: Bool) -> Binding<Bool> {
        Binding {
            filtering || unfolded.contains(key)
        } set: { on in
            if on { unfolded.insert(key) } else { unfolded.remove(key) }
        }
    }

    private func rows(_ ts: [AppAPI.Task]) -> some View {
        ForEach(ts) { t in
            Button { open = t } label: {
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Text(t.ref)
                        .font(theme.mono(.subheadline, .medium))
                        .foregroundStyle(theme.action)
                        .frame(minWidth: 40, alignment: .leading)
                    Text(t.title)
                        .font(theme.body(.body))
                        .foregroundStyle(theme.ink)
                        .lines(2)
                    Spacer(minLength: 4)
                    if !t.steps.isEmpty {
                        Text("\(t.steps.filter(\.done).count)/\(t.steps.count)")
                            .font(theme.mono(.caption))
                            .foregroundStyle(theme.muted)
                    }
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityElement(children: .combine)
            .accessibilityHint("Shows its steps")
        }
    }
}

/// A paired server's task: its status, thread and steps. Its notes stay
/// there.
private struct PeerTaskSheet: View {
    @Environment(\.theme) private var theme
    let task: AppAPI.Task

    var body: some View {
        NavigationStack {
            List {
                Section {
                    LabeledContent("Status", value: task.status)
                    if !task.thread.isEmpty { LabeledContent("Thread", value: task.thread) }
                }
                if !task.steps.isEmpty {
                    Section("Steps · \(task.steps.filter(\.done).count)/\(task.steps.count)") {
                        ForEach(task.steps, id: \.n) { s in
                            Label(s.text, systemImage: s.done ? "checkmark.circle.fill" : "circle")
                                .foregroundStyle(s.done ? theme.muted : theme.ink)
                        }
                    }
                }
            }
            .navigationTitle("\(task.ref) · \(task.title)")
            .navigationBarTitleDisplayMode(.inline)
        }
    }
}

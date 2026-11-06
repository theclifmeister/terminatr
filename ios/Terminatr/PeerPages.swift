import QuickLook
import SwiftUI
import TerminatrKit
import TerminatrUI

/// A paired server's shared project (T846, T886), drawn as one of this
/// server's (ProjectScreen): its header, its board's counts as
/// StatTiles, its coordinator, running there or not, its pages as
/// one PageRow, one line of its designs under way, then its threads
/// (SidebarTree.sessions). The
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
                let sessions = p.map { SidebarTree.sessions(of: $0, alias: alias) } ?? []
                let threads = sessions.filter { !$0.ref.isCoordinator }
                ForEach(sessions.filter(\.ref.isCoordinator), id: \.ref) { e in
                    let route = Route.session(e.ref)
                    PlainRouteLink(value: route) {
                        LeadRow(e.ref.role ?? .coordinator, state: .of(peer: e.session), selected: navigate.opens(route, on: model))
                    }
                    .buttonStyle(.plain)
                }
                stats
                if let peer, let p {
                    pageTiles(server: PeerPages.server(peer))
                        .designsLine(model.listsDesigns ? DesignsSource(alias: alias, project: p, generation: model.generation) : nil,
                                     opens: .peerPage(alias: alias, project: project, page: .designs))
                    SectionHeading("Threads", detail: threads.isEmpty ? "" : "\(threads.count)")
                    if threads.isEmpty {
                        notShared("No threads")
                    } else {
                        tiles(threads)
                    }
                }
            }
            .padding(.horizontal, Spacing.gutter)
            .padding(.vertical, Spacing.gutter)
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
        .screenBar(bare: true) { EmptyView() }
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
        return PlainRouteLink(value: Route.peerPage(alias: alias, project: project, page: .board)) {
            StatStrip(PeerPages.stats(counts).map { (value: $0.0.map { "\($0)" } ?? "–", word: $0.1) }, widest: .infinity, radius: Radius.tile)
        }
        .buttonStyle(.plain)
        .accessibilityHint("Opens the board")
    }

    /// The pages, as a project of this server's shows them: Memory and
    /// Library as its tm reads those.
    private func pageTiles(server: String) -> some View {
        let tiles: [(PeerPages.Page, FlatGlyph)] = [
            (.overview, .info), (.board, .board), (.memory, .mark), (.library, .books),
        ] + (model.listsDesigns ? [(.designs, .designs)] : [])
        return PageRow {
            ForEach(Array(tiles.enumerated()), id: \.element.0) { i, tile in
                let (page, glyph) = tile
                let route = Route.peerPage(alias: alias, project: project, page: page)
                PlainRouteLink(value: route) { PageTile(page.rawValue, glyph: glyph, selected: navigate.opens(route, on: model), rule: i > 0) }
                    .buttonStyle(.plain)
            }
        }
    }

    private func tiles(_ sessions: [(session: AppAPI.PeerSession?, ref: SessionRef, task: String, title: String)]) -> some View {
        TileGroup {
            ForEach(Array(sessions.enumerated()), id: \.element.ref) { i, e in
                let route = Route.session(e.ref)
                PlainRouteLink(value: route) {
                    SessionTile(e.ref.role?.word ?? (e.title.isEmpty ? e.ref.thread : e.title),
                                ref: e.task.isEmpty ? nil : e.task, role: e.ref.role, state: .of(peer: e.session), rule: i > 0,
                                selected: navigate.opens(route, on: model))
                }
                .buttonStyle(.plain)
            }
        }
    }

    private func notShared(_ text: String) -> some View {
        Text(text)
            .font(theme.sized(Flat.text))
            .foregroundStyle(theme.muted)
            .padding(13)
            .frame(maxWidth: .infinity, alignment: .leading)
            .surface(in: RoundedRectangle(cornerRadius: Radius.tile))
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
                DesignsPage(source: model.peers.first(where: { $0.alias == alias })?.projects.first(where: { $0.project == project })
                    .map { DesignsSource(alias: alias, project: $0, generation: model.generation) },
                    route: .peerPage(alias: alias, project: project, page: .designs))
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
        .pageBar(page.rawValue)
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
    @Environment(\.theme) private var theme
    let pages: AppAPI.PeerPages
    let alias: String
    let project: String

    var body: some View {
        ScrollView {
            if let p = model.peers.first(where: { $0.alias == alias })?.projects.first(where: { $0.project == project }) {
                OverviewView(ProjectOverview(pages, project: p))
                    .font(theme.sized(Flat.text))
                    .padding(Spacing.gutter)
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
        PanelPage {
            let filtering = !filter.trimmingCharacters(in: .whitespaces).isEmpty
            let deck = Dashboard.filtered(PeerPages.onDeck(pages), by: filter)
            let moving = Dashboard.filtered(PeerPages.moving(pages), by: filter)
            let backlog = Dashboard.filtered(PeerPages.backlog(pages), by: filter)
            let done = Dashboard.filtered(PeerPages.done(pages), by: filter)
            if !deck.isEmpty {
                SectionHeading("On deck · \(deck.count)")
                PanelList { rows(deck) }
            }
            if !moving.isEmpty {
                SectionHeading("In motion · \(moving.count)")
                PanelList { rows(moving) }
            }
            if !backlog.isEmpty || !done.isEmpty {
                PanelList {
                    if !backlog.isEmpty {
                        FoldRow(title: "Backlog · \(backlog.count)", open: fold("backlog", filtering))
                        if fold("backlog", filtering).wrappedValue { rows(backlog) }
                    }
                    if !done.isEmpty {
                        FoldRow(title: "Done · \(done.count)", open: fold("done", filtering))
                        if fold("done", filtering).wrappedValue { rows(done) }
                    }
                }
                .padding(.top, Spacing.item)
            }
            if deck.isEmpty && moving.isEmpty && backlog.isEmpty && done.isEmpty {
                Text(filtering ? "No task on the board matches “\(filter.trimmingCharacters(in: .whitespaces))”." : "Nothing on the board.")
                    .font(theme.sized(Flat.text))
                    .foregroundStyle(theme.muted)
            }
            if pages.truncated {
                Text("The board was too long to cross whole: some done tasks stay there.")
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.muted)
            }
        }
        // In the screen, under its title (T928, FilterField).
        .safeAreaInset(edge: .top, spacing: 0) { FilterField(prompt: "Filter by id or title", text: $filter) }
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
                ListRow {
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        Text(t.ref)
                            .font(theme.monoSized(12.5))
                            .foregroundStyle(theme.muted)
                            .frame(minWidth: 34 * theme.scale, alignment: .leading)
                        Text(t.title)
                            .font(theme.sized(Flat.text))
                            .foregroundStyle(theme.ink)
                            .lines(2)
                        Spacer(minLength: 4)
                        if !t.steps.isEmpty {
                            Text("\(t.steps.filter(\.done).count)/\(t.steps.count)")
                                .font(theme.monoSized(12.5))
                                .foregroundStyle(theme.muted)
                        }
                        if t.status == "done" {
                            FinishedTag(task: t)
                        }
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

import SwiftUI
import TerminatrKit
import TerminatrUI

/// The projects as the TUI's and the Mac's sidebar list them
/// (SidebarTree): this server's active ones, pinned first; each paired
/// server's shared ones; and the inactive ones folded under one quiet
/// row. Each is a ProjectCard (T846) under the screen's header, and
/// opens its coordinator and threads.
struct ProjectsScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    @Environment(\.navigate) private var navigate
    @State private var showInactive = false

    var body: some View {
        let gone = model.inactiveProjects
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                ScreenHeader("Projects", subtitle: subtitle(inactive: gone.count), symbol: symbol) {
                    SettingsButton().buttonStyle(.circle)
                }
                LazyVStack(alignment: .leading, spacing: Spacing.item) {
                    if let f = model.failure { Failure(text: f) }
                    if !model.peers.isEmpty { SectionHeading("This server") }
                    ForEach(model.activeProjects, id: \.slug) { card($0) }
                    ForEach(model.peers, id: \.alias) { peer in
                        SectionHeading(peer.name.isEmpty ? peer.alias : peer.name, detail: PeerTag.of(peer).word)
                        ForEach(peer.projects, id: \.project) { p in
                            let sessions = SidebarTree.sessions(of: p, alias: peer.alias)
                            let coordinator = sessions.first { $0.ref.isCoordinator }?.session ?? nil
                            let route = Route.peerProject(alias: peer.alias, project: p.project)
                            PlainRouteLink(value: route) {
                                ProjectCard(name: p.project, coordinator: .of(peer: coordinator), needs: p.stale ? 0 : p.needsYou,
                                            threads: sessions.filter { !$0.ref.isCoordinator }.map { Attention.of(peer: $0.session).tone },
                                            dim: peer.state != "up" || p.stale, selected: navigate?.open == route)
                            }
                            .buttonStyle(.plain)
                        }
                    }
                    if !gone.isEmpty {
                        InactiveFold(count: gone.count, names: gone.map(\.slug), open: $showInactive)
                        if showInactive {
                            ForEach(gone, id: \.slug) { card($0, dim: true) }
                        }
                    }
                }
                .padding(.horizontal, Spacing.gutter)
                .padding(.bottom, Spacing.gutter)
            }
        }
        .refreshable { await model.refresh() }
        .keepsPlace("projects")
        .kept($showInactive, "inactive")
        .navigationTitle("Projects")
        // The header has the title and the Settings button.
        .toolbar(.hidden, for: .navigationBar)
        .backdrop()
    }

    private func card(_ p: AppAPI.Project, dim: Bool = false) -> some View {
        let route = Route.project(p.slug)
        return PlainRouteLink(value: route) {
            ProjectCard(name: p.slug, coordinator: .of(coordinator: model.coordinatorSession(p.slug), in: p),
                        needs: Need.count(model.needs, project: p.slug),
                        threads: SidebarTree.threads(of: p) { model.session($0) }.map { model.attention($0).tone },
                        pinned: p.pinned, dim: dim, selected: navigate?.open == route)
        }
        .buttonStyle(.plain)
    }

    /// "studio · 2 active · 1 inactive".
    private func subtitle(inactive: Int) -> String {
        let source = model.source == .demo ? "Demo" : model.serverName
        return [source, "\(model.activeProjects.count) active", inactive > 0 ? "\(inactive) inactive" : nil]
            .compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · ")
    }

    private var symbol: String {
        model.source == .demo ? "sparkles" : model.linkWords.0
    }
}

/// The inactive projects folded into one quiet row on glass: "Inactive
/// · old-prototype" (or how many), its chevron turned while open.
private struct InactiveFold: View {
    @Environment(\.theme) private var theme
    @Environment(\.accessibilityReduceMotion) private var still
    let count: Int
    let names: [String]
    @Binding var open: Bool

    var body: some View {
        Button {
            withAnimation(still ? nil : .snappy) { open.toggle() }
        } label: {
            HStack {
                Text("Inactive · " + (count == 1 ? names[0] : "\(count)"))
                    .font(theme.body(.subheadline, .medium))
                    .foregroundStyle(theme.muted)
                    .lines(1)
                Spacer(minLength: Spacing.item)
                Image(systemName: "chevron.right")
                    .font(theme.body(.footnote, .semibold))
                    .foregroundStyle(theme.muted)
                    .rotationEffect(.degrees(open ? 90 : 0))
            }
            .padding(.horizontal, Spacing.card + 2)
            .frame(minHeight: tapTarget + 4)
            .surface(.glass, in: RoundedRectangle(cornerRadius: Radius.tile, style: .continuous))
            .contentShape(RoundedRectangle(cornerRadius: Radius.tile, style: .continuous))
        }
        .buttonStyle(.plain)
        .hoverEffect()
        .accessibilityLabel("Inactive projects, \(count)")
        .accessibilityValue(open ? "shown" : "hidden")
        .accessibilityHint(open ? "Hides them" : "Shows them")
    }
}

/// One of this server's projects (T846): its monogram, name and goal,
/// its board's counts as StatTiles, its coordinator, running or not,
/// with its context use (at the write level Start Coordinator under it;
/// held, Save & Clear, T868), its pages as PageTiles, its designs as
/// DesignTiles (DesignsTiles), then every unresolved thread, done ones
/// too, in the TUI's order (SidebarTree.threads), as SessionTiles, a
/// thread's context use only when nearly full. An inactive project's
/// rows are dormant.
struct ProjectScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    @Environment(\.navigate) private var navigate
    let slug: String
    @State private var clearing = false

    var body: some View {
        ScrollView {
            if let p = model.project(slug) {
                let dormant = !p.active
                let threads = SidebarTree.threads(of: p) { model.session($0) }
                LazyVStack(alignment: .leading, spacing: Spacing.row + 2) {
                    ProjectHeader(slug, goal: p.goal)
                    stats(p)
                    TileGroup {
                        let c = model.coordinatorSession(slug)
                        let route = Route.session(SessionRef(project: slug))
                        PlainRouteLink(value: route) {
                            SessionTile("Coordinator", state: .of(coordinator: c, in: p), busy: Busy.of(coordinator: c, in: p),
                                        context: model.context(SessionRef(project: slug)), dormant: dormant,
                                        selected: navigate?.open == route)
                        }
                        .buttonStyle(.plain)
                        .contextMenu { SaveClearButton(project: slug, asking: $clearing) }
                    }
                    .asksSaveClear(slug, isPresented: $clearing)
                    if let clear = model.clears[slug] { SaveClearStatus(clear: clear) }
                    if p.active, model.coordinatorSession(slug) == nil, model.can(AppAPI.Methods.projectOpen.name) {
                        StartCoordinatorButton(project: slug)
                    }
                    pages
                    if model.listsDesigns {
                        DesignsTiles(project: p, sessions: model.sessions, generation: model.generation)
                    }
                    let waiting = threads.filter { model.attention($0).waits }.count
                    SectionHeading("Threads", detail: threads.isEmpty ? "" : "\(threads.count)" + (waiting > 0 ? " · \(waiting) wait on you" : ""))
                    if threads.isEmpty {
                        Text("No threads")
                            .font(theme.body(.subheadline))
                            .foregroundStyle(theme.muted)
                            .padding(Spacing.card)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .surface(.card, in: RoundedRectangle(cornerRadius: Radius.tile, style: .continuous))
                    } else {
                        TileGroup {
                            ForEach(Array(threads.enumerated()), id: \.element.record.id) { i, t in
                                let route = Route.session(SessionRef(project: slug, thread: t.record.id))
                                PlainRouteLink(value: route) {
                                    SessionTile(t.record.title, ref: t.task?.ref, state: model.attention(t), busy: model.busy(t),
                                                pr: t.pr, context: ContextUse.of(model.session(t), hint: 0).flatMap { $0.level == .high ? $0 : nil }, dormant: dormant, rule: i > 0,
                                                selected: navigate?.open == route)
                                }
                                .buttonStyle(.plain)
                            }
                        }
                    }
                }
                .padding(.horizontal, Spacing.gutter)
                .padding(.bottom, Spacing.gutter)
            } else {
                ContentUnavailableView("No project \(slug)", systemImage: "folder.badge.questionmark")
                    .padding(.top, 60)
            }
        }
        .refreshable { await model.refresh() }
        .keepsPlace(Route.project(slug).listKey)
        .navigationTitle(slug)
        .headerTitle()
        .backdrop()
    }

    /// The board's counts, each opening the board: done only once the
    /// lists carry it.
    private func stats(_ p: AppAPI.Project) -> some View {
        TileRow {
            ForEach(Self.stats(p, done: model.listsDone), id: \.1) { n, word in
                PlainRouteLink(value: Route.board(slug)) { StatTile(n, word) }
                    .buttonStyle(.plain)
                    .accessibilityHint("Opens the board")
            }
        }
    }

    static func stats(_ p: AppAPI.Project, done: Bool) -> [(Int, String)] {
        [(Dashboard.onDeck(p).count, "on deck"), (Dashboard.backlog(p).count, "backlog")] + (done ? [(Dashboard.doneCount(p), "done")] : [])
    }

    /// The project's read-only pages, as the Mac's beside its dashboard:
    /// Memory and Library with a tm that reads them.
    private var pages: some View {
        let pages = model.pages
        let tiles: [(Route, String, String)] = [
            (.overview(slug), "Overview", "info.circle"),
            (.board(slug), "Board", "list.bullet.rectangle"),
        ] + (pages.contains(.memory) ? [(.memory(slug), "Memory", "brain")] : [])
            + (pages.contains(.library) ? [(.library(slug), "Library", "books.vertical")] : [])
        return TileRow {
            ForEach(tiles, id: \.1) { route, title, symbol in
                PlainRouteLink(value: route) { PageTile(title, symbol: symbol, selected: navigate?.open == route) }
                    .buttonStyle(.plain)
            }
        }
    }
}

extension View {
    /// A screen with its own header in its content (T846): the bar keeps
    /// its buttons, and the screen's title for the back button and
    /// VoiceOver, but shows no title of its own.
    func headerTitle() -> some View {
        navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .principal) { Color.clear.frame(width: 1, height: 1).accessibilityHidden(true) }
            }
    }
}

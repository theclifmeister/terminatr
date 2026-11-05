import SwiftUI
import TerminatrKit
import TerminatrUI

/// The projects as the TUI's and the Mac's sidebar list them
/// (SidebarTree): this server's active ones, pinned first; each paired
/// server's shared ones; and the inactive ones folded under one quiet
/// row. Each is a ProjectCard (T846), a group's in one ProjectPanel (T996),
/// under the screen's header, and opens its coordinator and threads. With several servers (T852) each
/// server's come under its name, in the order they were paired; one
/// the phone pairs with directly isn't listed again under another that
/// links to it (PhoneModel.shownPeers).
struct ProjectsScreen: View {
    @Environment(Phone.self) private var phone
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    /// The servers whose inactive projects are shown, by key.
    @State private var showInactive: Set<String> = []

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                ScreenHeader("Projects", subtitle: subtitle, symbol: symbol) {
                    SettingsButton().buttonStyle(.smallCircle)
                }
                LazyVStack(alignment: .leading, spacing: Spacing.item) {
                    ForEach(phone.models, id: \.key) { m in
                        ServerProjects(several: phone.several, showInactive: Binding {
                            showInactive.contains(m.key)
                        } set: {
                            if $0 { showInactive.insert(m.key) } else { showInactive.remove(m.key) }
                        })
                        .environment(m)
                    }
                }
                .padding(.horizontal, Spacing.gutter)
                .padding(.bottom, Spacing.gutter)
            }
        }
        .refreshable { await phone.refresh() }
        .keepsPlace("projects")
        .kept($showInactive, "inactive")
        .navigationTitle("Projects")
        // The header has the title and the Settings button.
        .toolbar(.hidden, for: .navigationBar)
        .backdrop()
    }

    /// "studio · 2 active · 1 inactive", or "2 servers · 5 active".
    private var subtitle: String {
        let active = phone.models.reduce(0) { $0 + $1.activeProjects.count }
        let inactive = phone.models.reduce(0) { $0 + $1.inactiveProjects.count }
        let source = model.source == .demo ? "Demo" : phone.several ? "\(phone.models.count) servers" : model.serverName
        return [source, "\(active) active", inactive > 0 && !phone.several ? "\(inactive) inactive" : nil]
            .compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: " · ")
    }

    private var symbol: String {
        if model.source == .demo { return "sparkles" }
        if phone.several { return phone.models.allSatisfy { $0.link == .up } ? "checkmark.circle.fill" : "antenna.radiowaves.left.and.right" }
        return model.linkWords.0
    }
}

/// One server's projects on the Projects screen: under its name when
/// the phone shows several, then its own, its paired servers' and its
/// inactive ones folded.
private struct ServerProjects: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.navigate) private var navigate
    let several: Bool
    @Binding var showInactive: Bool

    var body: some View {
        let gone = model.standaloneInactive
        let peers = model.shownPeers
        if several {
            SectionHeading(model.serverName, detail: model.link == .up ? "" : model.linkWords.1)
                .padding(.top, Spacing.tight)
        }
        if let f = model.failure { Failure(text: f) }
        if !several, !peers.isEmpty { SectionHeading("This server") }
        let entries = model.projectEntries
        if !entries.isEmpty {
            ProjectPanel {
                ForEach(Array(entries.enumerated()), id: \.element.slug) { i, e in
                    switch e {
                    case .project(let p): card(p, rule: i > 0)
                    case .program(let g): programCard(g, rule: i > 0)
                    }
                }
            }
        }
        ForEach(peers, id: \.alias) { peer in
            SectionHeading(peer.name.isEmpty ? peer.alias : peer.name, detail: PeerTag.of(peer).word)
            let projects = ProgramList.standalone(peer, programs: model.programs)
            if !projects.isEmpty {
                ProjectPanel {
                    ForEach(Array(projects.enumerated()), id: \.element.project) { i, p in
                        let sessions = SidebarTree.sessions(of: p, alias: peer.alias)
                        let coordinator = sessions.first { $0.ref.isCoordinator }?.session ?? nil
                        let route = Route.peerProject(alias: peer.alias, project: p.project)
                        PlainRouteLink(value: route) {
                            ProjectCard(name: p.project, coordinator: .of(peer: coordinator), needs: p.stale ? 0 : p.needsYou,
                                        threads: sessions.filter { !$0.ref.isCoordinator }.map { Attention.of(peer: $0.session).tone },
                                        dim: peer.state != "up" || p.stale, selected: navigate.opens(route, on: model), rule: i > 0)
                        }
                        .buttonStyle(.plain)
                    }
                }
            }
        }
        if !gone.isEmpty {
            InactiveFold(count: gone.count, names: gone.map(\.slug), open: $showInactive)
            if showInactive {
                ProjectPanel {
                    ForEach(Array(gone.enumerated()), id: \.element.slug) { i, p in card(p, dim: true, rule: i > 0) }
                }
            }
        }
    }

    /// A program: one row, with one count of what needs the user in it.
    /// Its projects, a paired one too, are listed only on the program's
    /// page (the user's decision, T977), not here and not in a server's
    /// section. The badge counts the program's needs once: it counts the
    /// one need list, not the rows.
    private func programCard(_ g: AppAPI.Program, rule: Bool) -> some View {
        let route = Route.program(g.slug)
        return PlainRouteLink(value: route) {
            ProgramCard(program: g, director: .of(director: model.directorSession(g), questions: g.questions),
                        needs: model.programNeeds(g).count, threads: model.programThreads(g),
                        selected: navigate.opens(route, on: model), rule: rule)
        }
        .buttonStyle(.plain)
    }

    private func card(_ p: AppAPI.Project, dim: Bool = false, rule: Bool) -> some View {
        let route = Route.project(p.slug)
        return PlainRouteLink(value: route) {
            ProjectCard(name: p.slug, coordinator: model.coordinator(of: p),
                        needs: Need.count(model.needs, project: p.slug),
                        threads: SidebarTree.threads(of: p) { model.session($0) }.map { model.attention($0).tone },
                        pinned: p.pinned, dim: dim, selected: navigate.opens(route, on: model), rule: rule)
        }
        .buttonStyle(.plain)
    }
}

/// The inactive projects folded into one quiet plain row: "Inactive
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
                    .font(theme.sized(Flat.label))
                    .foregroundStyle(theme.muted)
                    .lines(1)
                Spacer(minLength: Spacing.item)
                Image(systemName: "chevron.right")
                    .font(theme.body(.footnote, .semibold))
                    .foregroundStyle(theme.muted)
                    .rotationEffect(.degrees(open ? 90 : 0))
            }
            .frame(minHeight: tapTarget)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .hoverEffect()
        .accessibilityLabel("Inactive projects, \(count)")
        .accessibilityValue(open ? "shown" : "hidden")
        .accessibilityHint(open ? "Hides them" : "Shows them")
    }
}

/// One of this server's projects (T846): its monogram, name and goal,
/// its coordinator as the screen's main row (LeadRow, T1001), running or
/// not, with its context use (held, Save & Clear, T868), its board's counts
/// as a StatStrip (at the write level Start Coordinator under them), its
/// pages as one PageRow, its designs as DesignTiles (DesignsTiles), then
/// every unresolved thread, done ones too, in the TUI's order
/// (SidebarTree.threads), as SessionTiles, a thread's context use only when
/// nearly full. An inactive project's rows are dormant.
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
                    let c = model.coordinatorSession(slug)
                    let route = Route.session(SessionRef(project: slug))
                    PlainRouteLink(value: route) {
                        LeadRow(.coordinator, state: model.coordinator(of: p), busy: Busy.of(coordinator: c, in: p),
                                context: model.context(SessionRef(project: slug)), dormant: dormant,
                                selected: navigate.opens(route, on: model))
                    }
                    .buttonStyle(.plain)
                    .contextMenu { SaveClearButton(ref: SessionRef(project: slug), asking: $clearing) }
                    .asksSaveClear(SessionRef(project: slug), isPresented: $clearing)
                    stats(p)
                    if let clear = model.clears[slug] { SaveClearStatus(clear: clear) }
                    if p.active, model.coordinatorSession(slug) == nil, model.sleep(of: slug) == .awake, model.can(AppAPI.Methods.projectOpen.name) {
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
                            .font(theme.sized(Flat.text))
                            .foregroundStyle(theme.muted)
                            .padding(13)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .surface(in: RoundedRectangle(cornerRadius: Radius.tile))
                    } else {
                        TileGroup {
                            ForEach(Array(threads.enumerated()), id: \.element.record.id) { i, t in
                                let route = Route.session(SessionRef(project: slug, thread: t.record.id))
                                PlainRouteLink(value: route) {
                                    SessionTile(t.record.title, ref: t.task?.ref, state: model.attention(t), busy: model.busy(t),
                                                pr: t.pr, context: ContextUse.of(model.session(t), hint: 0).flatMap { $0.level == .high ? $0 : nil }, dormant: dormant, rule: i > 0,
                                                selected: navigate.opens(route, on: model))
                                }
                                .buttonStyle(.plain)
                            }
                        }
                    }
                }
                .padding(.horizontal, Spacing.gutter)
                .padding(.vertical, Spacing.gutter)
            } else {
                ContentUnavailableView("No project \(slug)", systemImage: "folder.badge.questionmark")
                    .padding(.top, 60)
            }
        }
        .refreshable { await model.refresh() }
        .keepsPlace(route: Route.project(slug))
        .navigationTitle(slug)
        // Back on the screen's own bar, in line with the content's edges
        // (T925).
        .screenBar(bare: true) { EmptyView() }
        .backdrop()
    }

    /// The board's counts, opening the board: done only once the lists
    /// carry it.
    private func stats(_ p: AppAPI.Project) -> some View {
        PlainRouteLink(value: Route.board(slug)) {
            StatStrip(Self.stats(p, done: model.listsDone).map { (value: "\($0.0)", word: $0.1) }, widest: .infinity, radius: Radius.tile)
        }
        .buttonStyle(.plain)
        .accessibilityHint("Opens the board")
    }

    static func stats(_ p: AppAPI.Project, done: Bool) -> [(Int, String)] {
        [(Dashboard.onDeck(p).count, "on deck"), (Dashboard.backlog(p).count, "backlog")] + (done ? [(Dashboard.doneCount(p), "done")] : [])
    }

    /// The project's pages, as the Mac's beside its dashboard: Memory and
    /// Library with a tm that reads them, Mods with one that lists them.
    private var pages: some View {
        let pages = model.pages
        let tiles: [(Route, String, String)] = [
            (.overview(slug), "Overview", "info.circle"),
            (.board(slug), "Board", "list.bullet.rectangle"),
        ] + (pages.contains(.memory) ? [(.memory(slug), "Memory", "bookmark")] : [])
            + (pages.contains(.library) ? [(.library(slug), "Library", "books.vertical")] : [])
            + (pages.contains(.mods) ? [(.mods(slug), "Mods", "puzzlepiece.extension")] : [])
        return PageRow {
            ForEach(Array(tiles.enumerated()), id: \.element.1) { i, tile in
                let (route, title, symbol) = tile
                PlainRouteLink(value: route) { PageTile(title, symbol: symbol, selected: navigate.opens(route, on: model), rule: i > 0) }
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

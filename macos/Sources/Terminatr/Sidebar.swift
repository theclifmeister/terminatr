import SwiftUI
import TerminatrKit
import TerminatrUI

/// The window's sidebar (design T746; edge to edge, to the flat look's
/// reference since T1000): the app's name and its buttons; the rows of the
/// TUI's sidebar (SidebarTree, T762): this server's active projects,
/// pinned ones first, each with its coordinator's state as the dot and its
/// needs as the badge, and under it its coordinator, running or not, and
/// every unresolved thread, done ones too, in the dashboard's order, with
/// [ui] needs_first the projects that need the user first; then each
/// paired server's shared projects, its coordinator and its unresolved
/// threads, running there or not (T768); and at the bottom the inactive
/// ones folded under "Inactive · n" (⇧⌘H unfolds them), each folded until
/// → expands it, dormant. A project shows its pages, a coordinator or
/// thread its session. Each section and project folds, and stays so across
/// launches. Every project and session row has its state as a dot, and
/// what waits on the user (allow?, asks, …) as a filled chip (T752).
/// Under the rows, in a section of its own, what needs the user (the
/// app's own: the TUI has its ⚑ and W instead); a need shows what it
/// names. At the foot, Settings and Pair a server. The rows are plain
/// views, not a List's, so they keep the reference's grid and no system
/// selection ring: ↑ ↓ move the selection, letters typed select the row
/// they begin, ← folds a project (or goes up to it from its session) and
/// → unfolds it. ⌥⌘F opens a filter over the rows, which keeps those that
/// match it, every one of them.
struct Sidebar: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    @Environment(\.density) private var density
    @Environment(SectionFocusOwner.self) private var sections
    /// Where the window's needs rotor finds the need rows.
    let needsRotor: Namespace.ID
    @State private var typing = TypeSelect()
    /// The rows have the keys from launch, not the filter field.
    @FocusState private var listFocused: Bool
    /// ⌥⌘F opened the filter; it stays while it has words.
    @State private var filterOpen = false

    var body: some View {
        GeometryReader { g in
            VStack(spacing: 0) {
                head
                if filterOpen || filtering {
                    FilterField {
                        filterOpen = false
                        listFocused = true
                    }
                    .padding(.horizontal, 8)
                    .padding(.bottom, 6)
                }
                ScrollViewReader { proxy in
                    ScrollView {
                        tree
                            .padding(.bottom, 8)
                    }
                    .scrollEdges()
                    .focusable()
                    .focusEffectDisabled()
                    .focused($listFocused)
                    .onAppear { listFocused = true }
                    .onKeyPress(keys: [.upArrow, .downArrow]) { press in
                        step(press.key == .downArrow ? 1 : -1) ? .handled : .ignored
                    }
                    .onKeyPress(keys: [.leftArrow, .rightArrow]) { press in
                        fold(open: press.key == .rightArrow) ? .handled : .ignored
                    }
                    .onKeyPress(characters: .alphanumerics, phases: .down) { press in
                        typeSelect(press)
                    }
                    .onChange(of: app.nav.selection) { _, s in
                        if let s { proxy.scrollTo(s) }
                    }
                    .onChange(of: app.serverReveal) { _, r in
                        // After the pass that lays out what it unfolded.
                        DispatchQueue.main.async { proxy.scrollTo(r.key, anchor: .top) }
                    }
                }
                .frame(maxHeight: .infinity)
                theme.lineSoft.frame(height: 1)
                needsSection
                    .frame(height: needsOpen ? CGFloat(WindowColumns.needsHeight(sidebar: g.size.height)) - 1 : 36)
                theme.lineSoft.frame(height: 1)
                foot
            }
        }
        .background(theme.chrome)
        .onChange(of: app.filterFocus) { filterOpen = true }
        .onChange(of: sections.ask?.id) {
            if sections.ask?.section == .sidebar { listFocused = true }
        }
    }

    // MARK: the head

    /// The app's mark and name, right of the window's buttons; Search
    /// (⌘K) and the sidebar's fold.
    private var head: some View {
        ZStack(alignment: .topLeading) {
            WindowDragArea()
            HStack(spacing: 10) {
                PromptMark(lineWidth: 2.2)
                    .frame(width: 13, height: 13)
                    .foregroundStyle(theme.onAction)
                    .frame(width: 20, height: 20)
                    .background(theme.action, in: RoundedRectangle(cornerRadius: 5))
                    .accessibilityHidden(true)
                Text("terminatr")
                    .font(theme.sized(15, .medium))
                    .lineLimit(1)
                Spacer(minLength: 4)
                IconButton(symbol: "magnifyingglass", name: "Search", help: "Search projects, threads, tasks and needs (⌘K)") { app.palette.toggle() }
                    .padding(.trailing, 5)
                IconButton(symbol: "sidebar.left", name: "Hide the sidebar", help: "Hide the sidebar (⌃⌘S)") { app.toggleSidebar() }
                    .padding(.trailing, 16)
            }
            .padding(.leading, 84)
            .padding(.top, 15)
        }
        .frame(height: 58, alignment: .top)
    }

    // MARK: the rows

    private var tree: some View {
        // Not lazy: a paired member lists under its program and under its
        // server, one id twice, and a lazy stack drops one of them.
        VStack(alignment: .leading, spacing: 0) {
            let active = shown(app.entries)
            if !filtering || !active.isEmpty {
                SectionHead(first: true, open: expanded(Navigation.sectionKey(server: ""))) {
                    // "This server" only tells it from paired ones.
                    HStack(spacing: 6) {
                        Text(app.peers.isEmpty ? "Projects" : "This server")
                        // Folded over a session that works (T985).
                        if !filtering, app.nav.collapsed.contains(Navigation.sectionKey(server: "")), localBusy {
                            BusyMark(size: 6).frame(width: 6, height: 6)
                        }
                    }
                }
                .id(Navigation.sectionKey(server: ""))
                if expanded(Navigation.sectionKey(server: "")).wrappedValue {
                    ForEach(active, id: \.slug) { e in
                        switch e {
                        case .project(let p): project(p)
                        case .program(let g): program(g)
                        }
                    }
                }
            }
            ForEach(app.peers, id: \.alias) { peer in
                let projects = shown(peer)
                let open = expanded(Navigation.sectionKey(server: peer.alias))
                if !filtering || !projects.isEmpty {
                    SectionHead(first: false, open: open) {
                        remoteHeader(peer)
                    }
                    .id(Navigation.sectionKey(server: peer.alias))
                    if open.wrappedValue {
                        ForEach(projects, id: \.project) { p in
                            remote(peer, p)
                        }
                    }
                }
            }
            // At the bottom, after the paired servers, as the TUI pins it.
            let inactive = shown(app.inactiveProjects)
            if !inactive.isEmpty {
                let open = filtering || app.nav.showInactive
                Button {
                    app.nav.showInactive.toggle()
                } label: {
                    Text("Inactive · \(inactive.count)")
                        .font(theme.sized(13.5))
                        .foregroundStyle(theme.muted)
                        .padding(.leading, 16)
                        .frame(maxWidth: .infinity, minHeight: 32, alignment: .leading)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .help("Projects set inactive: View › Show Inactive Projects (⇧⌘H)")
                .accessibilityValue(open ? "expanded" : "collapsed")
                if open {
                    ForEach(inactive, id: \.slug) { p in
                        project(p)
                    }
                }
            }
            if filtering, rows.isEmpty, shownNeeds.isEmpty {
                Text("Nothing matches “\(app.filter)”")
                    .font(theme.sized(13))
                    .foregroundStyle(theme.muted)
                    .padding(.horizontal, 17)
            }
        }
    }

    // MARK: the lower section

    /// One row, folded by default (T1017): "Needs you" and its count, a
    /// click unfolds what needs the user under it, another folds it, and
    /// the choice stays across launches. The count carries the attention
    /// so nothing goes unnoticed while it is folded.
    private var needsSection: some View {
        VStack(alignment: .leading, spacing: 0) {
            needsRow
            if needsOpen {
                let needs = shownNeeds
                if needs.isEmpty {
                    Text(filtering ? "No need matches." : "Nothing needs you. What waits for your answer shows here.")
                        .font(theme.sized(13))
                        .foregroundStyle(theme.muted)
                        .fixedSize(horizontal: false, vertical: true)
                        .padding(.horizontal, 13)
                    Spacer(minLength: 0)
                } else {
                    ScrollView {
                        VStack(alignment: .leading, spacing: 0) {
                            ForEach(needs) { n in
                                NeedLine(need: n) { app.open(project: n.project, session: n.session) }
                                    .accessibilityRotorEntry(id: n.id, in: needsRotor)
                            }
                        }
                    }
                    .scrollEdges()
                }
            }
        }
    }

    private var needsOpen: Bool { app.nav.needsOpen }

    /// The fold row, on the sidebar's grid: its chevron in the tile
    /// column, its text at the text edge, the count at the right.
    private var needsRow: some View {
        let count = app.needs.count
        return HStack(spacing: 0) {
            Button {
                app.nav.needsOpen.toggle()
            } label: {
                HStack(spacing: 0) {
                    Image(systemName: "chevron.down")
                        .font(.system(size: 9, weight: .semibold))
                        .rotationEffect(.degrees(needsOpen ? 0 : -90))
                        .frame(width: CGFloat(SidebarGrid.slot))
                        .padding(.leading, CGFloat(SidebarGrid.inset))
                        .accessibilityHidden(true)
                    Text("Needs you")
                        .font(theme.sized(13))
                        .padding(.leading, CGFloat(SidebarGrid.gap))
                    Spacer(minLength: 6)
                    needsCount(count)
                }
                .foregroundStyle(theme.muted)
                .frame(maxWidth: .infinity, alignment: .leading)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .help(needsOpen ? "Fold the list of what needs you" : "Show what needs you")
            .accessibilityElement(children: .ignore)
            .accessibilityLabel("Needs you, \(count == 0 ? "nothing waiting" : "\(count)")")
            .accessibilityValue(needsOpen ? "expanded" : "collapsed")
            .accessibilityAddTraits(.isHeader)
            IconButton(symbol: "clock", name: "Next need", help: "Next need (⌥⌘↓)") { app.stepNeed(1) }
                .disabled(app.needs.isEmpty)
                .padding(.leading, 6)
                .padding(.trailing, 16)
        }
        .frame(height: 36)
    }

    /// The count as the project rows' badge shows needs: the attention
    /// chip in the percent column (SidebarGrid); nothing at 0.
    private func needsCount(_ count: Int) -> some View {
        Group {
            if count > 0 {
                Text("\(count)")
                    .font(theme.sized(12, .medium))
                    .padding(.horizontal, 5)
                    .background(theme.attention, in: RoundedRectangle(cornerRadius: 4))
                    .foregroundStyle(theme.onAttention)
                    .fixedSize()
            } else {
                Color.clear.frame(height: 1)
            }
        }
        .frame(width: CGFloat(SidebarGrid.percent) * theme.scale, alignment: .trailing)
    }

    // MARK: the foot

    private var foot: some View {
        VStack(alignment: .leading, spacing: 0) {
            SettingsLink {
                HStack(spacing: 9) {
                    BarIcon(symbol: "gearshape")
                        .foregroundStyle(theme.muted)
                    Text("Settings")
                        .foregroundStyle(theme.ink)
                }
                .frame(height: 20)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .padding(.top, 10)
            Button { app.pairServer = true } label: {
                HStack(spacing: 9) {
                    BarIcon(symbol: "plus")
                    Text("Pair a server")
                }
                .foregroundStyle(theme.muted)
                .frame(height: 20)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .help("Pair another server with this one: tm peer pair and join")
            .padding(.top, 11)
            Spacer(minLength: 0)
        }
        .font(theme.sized(14))
        .padding(.leading, 24)
        .frame(height: 74, alignment: .topLeading)
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    // MARK: the filter

    private var filtering: Bool { !app.filter.trimmingCharacters(in: .whitespaces).isEmpty }

    private var shownNeeds: [Need] {
        filtering ? app.needs.filter { SidebarTree.matches(app.filter, $0.title, $0.detail) } : app.needs
    }

    /// Whether the filter finds a coordinator: by its word, as the TUI's.
    private var findsCoordinator: Bool { SidebarTree.matches(app.filter, "coordinator") }

    /// The projects the filter keeps: by name, or by a row it finds
    /// under them.
    private func shown(_ projects: [AppAPI.Project]) -> [AppAPI.Project] {
        guard filtering else { return projects }
        return projects.filter {
            findsCoordinator || SidebarTree.matches(app.filter, $0.slug) || !SidebarTree.threads(of: $0, matching: app.filter).isEmpty
        }
    }

    /// This server's entries the filter keeps: a project as shown(_:)
    /// keeps it, a program by its name, its director, or a member kept.
    private func shown(_ entries: [SidebarEntry]) -> [SidebarEntry] {
        guard filtering else { return entries }
        return entries.filter { e in
            switch e {
            case .project(let p): !shown([p]).isEmpty
            case .program(let g): SidebarTree.matches(app.filter, g.slug, "director") || !shownMembers(g).isEmpty
            }
        }
    }

    /// Whether a session of this server's entries works: what its
    /// section's header shows while it is folded.
    private var localBusy: Bool {
        app.entries.contains { e in
            switch e {
            case .project(let p): SidebarTree.busy(p, sessions: app.sessions)
            case .program(let g): SidebarTree.busy(g, projects: app.projects, sessions: app.sessions, peers: app.peers, watched: app.watched)
            }
        }
    }

    /// Program g's members as its rows list them, those the filter keeps
    /// while filtering.
    private func shownMembers(_ g: AppAPI.Program) -> [ProgramMemberRow] {
        let all = SidebarTree.memberRows(g, projects: app.projects, peers: app.peers)
        guard filtering, !SidebarTree.matches(app.filter, g.slug) else { return all }
        return all.filter { m in
            switch m {
            case .local(let p): !shown([p]).isEmpty
            case .paired(let alias, let slug, let peer, let p, _):
                SidebarTree.matches(app.filter, slug, alias) || (peer.flatMap { pe in p.map { !sessions(pe, $0).isEmpty } } ?? false)
            }
        }
    }

    /// A paired server's projects the filter keeps: by name or server,
    /// or by a session it finds.
    private func shown(_ peer: AppAPI.Peer) -> [AppAPI.PeerProject] {
        guard filtering else { return peer.projects }
        return peer.projects.filter { p in
            SidebarTree.matches(app.filter, p.project, peer.alias, peer.name) || !sessions(peer, p).isEmpty
        }
    }

    /// A paired server's project's rows under it (SidebarTree), with
    /// their sessions: those the filter finds, all of them when it finds
    /// the project.
    private func sessions(_ peer: AppAPI.Peer, _ p: AppAPI.PeerProject) -> [(session: AppAPI.PeerSession?, ref: SessionRef, task: String, title: String)] {
        let all = SidebarTree.sessions(of: p, alias: peer.alias, watched: app.watched)
        guard filtering, !SidebarTree.matches(app.filter, p.project, peer.alias, peer.name) else { return all }
        return all.filter { _, r, task, title in SidebarTree.matches(app.filter, task, title, r.thread, r.isCoordinator ? "coordinator" : "") }
    }

    /// What a project of this server lists under it (SidebarTree):
    /// whether its coordinator's row and its threads show, which
    /// threads, and whether they are dormant (an inactive project's,
    /// expanded: nothing of it runs). While filtering, what the filter
    /// finds, every project unfolded, as the TUI's.
    private func listing(_ p: AppAPI.Project, member: Bool = false) -> (open: Bool, coordinator: Bool, threads: [AppAPI.Thread], dormant: Bool) {
        if filtering {
            let found = SidebarTree.threads(of: p, matching: app.filter)
            return (!found.isEmpty || findsCoordinator, findsCoordinator, found, !p.active)
        }
        guard SidebarTree.opens(p, nav: app.nav, member: member) else { return (false, false, [], false) }
        let threads = SidebarTree.threads(of: p) { t in
            t.record.session.isEmpty ? nil : app.sessions.first { $0.id == t.record.session }
        }
        return (true, true, threads, !p.active)
    }

    // MARK: keys

    /// The rows the List can select, in its order, with what a letter
    /// typed matches: what type-to-select moves through.
    private var rows: [(Selection, String)] {
        var out: [(Selection, String)] = []
        func add(_ projects: [AppAPI.Project], member: Bool = false) {
            for p in projects {
                out.append((.project(p.slug), p.slug))
                let l = listing(p, member: member)
                if l.coordinator { out.append((.session(SessionRef(project: p.slug)), "Coordinator")) }
                out += l.threads.map { (.session(SessionRef(project: p.slug, thread: $0.record.id)), $0.record.title) }
            }
        }
        if !app.nav.collapsed.contains(Navigation.sectionKey(server: "")) {
            for e in shown(app.entries) {
                switch e {
                case .project(let p): add([p])
                case .program(let g):
                    out.append((.program(g.slug), g.slug))
                    guard filtering || SidebarTree.opens(g, nav: app.nav) else { continue }
                    out.append((.session(.director(g.slug)), "Director"))
                    for m in shownMembers(g) {
                        switch m {
                        case .local(let p): add([p], member: true)
                        case .paired(let alias, let slug, let peer, let p, _):
                            out.append((.project(slug, server: alias), slug))
                            if let peer, let p, SidebarTree.opens(member: alias, slug, of: g, nav: app.nav) {
                                out += sessions(peer, p).map { (.session($0.ref), $0.ref.isCoordinator ? "Coordinator" : $0.title.isEmpty ? $0.ref.thread : $0.title) }
                            }
                        }
                    }
                }
            }
        }
        for peer in app.peers where !app.nav.collapsed.contains(Navigation.sectionKey(server: peer.alias)) {
            for p in shown(peer) {
                out.append((.project(p.project, server: peer.alias), p.project))
                if !app.nav.collapsed.contains(Navigation.sectionKey(server: peer.alias, project: p.project)) {
                    out += sessions(peer, p).map { (.session($0.ref), $0.ref.isCoordinator ? "Coordinator" : $0.title.isEmpty ? $0.ref.thread : $0.title) }
                }
            }
        }
        if filtering || app.nav.showInactive { add(shown(app.inactiveProjects)) }
        return out
    }

    /// ↑ ↓ move the selection through the rows.
    private func step(_ by: Int) -> Bool {
        let rows = rows
        guard !rows.isEmpty else { return false }
        let i = rows.firstIndex { $0.0 == app.nav.selection }
        let to = i.map { $0 + by } ?? (by > 0 ? 0 : rows.count - 1)
        guard rows.indices.contains(to) else { return true }
        app.select(rows[to].0)
        return true
    }

    /// → unfolds the selected project, ← folds it, or from one of its
    /// sessions goes up to it.
    private func fold(open: Bool) -> Bool {
        switch app.nav.selection {
        case .program(let slug):
            if open {
                app.nav.collapsed.remove(Navigation.programKey(slug))
            } else {
                app.nav.collapsed.insert(Navigation.programKey(slug))
            }
            return true
        case .project(let slug, let server):
            let key = Navigation.sectionKey(server: server, project: slug)
            if open {
                app.nav.unfold(key)
            } else {
                app.nav.fold(key)
            }
            return true
        case .session(let ref) where !open:
            app.select(ref.director ? .program(ref.project) : .project(ref.project, server: ref.server))
            return true
        default:
            return false
        }
    }

    /// Letters typed select the next row that begins with them.
    private func typeSelect(_ press: KeyPress) -> KeyPress.Result {
        guard press.modifiers.isDisjoint(with: [.command, .control, .option]) else { return .ignored }
        let rows = rows
        let typed = typing.type(press.characters)
        let current = rows.firstIndex { $0.0 == app.nav.selection }
        if let i = TypeSelect.match(typed, in: rows.map(\.1), from: current) {
            app.select(rows[i].0)
        }
        return .handled
    }

    private func expanded(_ key: String) -> Binding<Bool> {
        Binding(
            get: { !app.nav.collapsed.contains(key) },
            set: { open in
                if open { app.nav.collapsed.remove(key) } else { app.nav.collapsed.insert(key) }
            }
        )
    }

    // MARK: this server's

    /// A project's line, and under it, unless it is folded (an inactive
    /// one until → expands it), its coordinator and its threads, as the
    /// TUI's tree; while filtering, the rows the filter finds. An
    /// inactive project's are dormant: nothing of it runs.
    @ViewBuilder
    private func project(_ p: AppAPI.Project, indent: CGFloat = 0, member: Bool = false) -> some View {
        let sel = Selection.project(p.slug)
        let needs = app.needs(of: p.slug).count
        let state = app.coordinator(of: p)
        let sleep = app.sleep(of: p.slug)
        let l = listing(p, member: member)
        let coordinator = app.coordinatorSession(p.slug)
        // Its coordinator's work shows on the coordinator's row, or
        // folded as what the row hides: never as the dot too (T985).
        let dot = app.busy(coordinator: p) == nil ? state.tone : .idle
        ProjectRow(name: p.slug, dot: theme.colour(dot), state: state.word, needs: needs, selected: app.nav.selection == sel,
                   busy: !l.open && SidebarTree.busy(p, sessions: app.sessions))
            .padding(.leading, indent)
            .selectable(sel, session: false) { listFocused = true }
            .contextMenu { ProjectMenu(project: p, coordinator: coordinator != nil, divided: false) }
        if l.open {
            if l.coordinator {
                // Its context use, as the TUI's row: warn from [ui]
                // context_hint, high from 80% (with app API 13's hint).
                let ctx = l.dormant ? nil : ContextUse.of(coordinator, hint: app.contextHint)
                // Asleep (T980): the TUI's ☾ for its dot and its word;
                // waking, a spinner until it runs.
                let sleeps = !l.dormant && sleep != .awake
                row(.session(SessionRef(project: p.slug)), label: SessionRole.coordinator.word, ref: nil, role: .coordinator,
                    tag: state.waits && !l.dormant ? (state.word, state.tone) : sleeps ? (state.word, nil) : nil,
                    dot: theme.colour(l.dormant ? .idle : state.tone),
                    spoken: l.dormant ? "inactive" : sleeps ? state.why.replacingOccurrences(of: " · ", with: ", ") : state.spoken,
                    dormant: l.dormant, context: ctx,
                    busy: l.dormant ? nil : app.busy(coordinator: p), menu: coordinator == nil ? nil : p,
                    sleep: l.dormant ? .awake : sleep, indent: CGFloat(SidebarGrid.contents(of: Double(indent))))
            }
            // Keyed with the project: thread ids repeat across projects,
            // and the List flattens every project's rows into one.
            ForEach(l.threads, id: \.record.id) { t in
                row(.session(SessionRef(project: p.slug, thread: t.record.id)), label: t.record.title, ref: t.task?.ref,
                    tag: l.dormant ? nil : tag(t), dot: theme.colour(l.dormant ? .idle : app.attention(t).tone),
                    spoken: l.dormant ? "inactive" : spoken(t), dormant: l.dormant, busy: l.dormant ? nil : app.busy(t), indent: CGFloat(SidebarGrid.contents(of: Double(indent))))
            }
        }
    }

    /// A session's row on the sidebar's grid (SidebarGrid): its text at
    /// the depth's edge, after its role's mark in the tile's column for a
    /// coordinator or director, which reads in medium ink where a thread
    /// stays quiet; then its status words and tag: a filled pill in its
    /// tone's colour, or plain muted text (PR) when it has none. A dormant
    /// one (an inactive project's) is muted. A running coordinator's row
    /// has its context use in the percent column, and Save & Clear in its
    /// menu (menu, its project). A working one (busy) has the activity
    /// mark for its dot and how long its turn has run. A role's row keeps
    /// the percent column empty or not, and a thread's whenever it has
    /// words there, so they end at one edge whatever its state.
    private func row(_ sel: Selection, label: String, ref: String?, role: SessionRole? = nil, tag: (String, Attention.Tone?)?, dot: Color, spoken: String,
                     dormant: Bool = false, context: ContextUse? = nil, busy: Busy? = nil, menu: AppAPI.Project? = nil,
                     sleep: Sleep = .awake, indent: CGFloat = 0) -> some View {
        let on = app.nav.selection == sel
        let leads = role != nil && !dormant
        return HStack(spacing: 6) {
            // The tile's column: a role's mark, a thread's bubble.
            Group {
                if let role {
                    RoleMark(role, slot: CGFloat(SidebarGrid.slot), colour: leads ? theme.ink : theme.muted)
                } else {
                    Image(systemName: SidebarGrid.threadSymbol)
                        .font(.system(size: 11 * theme.scale, weight: .medium))
                        .foregroundStyle(theme.muted)
                        .accessibilityHidden(true)
                }
            }
            .frame(width: CGFloat(SidebarGrid.slot), height: 14)
            .padding(.trailing, CGFloat(SidebarGrid.gap) - 6)
            Text([ref, label].compactMap { $0 }.joined(separator: " "))
                .font(theme.sized(13, leads ? .medium : .regular))
                .lineLimit(1)
                .truncationMode(.tail)
            Spacer(minLength: 4)
            if let busy {
                BusyLabel(busy: busy, colour: on ? theme.ink : nil)
                    .fixedSize()
            }
            if let (word, tone) = tag {
                if let tone {
                    Text(word)
                        .font(theme.sized(11.5, .medium))
                        .padding(.horizontal, 5)
                        .padding(.vertical, 1)
                        .background(theme.colour(tone), in: RoundedRectangle(cornerRadius: 4))
                        .foregroundStyle(theme.on(tone))
                } else {
                    Text(word)
                        .font(theme.sized(12))
                        .foregroundStyle(theme.muted)
                }
            }
            if role != nil || busy != nil || tag != nil {
                Group {
                    if let context {
                        Text(context.word)
                            .font(theme.sized(12, context.tone == nil ? .regular : .semibold))
                            .monospacedDigit()
                            .foregroundStyle(context.tone.map { theme.colour($0) } ?? theme.muted)
                            .fixedSize()
                            .help(context.spoken)
                    } else {
                        Color.clear.frame(height: 1)
                    }
                }
                .frame(width: CGFloat(SidebarGrid.percent) * theme.scale, alignment: .trailing)
            }
            // Its state at the row's end, the same slot whatever it shows.
            Group {
                if sleep == .waking {
                    ProgressView().controlSize(.mini)
                } else if sleep == .asleep {
                    Image(systemName: "moon.zzz.fill")
                        .font(.system(size: 8, weight: .semibold))
                        .foregroundStyle(theme.muted)
                } else if busy != nil {
                    BusyMark(size: 7)
                } else {
                    Dot(colour: dot)
                }
            }
            .frame(width: 7, height: 7)
            .accessibilityHidden(true)
        }
        .padding(.leading, CGFloat(SidebarGrid.inset) + indent)
        .padding(.trailing, 8)
        .foregroundStyle(on || leads ? theme.ink : theme.muted)
        .help(sleep == .asleep ? Sleep.words : "")
        // One element that says the row and its state: "T6 Network popup
        // polish, needs approval".
        .accessibilityElement(children: .ignore)
        .accessibilityLabel([ref, label].compactMap { $0 }.joined(separator: " "))
        .accessibilityValue([busy.map { $0.spoken(now: Date()) } ?? spoken, context?.spoken].compactMap { $0 }.joined(separator: ", "))
        .selectable(sel, session: true) { listFocused = true }
        .contextMenu {
            if case .session(let r) = sel { NewWindowButton(ref: r) }
            if let menu { ProjectMenu(project: menu, coordinator: true) }
        }
    }

    /// What VoiceOver says of a thread after its name: what it waits
    /// on, else its state, and its PR.
    private func spoken(_ t: AppAPI.Thread) -> String {
        let a = app.attention(t).spoken
        return t.pr > 0 ? "\(a), PR \(t.pr)" : a
    }

    /// A word for what the thread waits on, as the mockup's: a dialog,
    /// a question, a review (Attention, as its card's); else its PR.
    private func tag(_ t: AppAPI.Thread) -> (String, Attention.Tone?)? {
        let a = app.attention(t)
        if a.waits { return (a.word, a.tone) }
        if t.pr > 0 { return ("PR", nil) }
        return nil
    }

    // MARK: a paired server's

    /// A paired server's project: its line as this server's projects'
    /// (ProjectRow), and under it, unless ← folded it, its coordinator,
    /// running there or not, and its threads, as the TUI's. Dimmed while
    /// its server is offline or its listing stale.
    /// Under its program (under, the program), it is named
    /// "alias/slug" and folds apart; under its server's group it is
    /// tagged with its program, if it is in one. Both rows come from the
    /// one peer.list entry and show the same project.
    @ViewBuilder
    private func remote(_ peer: AppAPI.Peer, _ p: AppAPI.PeerProject, under g: AppAPI.Program? = nil) -> some View {
        let sel = Selection.project(p.project, server: peer.alias)
        let dim = peer.state == "up" && !p.stale ? 1.0 : 0.55
        // The coordinator's state, as this server's projects have it.
        let coordinator = p.sessions.first { SessionRef($0, of: peer.alias)?.isCoordinator == true }
        let indent: CGFloat = g == nil ? 0 : CGFloat(SidebarGrid.step)
        let open = g.map { SidebarTree.opens(member: peer.alias, p.project, of: $0, nav: app.nav) }
            ?? !app.nav.collapsed.contains(Navigation.sectionKey(server: peer.alias, project: p.project))
        let progTag = g == nil ? app.pairedMembers["\(peer.alias)/\(p.project)"].map { "· \($0)" } ?? "" : ""
        ProjectRow(name: g == nil ? p.project : ProgramMemberRow.paired(alias: peer.alias, slug: p.project, peer: peer, project: p, state: "").name,
                   dot: coordinator.map { theme.colour(Busy.of(peer: $0) == nil ? tone($0) : .idle) } ?? theme.idle,
                   state: coordinator.map(spoken) ?? "not running", needs: p.stale ? 0 : p.needsYou,
                   selected: app.nav.selection == sel, tag: progTag,
                   busy: !open && !filtering && SidebarTree.busy(peer, p, watched: app.watched))
            .padding(.leading, indent)
            .selectable(sel, session: false) { listFocused = true }
            .opacity(dim)
            .contextMenu {
                if let g {
                    Button(open ? "Fold" : "Unfold") {
                        let key = Navigation.memberKey(program: g.slug, server: peer.alias, project: p.project)
                        if open { app.nav.fold(key) } else { app.nav.unfold(key) }
                    }
                }
            }
        if open || filtering {
            ForEach(sessions(peer, p), id: \.ref) { s, r, task, title in
                row(.session(r), label: r.role?.word ?? (title.isEmpty ? r.thread : title), ref: task.isEmpty ? nil : task, role: r.role,
                    tag: s.flatMap(tag), dot: theme.colour(s.map(tone) ?? .idle), spoken: s.map(spoken) ?? "not running",
                    busy: peer.state == "up" ? Busy.of(peer: s) : nil, indent: CGFloat(SidebarGrid.contents(of: Double(indent))))
                    .opacity(dim)
            }
        }
    }

    // MARK: a program's

    /// A program's line (the programs design §11.1): its mark, its
    /// director's state as the dot, and its needs and its members',
    /// each counted once, as the badge; and under it, unless ← folded
    /// it, its director and its members in its order, one level in, a
    /// paired one as its server's group lists it. A click shows the
    /// program's screen.
    @ViewBuilder
    private func program(_ g: AppAPI.Program) -> some View {
        let sel = Selection.program(g.slug)
        let director = app.directorSession(g.slug)
        let state = Attention.of(director: director, in: g)
        let working = Busy.of(director: director, in: g)
        let open = filtering || SidebarTree.opens(g, nav: app.nav)
        ProjectRow(name: g.slug, dot: theme.colour(working == nil ? state.tone : .idle), state: state.word, needs: app.needs(of: g).count,
                   selected: app.nav.selection == sel, program: true, tag: g.paused ? "paused" : "",
                   busy: !open && SidebarTree.busy(g, projects: app.projects, sessions: app.sessions, peers: app.peers, watched: app.watched))
            .selectable(sel, session: false) { listFocused = true }
            .contextMenu {
                Button("Show the Program") { app.select(sel) }
                Button("Open the Director") { app.select(.session(.director(g.slug))) }
            }
        if open {
            let dref = SessionRef.director(g.slug)
            if !filtering || SidebarTree.matches(app.filter, g.slug, "director") {
                row(.session(dref), label: SessionRole.director.word, ref: nil, role: .director, tag: state.waits ? (state.word, state.tone) : nil,
                    dot: theme.colour(state.tone), spoken: state.spoken, context: ContextUse.of(director, hint: app.contextHint),
                    busy: working, indent: CGFloat(SidebarGrid.contents(of: 0)))
            }
            ForEach(shownMembers(g), id: \.self) { m in
                switch m {
                case .local(let p):
                    project(p, indent: CGFloat(SidebarGrid.step), member: true)
                case .paired(let alias, let slug, let peer, let p, let mstate):
                    if let peer, let p {
                        remote(peer, p, under: g)
                    } else {
                        // Not in the mirror: its invite waits, or it isn't
                        // shared with this server. Its row alone, dim.
                        ProjectRow(name: m.name, dot: theme.idle, state: "not shared", needs: 0, selected: false,
                                   tag: mstate == "invited" || mstate == "pending" ? "invited" : mstate == "declined" ? "declined" : "not shared")
                            .padding(.leading, CGFloat(SidebarGrid.step))
                            .frame(height: CGFloat(density.rowHeight(session: false)))
                            .padding(.leading, 8)
                            .padding(.trailing, 9)
                            .opacity(0.55)
                            .help("\(slug) on \(alias) isn't shared with this server")
                    }
                }
            }
        }
    }

    /// A paired server: its name, then its tag (PeerTag) right after it,
    /// as the rows' pills: the trailing edge is the fold chevron's. A
    /// long name truncates, never the tag.
    private func remoteHeader(_ peer: AppAPI.Peer) -> some View {
        HStack(spacing: 6) {
            Image(systemName: "network")
                .accessibilityHidden(true)
            Text(peer.name.isEmpty ? peer.alias : peer.name)
                .lineLimit(1)
                .truncationMode(.tail)
            PeerTagView(tag: PeerTag.of(peer))
                .fixedSize()
                .layoutPriority(1)
            // Folded over a session that works (T985).
            if !filtering, app.nav.collapsed.contains(Navigation.sectionKey(server: peer.alias)),
               SidebarTree.busy(peer, watched: app.watched) {
                BusyMark(size: 6).frame(width: 6, height: 6)
            }
            Spacer(minLength: 0)
        }
        .help(remoteHelp(peer))
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(PeerTag.label(peer))
    }

    private func remoteHelp(_ peer: AppAPI.Peer) -> String {
        switch RemotePane.of(caps: app.caps, peer: peer, alias: peer.alias) {
        case .write: "\(peer.alias)'s panes open here, and take your keys."
        case .view: "\(peer.alias)'s panes open here to watch; keys stay here."
        case .closed(let why): why
        }
    }

    /// A paired server's session's word: a question or a dialog there.
    private func tag(_ s: AppAPI.PeerSession) -> (String, Attention.Tone?)? {
        if s.asking { return ("asks", .question) }
        if s.state == "blocked" { return s.reason == "permission" ? ("allow?", .approval) : ("blocked", .attention) }
        return nil
    }

    /// What VoiceOver says of a paired server's session after its name.
    private func spoken(_ s: AppAPI.PeerSession) -> String {
        if s.asking { return "asks a question" }
        if s.state == "blocked" { return s.reason == "permission" ? "needs approval" : "blocked" }
        return s.state.isEmpty ? "running" : s.state
    }

    /// A paired server's session's dot.
    private func tone(_ s: AppAPI.PeerSession) -> Attention.Tone {
        if let (_, tone) = tag(s), let tone { return tone }
        return s.state == "idle" || s.state.isEmpty ? .idle : .working
    }
}

/// A section's label: a click folds what's under it, its chevron showing
/// while it is folded or the pointer is over it.
private struct SectionHead<Label: View>: View {
    @Environment(\.theme) private var theme
    let first: Bool
    let open: Binding<Bool>
    @ViewBuilder var label: Label
    @State private var over = false

    var body: some View {
        Button {
            open.wrappedValue.toggle()
        } label: {
            HStack(spacing: 6) {
                label
                Spacer(minLength: 0)
                Image(systemName: "chevron.down")
                    .font(.system(size: 9, weight: .semibold))
                    .rotationEffect(.degrees(open.wrappedValue ? 0 : -90))
                    .opacity(over || !open.wrappedValue ? 1 : 0)
                    .accessibilityHidden(true)
            }
            .font(theme.sized(13))
            .foregroundStyle(theme.muted)
            .padding(.leading, 17)
            .padding(.trailing, 16)
            .frame(height: 22, alignment: .top)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .onHover { over = $0 }
        .padding(.top, first ? 0 : 8)
        .accessibilityValue(open.wrappedValue ? "expanded" : "collapsed")
        .accessibilityAddTraits(.isHeader)
    }
}

/// A need in the lower section: a click shows what it names.
private struct NeedLine: View {
    @Environment(\.theme) private var theme
    let need: Need
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(alignment: .firstTextBaseline, spacing: 9) {
                Dot(colour: theme.colour(need.kind))
                VStack(alignment: .leading, spacing: 2) {
                    Text(need.title)
                        .font(theme.sized(13, .medium))
                        .lineLimit(1)
                    Text(need.detail)
                        .font(theme.sized(12.5))
                        .foregroundStyle(theme.muted)
                        .lineLimit(1)
                }
                Spacer(minLength: 0)
            }
            .padding(.horizontal, 13)
            .frame(height: 48, alignment: .top)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .combine)
        .accessibilityHint(need.kind.verb)
    }
}

private struct Selectable: ViewModifier {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    @Environment(\.density) private var density
    let sel: Selection
    let session: Bool
    let tapped: () -> Void

    func body(content: Content) -> some View {
        let on = app.nav.selection == sel
        let shape = RoundedRectangle(cornerRadius: 6)
        content
            .frame(maxWidth: .infinity, alignment: .leading)
            .frame(height: CGFloat(density.rowHeight(session: session)))
            .background(on ? theme.selection : .clear, in: shape)
            .contentShape(shape)
            .onTapGesture {
                tapped()
                app.select(sel)
            }
            .padding(.leading, 8)
            .padding(.trailing, 9)
            .id(sel)
            .accessibilityAddTraits(on ? [.isButton, .isSelected] : .isButton)
            .accessibilityAction { app.select(sel) }
    }
}

private extension View {
    /// A sidebar row that shows sel in the window: a click, ↑ ↓ or a
    /// letter typed selects it, and the selected one wears the fill.
    func selectable(_ sel: Selection, session: Bool, tapped: @escaping () -> Void) -> some View {
        modifier(Selectable(sel: sel, session: session, tapped: tapped))
    }
}

/// The terminal prompt, the app's mark: a chevron and a line.
struct PromptMark: View {
    var lineWidth: CGFloat = 1.5

    var body: some View {
        Canvas { c, size in
            let k = size.width / 24
            var p = Path()
            p.move(to: CGPoint(x: 4 * k, y: 7 * k))
            p.addLine(to: CGPoint(x: 9 * k, y: 12 * k))
            p.addLine(to: CGPoint(x: 4 * k, y: 17 * k))
            p.move(to: CGPoint(x: 12 * k, y: 18 * k))
            p.addLine(to: CGPoint(x: 20 * k, y: 18 * k))
            c.stroke(p, with: .foreground, style: StrokeStyle(lineWidth: lineWidth, lineCap: .round, lineJoin: .round))
        }
        .accessibilityHidden(true)
    }
}

/// The sidebar's filter field (⌥⌘F): Esc clears it and hands the keys
/// back to the List (done).
private struct FilterField: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let done: () -> Void
    @FocusState private var focused: Bool

    var body: some View {
        @Bindable var app = app
        HStack(spacing: 6) {
            Image(systemName: "line.3.horizontal.decrease")
                .font(theme.body(.caption, .semibold))
                .foregroundStyle(theme.muted)
                .accessibilityHidden(true)
            TextField("Filter", text: $app.filter)
                .textFieldStyle(.plain)
                .font(theme.body(.callout))
                .focused($focused)
                .onExitCommand {
                    app.filter = ""
                    done()
                }
                .onSubmit(done)
                .accessibilityLabel("Filter the sidebar")
            if !app.filter.isEmpty {
                Button {
                    app.filter = ""
                } label: {
                    Image(systemName: "xmark.circle.fill")
                }
                .accessibilityLabel("Clear the filter")
                .buttonStyle(.plain)
                .foregroundStyle(theme.muted)
                .help("Clear the filter (Esc)")
            }
        }
        .padding(.horizontal, 9)
        .padding(.vertical, 6)
        .background(theme.pane, in: RoundedRectangle(cornerRadius: 6))
        .overlay(RoundedRectangle(cornerRadius: 6).strokeBorder(focused ? theme.action.opacity(0.7) : theme.line, lineWidth: 1))
        .help("Filter the sidebar (⌥⌘F)")
        .onChange(of: app.filterFocus) { focused = true }
        .onAppear { focused = true }
    }
}

/// A paired server's tag in its header: "offline" muted with a dot, else
/// its panes' access as a quiet capsule with its icon (a pill,
/// unfilled: nothing here waits on the user).
struct PeerTagView: View {
    @Environment(\.theme) private var theme
    let tag: PeerTag

    var body: some View {
        HStack(spacing: 3) {
            if let symbol = tag.symbol {
                Image(systemName: symbol)
                    .imageScale(.small)
            } else {
                Circle()
                    .fill(theme.idle)
                    .frame(width: 5, height: 5)
            }
            Text(tag.word)
                .font(theme.body(.caption))
        }
        .padding(.horizontal, 6)
        .padding(.vertical, 1)
        .foregroundStyle(theme.muted)
        .background(theme.line.opacity(tag.offline ? 0.5 : 1), in: RoundedRectangle(cornerRadius: 4))
        .textCase(nil)
    }
}

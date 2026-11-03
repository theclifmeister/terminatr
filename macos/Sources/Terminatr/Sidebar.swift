import SwiftUI
import TerminatrKit
import TerminatrUI

/// The window's sidebar (design T746, lean as T748 milestone A has it):
/// what needs the user, cut to three rows and "+N more" (the app's own:
/// the TUI has its ⚑ and W instead); then the rows of the TUI's sidebar
/// (SidebarTree, T762): this server's active projects, pinned ones
/// first, each with its coordinator's state as the dot and its needs as
/// the badge, and under it its coordinator, running or not, and every
/// unresolved thread, done ones too, in the dashboard's order, with
/// [ui] needs_first the projects that need the user first; then each
/// paired server's shared projects, its coordinator and its unresolved
/// threads, running there or not (T768); and at the bottom the inactive ones
/// folded under "Inactive (n)" (⇧⌘H unfolds them), each folded until →
/// expands it, dormant. A project shows its dashboard, a coordinator or
/// thread its session; a need shows what it names. Each section and
/// project folds, and stays so across launches. Every
/// project and session row has its state as a dot, and what waits on
/// the user (allow?, asks, …) as a filled pill (T752). The selection is
/// the List's (T748 milestone E): ↑ ↓ move it, letters typed select the
/// row they begin, ← folds a project (or goes up to it from its
/// session) and → unfolds it. It is drawn as Bloom's violet pill, not
/// the List's own (the system accent). The filter field on top (⌥⌘F)
/// keeps the rows that match it, every one of them.
struct Sidebar: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    @Environment(\.density) private var density
    @Environment(\.sidebarRowSize) private var rowSize
    @Environment(\.defaultMinListRowHeight) private var minRowHeight
    /// Where the window's needs rotor finds the need rows.
    let needsRotor: Namespace.ID
    @State private var typing = TypeSelect()
    /// The List has the keys from launch, not the filter field.
    @FocusState private var listFocused: Bool

    var body: some View {
        List(selection: selection) {
            let needs = shownNeeds
            if !needs.isEmpty {
                Section(isExpanded: expanded(Navigation.needsSection)) {
                    let all = filtering || app.nav.unfolded.contains(Navigation.moreKey())
                    let shown = all ? needs : Array(needs.prefix(SidebarTree.needsShown))
                    ForEach(shown) { n in
                        NeedLine(need: n) { app.open(project: n.project, session: n.session) }
                            .accessibilityRotorEntry(id: n.id, in: needsRotor)
                    }
                    if !filtering, needs.count > SidebarTree.needsShown {
                        more(Navigation.moreKey(), all ? "Show fewer" : SidebarTree.moreNeeds(Array(needs.dropFirst(SidebarTree.needsShown))), unfolded: all)
                    }
                } header: {
                    HStack(spacing: 6) {
                        Text("Needs you")
                        Text("\(needs.count)")
                            .font(theme.mono(.caption2, .medium))
                            .padding(.horizontal, 5)
                            .background(theme.attention, in: Capsule())
                            .foregroundStyle(theme.onAttention)
                    }
                    .font(theme.body(.caption, .semibold))
                    .accessibilityElement(children: .combine)
                }
            }
            let active = shown(app.entries)
            if !filtering || !active.isEmpty {
                Section(isExpanded: expanded(Navigation.sectionKey(server: ""))) {
                    ForEach(active, id: \.slug) { e in
                        switch e {
                        case .project(let p): project(p)
                        case .program(let g): program(g)
                        }
                    }
                } header: {
                    // "This server" only tells it from paired ones.
                    Text(app.peers.isEmpty ? "Projects" : "This server")
                        .font(theme.body(.caption, .semibold))
                }
            }
            ForEach(app.peers, id: \.alias) { peer in
                let projects = shown(peer)
                if !filtering || !projects.isEmpty {
                    Section(isExpanded: expanded(Navigation.sectionKey(server: peer.alias))) {
                        ForEach(projects, id: \.project) { p in
                            remote(peer, p)
                        }
                    } header: {
                        remoteHeader(peer)
                            .font(theme.body(.caption, .semibold))
                    }
                }
            }
            // At the bottom, after the paired servers, as the TUI pins it.
            let inactive = shown(app.inactiveProjects)
            if !inactive.isEmpty {
                Section(isExpanded: Binding(get: { filtering || app.nav.showInactive }, set: { app.nav.showInactive = $0 })) {
                    ForEach(inactive, id: \.slug) { p in
                        project(p)
                    }
                } header: {
                    Text("Inactive (\(inactive.count))")
                        .font(theme.body(.caption, .semibold))
                        .help("Projects set inactive: View › Show Inactive Projects (⇧⌘H)")
                }
            }
            if filtering, rows.isEmpty, needs.isEmpty {
                Text("Nothing matches “\(app.filter)”")
                    .font(theme.body(.subheadline))
                    .foregroundStyle(theme.muted)
            }
        }
        .listStyle(.sidebar)
        // Regular is the system's own (System Settings › Sidebar icon size).
        .environment(\.sidebarRowSize, density == .compact ? .small : density == .roomy ? .large : rowSize)
        .environment(\.defaultMinListRowHeight, density.minRowHeight ?? minRowHeight)
        .scrollContentBackground(.hidden)
        .focused($listFocused)
        .onAppear { listFocused = true }
        .onKeyPress(keys: [.leftArrow, .rightArrow]) { press in
            fold(open: press.key == .rightArrow) ? .handled : .ignored
        }
        .onKeyPress(characters: .alphanumerics, phases: .down) { press in
            typeSelect(press)
        }
        .background { Frost() }
        .safeAreaInset(edge: .top) {
            FilterField { listFocused = true }
                .padding(.horizontal, 12)
                .padding(.bottom, 4)
        }
    }

    /// The List's selection is the window's: a row picked by click or
    /// key shows its project or session.
    private var selection: Binding<Selection?> {
        Binding(get: { app.nav.selection }, set: { s in
            if let s, s != app.nav.selection { app.select(s) }
        })
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
    private func listing(_ p: AppAPI.Project) -> (open: Bool, coordinator: Bool, threads: [AppAPI.Thread], dormant: Bool) {
        if filtering {
            let found = SidebarTree.threads(of: p, matching: app.filter)
            return (!found.isEmpty || findsCoordinator, findsCoordinator, found, !p.active)
        }
        guard SidebarTree.opens(p, nav: app.nav) else { return (false, false, [], false) }
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
        func add(_ projects: [AppAPI.Project]) {
            for p in projects {
                out.append((.project(p.slug), p.slug))
                let l = listing(p)
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
                        case .local(let p): add([p])
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

    /// A "N more" row: a click unfolds what it counts, or folds it again.
    private func more(_ key: String, _ label: String, unfolded: Bool) -> some View {
        Button {
            if unfolded { app.nav.unfolded.remove(key) } else { app.nav.unfolded.insert(key) }
        } label: {
            HStack(spacing: 6) {
                Text(label)
                    .font(theme.body(.subheadline))
                    .lineLimit(1)
                Spacer(minLength: 4)
                Image(systemName: unfolded ? "chevron.up" : "chevron.down")
                    .font(theme.body(.glyph, .semibold))
                    .accessibilityHidden(true)
            }
            .foregroundStyle(theme.muted)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    // MARK: this server's

    /// A project's line, and under it, unless it is folded (an inactive
    /// one until → expands it), its coordinator and its threads, as the
    /// TUI's tree; while filtering, the rows the filter finds. An
    /// inactive project's are dormant: nothing of it runs.
    @ViewBuilder
    private func project(_ p: AppAPI.Project, indent: CGFloat = 0) -> some View {
        let sel = Selection.project(p.slug)
        let needs = app.needs(of: p.slug).count
        let state = Attention.of(coordinator: app.coordinatorSession(p.slug), in: p)
        let l = listing(p)
        let coordinator = app.coordinatorSession(p.slug)
        ProjectRow(name: p.slug, dot: theme.colour(state.tone), state: state.word, needs: needs, selected: app.nav.selection == sel)
            .padding(.leading, indent)
            .selectable(sel)
            .contextMenu { ProjectMenu(project: p, coordinator: coordinator != nil, divided: false) }
        if l.open {
            if l.coordinator {
                // Its context use, as the TUI's row: warn from [ui]
                // context_hint, high from 80% (with app API 13's hint).
                let ctx = l.dormant ? nil : ContextUse.of(coordinator, hint: app.contextHint)
                row(.session(SessionRef(project: p.slug)), label: "Coordinator", ref: nil,
                    tag: state.waits && !l.dormant ? (state.word, state.tone) : nil, dot: theme.colour(l.dormant ? .idle : state.tone),
                    spoken: l.dormant ? "inactive" : state.spoken, dormant: l.dormant, context: ctx,
                    busy: l.dormant ? nil : app.busy(coordinator: p), menu: coordinator == nil ? nil : p, indent: indent)
            }
            // Keyed with the project: thread ids repeat across projects,
            // and the List flattens every project's rows into one.
            ForEach(l.threads, id: \.record.id) { t in
                row(.session(SessionRef(project: p.slug, thread: t.record.id)), label: t.record.title, ref: t.task?.ref,
                    tag: l.dormant ? nil : tag(t), dot: theme.colour(l.dormant ? .idle : app.attention(t).tone),
                    spoken: l.dormant ? "inactive" : spoken(t), dormant: l.dormant, busy: l.dormant ? nil : app.busy(t), indent: indent)
                    .id("\(p.slug)/\(t.record.id)")
            }
        }
    }

    /// A session's row: its dot, task ref and title, and its tag: a
    /// filled pill in its tone's colour, or plain muted text (PR) when it
    /// has none. A dormant one (an inactive project's) is muted.
    /// A running coordinator's row has its context use, and Save & Clear
    /// in its menu (menu, its project). A working one (busy) has the
    /// activity mark for its dot and how long its turn has run.
    private func row(_ sel: Selection, label: String, ref: String?, tag: (String, Attention.Tone?)?, dot: Color, spoken: String,
                     dormant: Bool = false, context: ContextUse? = nil, busy: Busy? = nil, menu: AppAPI.Project? = nil,
                     indent: CGFloat = 0) -> some View {
        let on = app.nav.selection == sel
        return HStack(spacing: 6) {
            if busy != nil {
                BusyMark(size: 6, colour: on ? theme.onAction : nil)
                    .frame(width: 6, height: 6)
            } else {
                Circle()
                    .fill(on ? theme.onAction : dot)
                    .frame(width: 6, height: 6)
                    .accessibilityHidden(true)
            }
            if let ref {
                Text(ref)
                    .font(theme.mono(.footnote, .medium))
                    .foregroundStyle(on ? theme.onAction : theme.action)
            }
            Text(label)
                .font(theme.body(.callout))
                .lineLimit(1)
            Spacer(minLength: 4)
            if let busy {
                BusyLabel(busy: busy, colour: on ? theme.onAction : nil)
                    .fixedSize()
            }
            if let context {
                Text(context.word)
                    .font(theme.mono(.caption2, context.tone == nil ? .regular : .semibold))
                    .foregroundStyle(on ? theme.onAction : context.tone.map { theme.colour($0) } ?? theme.muted)
                    .help(context.spoken)
            }
            if let (word, tone) = tag {
                if let tone {
                    Text(word)
                        .font(theme.mono(.caption2, .medium))
                        .padding(.horizontal, 6)
                        .padding(.vertical, 1)
                        .background(theme.colour(tone), in: Capsule())
                        .foregroundStyle(theme.on(tone))
                } else {
                    Text(word)
                        .font(theme.mono(.caption2, .medium))
                        .foregroundStyle(on ? theme.onAction : theme.muted)
                }
            }
        }
        .padding(.leading, 14 + indent)
        .foregroundStyle(on ? theme.onAction : dormant ? theme.muted : theme.ink)
        // One element that says the row and its state: "T6 Network popup
        // polish, needs approval".
        .accessibilityElement(children: .ignore)
        .accessibilityLabel([ref, label].compactMap { $0 }.joined(separator: " "))
        .accessibilityValue([busy.map { $0.spoken(now: Date()) } ?? spoken, context?.spoken].compactMap { $0 }.joined(separator: ", "))
        .selectable(sel)
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
        let indent: CGFloat = g == nil ? 0 : 12
        let open = g.map { SidebarTree.opens(member: peer.alias, p.project, of: $0, nav: app.nav) }
            ?? !app.nav.collapsed.contains(Navigation.sectionKey(server: peer.alias, project: p.project))
        let progTag = g == nil ? app.pairedMembers["\(peer.alias)/\(p.project)"].map { "· \($0)" } ?? "" : ""
        ProjectRow(name: g == nil ? p.project : ProgramMemberRow.paired(alias: peer.alias, slug: p.project, peer: peer, project: p, state: "").name,
                   dot: coordinator.map { theme.colour(tone($0)) } ?? theme.idle,
                   state: coordinator.map(spoken) ?? "not running", needs: p.stale ? 0 : p.needsYou,
                   selected: app.nav.selection == sel, tag: progTag)
            .padding(.leading, indent)
            .selectable(sel)
            .opacity(dim)
            .contextMenu {
                if let g {
                    Button(open ? "Fold" : "Unfold") {
                        let key = Navigation.memberKey(program: g.slug, server: peer.alias, project: p.project)
                        if open { app.nav.collapsed.insert(key) } else { app.nav.collapsed.remove(key) }
                    }
                }
            }
        if open || filtering {
            ForEach(sessions(peer, p), id: \.ref) { s, r, task, title in
                row(.session(r), label: r.isCoordinator ? "Coordinator" : title.isEmpty ? r.thread : title, ref: task.isEmpty ? nil : task,
                    tag: s.flatMap(tag), dot: theme.colour(s.map(tone) ?? .idle), spoken: s.map(spoken) ?? "not running", indent: indent)
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
        ProjectRow(name: g.slug, dot: theme.colour(state.tone), state: state.word, needs: app.needs(of: g).count,
                   selected: app.nav.selection == sel, program: true, tag: g.paused ? "paused" : "")
            .selectable(sel)
            .contextMenu {
                Button("Show the Program") { app.select(sel) }
                Button("Open the Director") { app.select(.session(.director(g.slug))) }
            }
        if filtering || SidebarTree.opens(g, nav: app.nav) {
            let dref = SessionRef.director(g.slug)
            if !filtering || SidebarTree.matches(app.filter, g.slug, "director") {
                row(.session(dref), label: "Director", ref: nil, tag: state.waits ? (state.word, state.tone) : nil,
                    dot: theme.colour(state.tone), spoken: state.spoken, context: ContextUse.of(director, hint: app.contextHint))
            }
            ForEach(shownMembers(g), id: \.self) { m in
                switch m {
                case .local(let p):
                    project(p, indent: 12)
                case .paired(let alias, let slug, let peer, let p, let mstate):
                    if let peer, let p {
                        remote(peer, p, under: g)
                    } else {
                        // Not in the mirror: its invite waits, or it isn't
                        // shared with this server. Its row alone, dim.
                        ProjectRow(name: m.name, dot: theme.idle, state: "not shared", needs: 0, selected: false,
                                   tag: mstate == "invited" || mstate == "pending" ? "invited" : mstate == "declined" ? "declined" : "not shared")
                            .padding(.leading, 12)
                            .padding(.vertical, density.rowPadding)
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

/// A need in the sidebar: a click shows what it names.
private struct NeedLine: View {
    @Environment(\.theme) private var theme
    @Environment(\.density) private var density
    let need: Need
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 6) {
                Image(systemName: need.kind.symbol)
                    .foregroundStyle(theme.colour(need.kind))
                    .frame(width: 16)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 0) {
                    Text(need.title)
                        .font(theme.body(.callout, .medium))
                        .lineLimit(1)
                    Text(need.detail)
                        .font(theme.body(.caption))
                        .foregroundStyle(theme.muted)
                        .lineLimit(1)
                }
                Spacer(minLength: 0)
            }
            .padding(.vertical, density.rowPadding)
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

    func body(content: Content) -> some View {
        let on = app.nav.selection == sel
        content
            .padding(.vertical, density.rowPadding)
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(Rectangle())
            .tag(sel)
            .listRowBackground(
                RoundedRectangle(cornerRadius: 9)
                    .fill(on ? theme.action : .clear)
                    .shadow(color: on ? theme.action.opacity(0.5) : .clear, radius: 7, y: 4)
                    .padding(.horizontal, 10)
            )
            .accessibilityAddTraits(on ? [.isButton, .isSelected] : .isButton)
            .accessibilityAction { app.select(sel) }
    }
}

private extension View {
    /// A sidebar row that shows sel in the window: the List selects it
    /// (a click, ↑ ↓, a letter typed), and the selected one wears the
    /// pill.
    func selectable(_ sel: Selection) -> some View {
        modifier(Selectable(sel: sel))
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
        .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 9))
        .overlay(RoundedRectangle(cornerRadius: 9).strokeBorder(focused ? theme.action.opacity(0.7) : theme.edge, lineWidth: 1))
        .help("Filter the sidebar (⌥⌘F)")
        .onChange(of: app.filterFocus) { focused = true }
    }
}

/// A paired server's tag in its header: "offline" muted with a dot, else
/// its panes' access as a quiet capsule with its icon (Bloom's pill,
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
                .font(theme.mono(.caption2, .medium))
        }
        .padding(.horizontal, 6)
        .padding(.vertical, 1)
        .foregroundStyle(theme.muted)
        .background(theme.line.opacity(tag.offline ? 0.5 : 1), in: Capsule())
        .textCase(nil)
    }
}

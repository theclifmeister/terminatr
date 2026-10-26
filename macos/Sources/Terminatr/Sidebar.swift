import SwiftUI
import TerminatrKit

/// The window's sidebar (design T746, lean as T748 milestone A has it):
/// what needs the user, cut to three rows and "+N more"; then this
/// server's projects as the TUI orders them, pinned and active ones,
/// one line each with its coordinator's state as the dot and its needs
/// as the badge, and the inactive ones folded under "Inactive (n)" at
/// the bottom (⇧⌘H unfolds them); then each paired server's shared
/// projects (as the TUI's sidebar has them, alias/session). Only the
/// selected project and those that need the user list their
/// coordinator (when it runs or asks) and threads (those that wait on
/// the user, the rest folded into "N more"). A project shows its
/// dashboard, a coordinator or thread its session; a need shows what it
/// names. Each section folds, and stays so across launches. Every
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
                    }
                    if !filtering, needs.count > SidebarTree.needsShown {
                        more(Navigation.moreKey(), all ? "Show fewer" : SidebarTree.moreNeeds(Array(needs.dropFirst(SidebarTree.needsShown))), unfolded: all)
                    }
                } header: {
                    HStack(spacing: 6) {
                        Text("Needs you")
                        Text("\(needs.count)")
                            .font(theme.mono(10.5, .medium))
                            .padding(.horizontal, 5)
                            .background(theme.attention, in: Capsule())
                            .foregroundStyle(theme.onAttention)
                    }
                }
            }
            let active = shown(app.activeProjects)
            if !filtering || !active.isEmpty {
                Section(isExpanded: expanded(Navigation.sectionKey(server: ""))) {
                    ForEach(active, id: \.slug) { p in
                        project(p)
                    }
                } header: {
                    // "This server" only tells it from paired ones.
                    Text(app.peers.isEmpty ? "Projects" : "This server")
                }
            }
            let inactive = shown(app.inactiveProjects)
            if !inactive.isEmpty {
                Section(isExpanded: Binding(get: { filtering || app.nav.showInactive }, set: { app.nav.showInactive = $0 })) {
                    ForEach(inactive, id: \.slug) { p in
                        project(p)
                    }
                } header: {
                    Text("Inactive (\(inactive.count))")
                        .help("Projects set inactive: View › Show Inactive Projects (⇧⌘H)")
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
                    }
                }
            }
            if filtering, rows.isEmpty, needs.isEmpty {
                Text("Nothing matches “\(app.filter)”")
                    .font(theme.body(12))
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
        .safeAreaInset(edge: .bottom) {
            // Only when something is wrong: all well, it takes no row.
            if StatusLine.shows(app) {
                StatusLine()
                    .padding(12)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
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

    /// The projects the filter keeps: by name, or by a thread it finds.
    private func shown(_ projects: [AppAPI.Project]) -> [AppAPI.Project] {
        guard filtering else { return projects }
        return projects.filter { SidebarTree.matches(app.filter, $0.slug) || !SidebarTree.threads(of: $0, matching: app.filter).isEmpty }
    }

    /// A paired server's projects the filter keeps: by name or server,
    /// or by a session it finds.
    private func shown(_ peer: AppAPI.Peer) -> [AppAPI.PeerProject] {
        guard filtering else { return peer.projects }
        return peer.projects.filter { p in
            SidebarTree.matches(app.filter, p.project, peer.alias, peer.name) || !sessions(peer, p).isEmpty
        }
    }

    /// A paired server's project's sessions with their refs: those the
    /// filter finds, all of them when it finds the project.
    private func sessions(_ peer: AppAPI.Peer, _ p: AppAPI.PeerProject) -> [(AppAPI.PeerSession, SessionRef)] {
        let all = p.sessions.compactMap { s in SessionRef(s, of: peer.alias).map { (s, $0) } }
        guard filtering, !SidebarTree.matches(app.filter, p.project, peer.alias, peer.name) else { return all }
        return all.filter { s, r in SidebarTree.matches(app.filter, s.task, r.thread, r.isCoordinator ? "coordinator" : "") }
    }

    /// What a project of this server lists under it: its coordinator
    /// row, its threads, and how many fold into "N more".
    private func listing(_ p: AppAPI.Project) -> (open: Bool, coordinator: Bool, threads: [AppAPI.Thread], more: Int) {
        let coordinator = app.coordinatorSession(p.slug)
        let state = Attention.of(coordinator: coordinator, in: p)
        if filtering {
            let found = SidebarTree.threads(of: p, matching: app.filter)
            return (!found.isEmpty, false, found, 0)
        }
        guard SidebarTree.opens(p, nav: app.nav, needs: app.needs(of: p.slug).count) else { return (false, false, [], 0) }
        let all = app.nav.unfolded.contains(Navigation.moreKey(project: p.slug))
        let selected = app.selectedRef.flatMap { $0.server.isEmpty && $0.project == p.slug ? $0.thread : nil }
        let (shown, more) = SidebarTree.threads(of: p, selected: selected, unfolded: all) { app.attention($0) }
        return (true, coordinator != nil || state.waits, shown, more)
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
        if !app.nav.collapsed.contains(Navigation.sectionKey(server: "")) { add(shown(app.activeProjects)) }
        if filtering || app.nav.showInactive { add(shown(app.inactiveProjects)) }
        for peer in app.peers where !app.nav.collapsed.contains(Navigation.sectionKey(server: peer.alias)) {
            for p in shown(peer) {
                out.append((.project(p.project, server: peer.alias), p.project))
                if !app.nav.collapsed.contains(Navigation.sectionKey(server: peer.alias, project: p.project)) {
                    out += sessions(peer, p).map { (.session($1), $1.isCoordinator ? "Coordinator" : $1.thread) }
                }
            }
        }
        return out
    }

    /// → unfolds the selected project, ← folds it, or from one of its
    /// sessions goes up to it.
    private func fold(open: Bool) -> Bool {
        switch app.nav.selection {
        case .project(let slug, let server):
            let key = Navigation.sectionKey(server: server, project: slug)
            if !server.isEmpty {
                // A paired server's project is a disclosure group.
                if open { app.nav.collapsed.remove(key) } else { app.nav.collapsed.insert(key) }
            } else if open {
                app.nav.unfold(key)
            } else {
                app.nav.fold(key)
            }
            return true
        case .session(let ref) where !open:
            app.select(.project(ref.project, server: ref.server))
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
                    .font(theme.body(12))
                    .lineLimit(1)
                Spacer(minLength: 4)
                Image(systemName: unfolded ? "chevron.up" : "chevron.down")
                    .font(theme.body(9, .semibold))
                    .accessibilityHidden(true)
            }
            .foregroundStyle(theme.muted)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    // MARK: this server's

    /// A project's line, and under it, when it is selected or needs the
    /// user (or → unfolded it), its coordinator (when it runs or asks)
    /// and its threads; while filtering, the threads the filter finds.
    @ViewBuilder
    private func project(_ p: AppAPI.Project) -> some View {
        let sel = Selection.project(p.slug)
        let needs = app.needs(of: p.slug).count
        let state = Attention.of(coordinator: app.coordinatorSession(p.slug), in: p)
        let l = listing(p)
        ProjectRow(project: p, dot: theme.colour(state.tone), state: state.word, needs: needs, selected: app.nav.selection == sel)
            .selectable(sel)
        if l.open {
            if l.coordinator {
                row(.session(SessionRef(project: p.slug)), label: "Coordinator", ref: nil,
                    tag: state.waits ? (state.word, state.tone) : nil, dot: theme.colour(state.tone))
            }
            let key = Navigation.moreKey(project: p.slug)
            // Keyed with the project: thread ids repeat across projects,
            // and the List flattens every project's rows into one.
            ForEach(l.threads, id: \.record.id) { t in
                row(.session(SessionRef(project: p.slug, thread: t.record.id)), label: t.record.title, ref: t.task?.ref,
                    tag: tag(t), dot: theme.colour(app.attention(t).tone))
                    .id("\(p.slug)/\(t.record.id)")
            }
            if l.more > 0 {
                self.more(key, l.more == 1 ? "1 more thread" : "\(l.more) more threads", unfolded: false)
                    .padding(.leading, 14)
            } else if !filtering, app.nav.unfolded.contains(key), !p.threads.isEmpty {
                self.more(key, "Show fewer", unfolded: true)
                    .padding(.leading, 14)
            }
        }
    }

    /// A session's row: its dot, task ref and title, and its tag: a
    /// filled pill in its tone's colour, or plain muted text (PR) when it
    /// has none.
    private func row(_ sel: Selection, label: String, ref: String?, tag: (String, Attention.Tone?)?, dot: Color) -> some View {
        let on = app.nav.selection == sel
        return HStack(spacing: 6) {
            Circle()
                .fill(on ? theme.onAction : dot)
                .frame(width: 6, height: 6)
                .accessibilityHidden(true)
            if let ref {
                Text(ref)
                    .font(theme.mono(11.5, .medium))
                    .foregroundStyle(on ? theme.onAction : theme.action)
            }
            Text(label)
                .font(theme.body(12.5))
                .lineLimit(1)
            Spacer(minLength: 4)
            if let (word, tone) = tag {
                if let tone {
                    Text(word)
                        .font(theme.mono(10.5, .medium))
                        .padding(.horizontal, 6)
                        .padding(.vertical, 1)
                        .background(theme.colour(tone), in: Capsule())
                        .foregroundStyle(theme.on(tone))
                } else {
                    Text(word)
                        .font(theme.mono(10.5, .medium))
                        .foregroundStyle(on ? theme.onAction : theme.muted)
                }
            }
        }
        .padding(.leading, 14)
        .foregroundStyle(on ? theme.onAction : theme.ink)
        .selectable(sel)
        .contextMenu {
            if case .session(let r) = sel { NewWindowButton(ref: r) }
        }
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

    private func remote(_ peer: AppAPI.Peer, _ p: AppAPI.PeerProject) -> some View {
        let sel = Selection.project(p.project, server: peer.alias)
        let on = app.nav.selection == sel
        return DisclosureGroup(isExpanded: expanded(Navigation.sectionKey(server: peer.alias, project: p.project))) {
            ForEach(sessions(peer, p), id: \.0.id) { s, r in
                row(.session(r), label: r.isCoordinator ? "Coordinator" : r.thread, ref: s.task.isEmpty ? nil : s.task,
                    tag: tag(s), dot: theme.colour(tone(s)))
            }
        } label: {
            HStack(spacing: 8) {
                // The coordinator's state, as this server's projects have it.
                let coordinator = p.sessions.first { SessionRef($0, of: peer.alias)?.isCoordinator == true }
                Circle()
                    .fill(coordinator.map { theme.colour(tone($0)) } ?? theme.idle)
                    .overlay(Circle().strokeBorder(on ? theme.onAction.opacity(0.8) : .clear, lineWidth: 1))
                    .frame(width: 8, height: 8)
                    .accessibilityHidden(true)
                Text(p.project)
                    .font(theme.body(13, .semibold))
                    .lineLimit(1)
                Spacer(minLength: 4)
                if p.needsYou > 0, !p.stale {
                    Text("\(p.needsYou)")
                        .font(theme.mono(11, .medium))
                        .padding(.horizontal, 6)
                        .padding(.vertical, 1)
                        .background(theme.attention, in: Capsule())
                        .foregroundStyle(theme.onAttention)
                        .accessibilityLabel("\(p.needsYou) need you")
                }
            }
            .foregroundStyle(on ? theme.onAction : theme.ink)
            .selectable(sel)
        }
        .opacity(peer.state == "up" && !p.stale ? 1 : 0.55)
    }

    /// A paired server: its name, and whether its panes open here.
    private func remoteHeader(_ peer: AppAPI.Peer) -> some View {
        HStack(spacing: 5) {
            Image(systemName: "network")
            Text(peer.name.isEmpty ? peer.alias : peer.name)
            Spacer()
            Text(peer.state != "up" ? "offline" : peer.panes == "type" ? "write" : peer.panes == "view" ? "view" : "off")
                .font(theme.mono(10.5))
        }
        .help(remoteHelp(peer))
        .accessibilityElement(children: .combine)
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
                Image(systemName: NeedRow.symbol(need.kind))
                    .foregroundStyle(NeedRow.colour(need.kind, theme))
                    .frame(width: 16)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 0) {
                    Text(need.title)
                        .font(theme.body(12.5, .medium))
                        .lineLimit(1)
                    Text(need.detail)
                        .font(theme.body(11))
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
        .accessibilityHint(NeedRow.verb(need.kind))
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
                .font(theme.body(11, .semibold))
                .foregroundStyle(theme.muted)
                .accessibilityHidden(true)
            TextField("Filter", text: $app.filter)
                .textFieldStyle(.plain)
                .font(theme.body(12.5))
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

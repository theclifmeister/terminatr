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
/// drawn as Bloom's violet pill, not the List's own (the system accent).
struct Sidebar: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme

    var body: some View {
        List {
            if !app.needs.isEmpty {
                Section(isExpanded: expanded(Navigation.needsSection)) {
                    let all = app.nav.unfolded.contains(Navigation.moreKey())
                    let shown = all ? app.needs : Array(app.needs.prefix(SidebarTree.needsShown))
                    ForEach(shown) { n in
                        NeedLine(need: n) { app.open(project: n.project, session: n.session) }
                    }
                    if app.needs.count > SidebarTree.needsShown {
                        more(Navigation.moreKey(), all ? "Show fewer" : SidebarTree.moreNeeds(Array(app.needs.dropFirst(SidebarTree.needsShown))), unfolded: all)
                    }
                } header: {
                    HStack(spacing: 6) {
                        Text("Needs you")
                        Text("\(app.needs.count)")
                            .font(theme.mono(10.5, .medium))
                            .padding(.horizontal, 5)
                            .background(theme.attention, in: Capsule())
                            .foregroundStyle(theme.onAttention)
                    }
                }
            }
            Section(isExpanded: expanded(Navigation.sectionKey(server: ""))) {
                ForEach(app.activeProjects, id: \.slug) { p in
                    project(p)
                }
            } header: {
                // "This server" only tells it from paired ones.
                Text(app.peers.isEmpty ? "Projects" : "This server")
            }
            let inactive = app.inactiveProjects
            if !inactive.isEmpty {
                Section(isExpanded: Binding(get: { app.nav.showInactive }, set: { app.nav.showInactive = $0 })) {
                    ForEach(inactive, id: \.slug) { p in
                        project(p)
                    }
                } header: {
                    Text("Inactive (\(inactive.count))")
                        .help("Projects set inactive: View › Show Inactive Projects (⇧⌘H)")
                }
            }
            ForEach(app.peers, id: \.alias) { peer in
                Section(isExpanded: expanded(Navigation.sectionKey(server: peer.alias))) {
                    ForEach(peer.projects, id: \.project) { p in
                        remote(peer, p)
                    }
                } header: {
                    remoteHeader(peer)
                }
            }
        }
        .listStyle(.sidebar)
        .scrollContentBackground(.hidden)
        .background { Frost() }
        .safeAreaInset(edge: .bottom) {
            // Only when something is wrong: all well, it takes no row.
            if StatusLine.shows(app) {
                StatusLine()
                    .padding(12)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
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
                    .font(.system(size: 12))
                    .lineLimit(1)
                Spacer(minLength: 4)
                Image(systemName: unfolded ? "chevron.up" : "chevron.down")
                    .font(.system(size: 9, weight: .semibold))
                    .accessibilityHidden(true)
            }
            .foregroundStyle(theme.muted)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    // MARK: this server's

    /// A project's line, and under it, when it is selected or needs the
    /// user, its coordinator (when it runs or asks) and its threads.
    @ViewBuilder
    private func project(_ p: AppAPI.Project) -> some View {
        let sel = Selection.project(p.slug)
        let needs = app.needs(of: p.slug).count
        let coordinator = app.coordinatorSession(p.slug)
        let state = Attention.of(coordinator: coordinator, in: p)
        ProjectRow(project: p, dot: theme.colour(state.tone), state: state.word, needs: needs, selected: app.nav.selection == sel)
            .selectable(sel)
        if SidebarTree.expands(p, selection: app.nav.selection, needs: needs) {
            if coordinator != nil || state.waits {
                row(.session(SessionRef(project: p.slug)), label: "Coordinator", ref: nil,
                    tag: state.waits ? (state.word, state.tone) : nil, dot: theme.colour(state.tone))
            }
            let key = Navigation.moreKey(project: p.slug)
            let all = app.nav.unfolded.contains(key)
            let selected = app.selectedRef.flatMap { $0.server.isEmpty && $0.project == p.slug ? $0.thread : nil }
            let (shown, more) = SidebarTree.threads(of: p, selected: selected, unfolded: all) { app.attention($0) }
            // Keyed with the project: thread ids repeat across projects,
            // and the List flattens every project's rows into one.
            ForEach(shown, id: \.record.id) { t in
                row(.session(SessionRef(project: p.slug, thread: t.record.id)), label: t.record.title, ref: t.task?.ref,
                    tag: tag(t), dot: theme.colour(app.attention(t).tone))
                    .id("\(p.slug)/\(t.record.id)")
            }
            if more > 0 {
                self.more(key, more == 1 ? "1 more thread" : "\(more) more threads", unfolded: false)
                    .padding(.leading, 14)
            } else if all, !p.threads.isEmpty {
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
                .font(.system(size: 12.5))
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
            ForEach(p.sessions.compactMap { s in SessionRef(s, of: peer.alias).map { (s, $0) } }, id: \.0.id) { s, r in
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
                    .font(.system(size: 13, weight: .semibold))
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
                        .font(.system(size: 12.5, weight: .medium))
                        .lineLimit(1)
                    Text(need.detail)
                        .font(.system(size: 11))
                        .foregroundStyle(theme.muted)
                        .lineLimit(1)
                }
                Spacer(minLength: 0)
            }
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
    let sel: Selection

    func body(content: Content) -> some View {
        let on = app.nav.selection == sel
        content
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(Rectangle())
            .onTapGesture { app.select(sel) }
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
    /// A sidebar row that shows sel in the window: a click selects it,
    /// and the selected one wears the pill.
    func selectable(_ sel: Selection) -> some View {
        modifier(Selectable(sel: sel))
    }
}

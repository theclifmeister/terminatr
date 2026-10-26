import SwiftUI
import TerminatrKit

/// The window's sidebar (design T746): what needs the user, then this
/// server's projects, each with its coordinator and open threads, then
/// each paired server's shared projects with theirs (as the TUI's
/// sidebar has them, alias/session). A project shows its dashboard, a
/// coordinator or thread its session; a need shows what it names. Each
/// section folds, and stays so across launches. The selection is drawn
/// as Bloom's violet pill, not the List's own (the system accent).
struct Sidebar: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme

    var body: some View {
        List {
            if !app.needs.isEmpty {
                Section(isExpanded: expanded(Navigation.needsSection)) {
                    ForEach(app.needs) { n in
                        NeedLine(need: n) { app.open(project: n.project, session: n.session) }
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
                ForEach(app.projects, id: \.slug) { p in
                    project(p)
                }
            } header: {
                Text("This server")
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
            StatusLine()
                .padding(12)
                .frame(maxWidth: .infinity, alignment: .leading)
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

    // MARK: this server's

    private func project(_ p: AppAPI.Project) -> some View {
        let sel = Selection.project(p.slug)
        return DisclosureGroup(isExpanded: expanded(Navigation.sectionKey(server: "", project: p.slug))) {
            row(.session(SessionRef(project: p.slug)), label: "Coordinator", ref: nil, tag: nil)
            ForEach(p.threads.filter { !$0.record.done }, id: \.record.id) { t in
                row(.session(SessionRef(project: p.slug, thread: t.record.id)), label: t.record.title, ref: t.task?.ref, tag: tag(t))
            }
        } label: {
            ProjectRow(project: p, needs: app.needs(of: p.slug).count, selected: app.nav.selection == sel)
                .selectable(sel)
        }
    }

    private func row(_ sel: Selection, label: String, ref: String?, tag: (String, Color)?) -> some View {
        let on = app.nav.selection == sel
        return HStack(spacing: 6) {
            if let ref {
                Text(ref)
                    .font(theme.mono(11.5, .medium))
                    .foregroundStyle(on ? theme.onAction : theme.action)
            }
            Text(label)
                .font(.system(size: 12.5))
                .lineLimit(1)
            Spacer(minLength: 4)
            if let (word, colour) = tag {
                Text(word)
                    .font(theme.mono(10.5, .medium))
                    .foregroundStyle(on ? theme.onAction : colour)
            }
        }
        .foregroundStyle(on ? theme.onAction : theme.ink)
        .selectable(sel)
        .contextMenu {
            if case .session(let r) = sel { NewWindowButton(ref: r) }
        }
    }

    /// A word for what the thread waits on, as the mockup's: a dialog,
    /// a question, a review (Attention, as its card's); else its PR.
    private func tag(_ t: AppAPI.Thread) -> (String, Color)? {
        let a = app.attention(t)
        if a.waits { return (a.word, theme.colour(a.tone)) }
        if t.pr > 0 { return ("PR", theme.muted) }
        return nil
    }

    // MARK: a paired server's

    private func remote(_ peer: AppAPI.Peer, _ p: AppAPI.PeerProject) -> some View {
        let sel = Selection.project(p.project, server: peer.alias)
        let on = app.nav.selection == sel
        return DisclosureGroup(isExpanded: expanded(Navigation.sectionKey(server: peer.alias, project: p.project))) {
            ForEach(p.sessions.compactMap { s in SessionRef(s, of: peer.alias).map { (s, $0) } }, id: \.0.id) { s, r in
                row(.session(r), label: r.isCoordinator ? "Coordinator" : r.thread, ref: s.task.isEmpty ? nil : s.task, tag: tag(s))
            }
        } label: {
            HStack(spacing: 6) {
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
    private func tag(_ s: AppAPI.PeerSession) -> (String, Color)? {
        if s.asking { return ("asks", theme.question) }
        if s.state == "blocked" { return s.reason == "permission" ? ("allow?", theme.approval) : ("blocked", theme.attention) }
        return nil
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

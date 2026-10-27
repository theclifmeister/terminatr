import SwiftUI
import TerminatrKit
import TerminatrUI

/// The command palette (⌘K, T748 milestone E), over the window: words
/// typed find a need, a project, a thread or a task (Palette), or one
/// of the window's actions. ↑ ↓ move, ↩ shows the row, ⌘↩ acts on it
/// (E1): a session's dialog or question shows here with the cards the
/// conversation has, and asks what they ask; a message goes through the
/// composer; a task is delegated once the user confirms it; a task in
/// review shows Accept and Send back…, as the dashboard's cards. Esc
/// goes back, then closes.
struct CommandPalette: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    @State private var query = ""
    @State private var at = 0
    @State private var acting: (item: PaletteItem, ref: SessionRef, prompt: Bool)?
    @State private var delegating: PaletteItem?
    @State private var reviewing: PaletteItem?
    @State private var said: (text: String, failed: Bool)?
    @FocusState private var focused: Bool

    var body: some View {
        VStack(spacing: 0) {
            if let acting {
                ActPanel(item: acting.item, ref: acting.ref, prompt: acting.prompt) { back() }
            } else if let reviewing, case .review(let project, let task) = reviewing.act {
                ReviewPanel(item: reviewing, project: project, task: task) { back() }
            } else {
                search
            }
        }
        .frame(width: 620)
        .frosted(RoundedRectangle(cornerRadius: 16))
        .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 16))
        .overlay(RoundedRectangle(cornerRadius: 16).strokeBorder(theme.edge, lineWidth: 1))
        .shadow(color: .black.opacity(0.35), radius: 30, y: 14)
        .onChange(of: acting == nil) { _, searching in app.paletteActing = !searching }
        .onDisappear { app.paletteActing = false }
        .confirmationDialog(delegating.map { "Delegate \($0.title)?" } ?? "", isPresented: Binding(get: { delegating != nil }, set: { if !$0 { delegating = nil } }),
                            presenting: delegating) { item in
            Button("Ask to Delegate") { delegate(item) }
            Button("Cancel", role: .cancel) {}
        } message: { item in
            Text("\(item.go?.project ?? "The project")'s coordinator is asked to delegate it: it starts a thread for it when it handles the ask.")
        }
    }

    // MARK: searching

    private var rows: [PaletteItem] {
        Palette.search(Palette.items(projects: app.projects, sessions: app.sessions, peers: app.peers, needs: app.needs) + actions, query)
    }

    private var search: some View {
        let rows = rows
        let current = rows.isEmpty ? nil : rows[min(at, rows.count - 1)]
        return VStack(spacing: 0) {
            HStack(spacing: 10) {
                Text("⌘K")
                    .font(theme.mono(.subheadline, .medium))
                    .foregroundStyle(theme.muted)
                TextField("Go to a project, thread, task or need", text: $query)
                    .textFieldStyle(.plain)
                    .font(theme.body(.title3))
                    .focused($focused)
                    .onKeyPress(.upArrow) { move(-1, rows.count); return .handled }
                    .onKeyPress(.downArrow) { move(1, rows.count); return .handled }
                    .onKeyPress(.return, phases: .down) { press in
                        if let current {
                            if press.modifiers.contains(.command) { act(current) } else { go(current) }
                        }
                        return .handled
                    }
                    .onExitCommand { app.palette = false }
                Text("esc")
                    .font(theme.mono(.caption2))
                    .padding(.horizontal, 5)
                    .padding(.vertical, 1)
                    .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(theme.edge, lineWidth: 1))
                    .foregroundStyle(theme.muted)
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 13)
            theme.line.frame(height: 1)
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 1) {
                        ForEach(Array(rows.enumerated()), id: \.element.id) { i, r in
                            if i == 0 || rows[i - 1].group != r.group {
                                Text(r.group.rawValue.uppercased())
                                    .font(theme.mono(.label, .medium))
                                    .tracking(1)
                                    .foregroundStyle(theme.muted)
                                    .padding(.horizontal, 10)
                                    .padding(.top, i == 0 ? 4 : 10)
                                    .padding(.bottom, 3)
                            }
                            row(r, on: r.id == current?.id)
                                .id(r.id)
                                .onTapGesture { go(r) }
                        }
                        if rows.isEmpty {
                            Text("Nothing matches “\(query)”")
                                .foregroundStyle(theme.muted)
                                .frame(maxWidth: .infinity)
                                .padding(.vertical, 18)
                        }
                    }
                    .padding(8)
                }
                .frame(maxHeight: 380)
                .fixedSize(horizontal: false, vertical: true)
                .onChange(of: current?.id) { _, id in
                    if let id { proxy.scrollTo(id) }
                }
            }
            if let said {
                theme.line.frame(height: 1)
                Text(said.text)
                    .font(theme.body(.subheadline))
                    .foregroundStyle(said.failed ? theme.attention : theme.muted)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 16)
                    .padding(.vertical, 8)
            }
        }
        .onAppear { focused = true }
        .onChange(of: query) { at = 0 }
    }

    private func row(_ r: PaletteItem, on: Bool) -> some View {
        HStack(spacing: 9) {
            Circle()
                .fill(r.tone.map { on ? theme.onAction : theme.colour($0) } ?? .clear)
                .frame(width: 7, height: 7)
                .accessibilityHidden(true)
            Text(r.title)
                .font(theme.body(.body, .medium))
                .lineLimit(1)
            if !r.detail.isEmpty {
                Text(r.detail)
                    .font(theme.body(.subheadline))
                    .foregroundStyle(on ? theme.onAction.opacity(0.8) : theme.muted)
                    .lineLimit(1)
            }
            Spacer(minLength: 8)
            Text(hint(r, on: on))
                .font(theme.mono(.caption))
                .foregroundStyle(on ? theme.onAction : theme.muted)
                .fixedSize()
        }
        .foregroundStyle(on ? theme.onAction : theme.ink)
        .padding(.horizontal, 10)
        .padding(.vertical, 7)
        .background(on ? theme.action : .clear, in: RoundedRectangle(cornerRadius: 8))
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(on ? [.isButton, .isSelected] : .isButton)
    }

    /// What the row's keys do: its command's key, else ↩ and ⌘↩ on the
    /// one picked.
    private func hint(_ r: PaletteItem, on: Bool) -> String {
        if !r.keys.isEmpty { return r.keys }
        guard on else { return r.act.map { "⌘↩ \($0.verb)" } ?? "" }
        return ["↩ open", r.act.map { "⌘↩ \($0.verb)" }].compactMap { $0 }.joined(separator: "  ")
    }

    private func move(_ by: Int, _ n: Int) {
        guard n > 0 else { return }
        at = ((min(at, n - 1) + by) % n + n) % n
    }

    // MARK: going and acting

    private func go(_ r: PaletteItem) {
        app.palette = false
        if let c = r.command {
            run(c)
        } else if let s = r.go {
            app.select(s)
        }
    }

    private func act(_ r: PaletteItem) {
        switch r.act {
        case .approve(let ref), .answer(let ref):
            acting = (r, ref, false)
        case .prompt(let ref):
            acting = (r, ref, true)
        case .delegate:
            delegating = r
        case .review:
            reviewing = r
        case nil:
            go(r)
        }
    }

    private func back() {
        acting = nil
        reviewing = nil
        focused = true
    }

    private func delegate(_ r: PaletteItem) {
        guard case .delegate(let project, let task) = r.act else { return }
        said = ("Asking \(project)'s coordinator to delegate \(task)…", false)
        Task {
            do {
                let asked = try await app.delegate(project: project, task: task)
                said = (asked ? "Asked \(project)'s coordinator to delegate \(task)." : "\(task) waits on the coordinator already.", false)
            } catch {
                said = (error.localizedDescription, true)
            }
        }
    }

    // MARK: the window's actions

    private var actions: [PaletteItem] {
        var out = [
            PaletteItem(id: "cmd:next", group: .actions, title: "Next thing that needs you", command: .nextNeed, keys: "⌥⌘↓"),
            PaletteItem(id: "cmd:previous", group: .actions, title: "Previous thing that needs you", command: .previousNeed, keys: "⌥⌘↑"),
        ]
        if !app.inactiveProjects.isEmpty {
            out.append(PaletteItem(id: "cmd:inactive", group: .actions,
                                   title: app.nav.showInactive ? "Hide inactive projects" : "Show inactive projects",
                                   command: .inactive, keys: "⇧⌘H"))
        }
        if let ref = app.selectedRef {
            if !ref.isRemote {
                out.append(PaletteItem(id: "cmd:find", group: .actions, title: "Find in the conversation", command: .find, keys: "⌘F"))
                out.append(PaletteItem(id: "cmd:tab", group: .actions, title: "Conversation or Terminal", command: .tab, keys: "⇧⌘T"))
            }
            out.append(PaletteItem(id: "cmd:inspector", group: .actions, title: app.inspector ? "Hide the inspector" : "Show the inspector",
                                   command: .inspector, keys: "⌥⌘I"))
        }
        if app.controlsProjects, let p = app.shownProject {
            out += Palette.projectActions(p, coordinator: app.coordinatorSession(p.slug), sessions: app.sessions, hint: app.contextHint)
        }
        out += [
            PaletteItem(id: "cmd:filter", group: .actions, title: "Filter the sidebar", command: .filter, keys: "⌥⌘F"),
            PaletteItem(id: "cmd:back", group: .actions, title: "Back", command: .back, keys: "⌘["),
            PaletteItem(id: "cmd:forward", group: .actions, title: "Forward", command: .forward, keys: "⌘]"),
            PaletteItem(id: "cmd:shortcuts", group: .actions, title: "Keyboard shortcuts", command: .shortcuts, keys: "⌘/"),
        ]
        return out + Palette.themes(current: ThemeName.of(UserDefaults.standard.string(forKey: ThemeName.key)))
    }

    private func run(_ c: PaletteItem.Command) {
        switch c {
        case .nextNeed: app.stepNeed(1)
        case .previousNeed: app.stepNeed(-1)
        case .inactive: app.nav.showInactive.toggle()
        case .tab: app.switchTab()
        case .inspector: app.inspector.toggle()
        case .find: app.findInConversation()
        case .filter: app.focusFilter()
        case .back: app.goBack()
        case .forward: app.goForward()
        case .shortcuts: app.shortcuts = true
        case .theme(let t): UserDefaults.standard.set(t.rawValue, forKey: ThemeName.key)
        case .ask(let a): app.ask(a)
        }
    }
}

/// The palette's act on a session: its cards (a dialog to allow, a
/// question to answer) as the conversation shows them, with the same
/// checks and confirmations, or its composer for a message. ⌘↩ takes
/// the first card's first choice here (AppModel.paletteChoice).
private struct ActPanel: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let item: PaletteItem
    let ref: SessionRef
    let prompt: Bool
    let back: () -> Void

    var body: some View {
        let m = app.model(ref)
        let s = app.session(of: m)
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 10) {
                Button(action: back) {
                    Label("Back", systemImage: "chevron.backward")
                }
                .buttonStyle(.borderless)
                .keyboardShortcut(.cancelAction)
                .help("Back to the palette (Esc)")
                Text(app.title(ref))
                    .font(theme.title(.headline, .bold))
                    .lineLimit(1)
                Spacer()
                Button("Open") {
                    app.palette = false
                    app.select(.session(ref))
                }
                .help("Shows the session in the window")
            }
            .padding(.horizontal, 16)
            .padding(.top, 14)
            if prompt {
                Composer(model: m, session: s, autofocus: true)
            } else if AppModel.waitsOnCard(s) {
                Cards(model: m, session: s, firstChoice: app.paletteChoice)
                    .padding(.bottom, 16)
            } else {
                Text("Nothing waits on you there any more.")
                    .foregroundStyle(theme.muted)
                    .padding(.horizontal, 16)
                    .padding(.bottom, 16)
            }
        }
        .task(id: app.bridge == nil) { m.follow(app) }
        .onChange(of: app.sessionID(ref)) { m.follow(app) }
    }
}

/// The palette's act on a task in review: Accept and Send back…
/// (TaskActs), each asking the project's coordinator.
private struct ReviewPanel: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let item: PaletteItem
    let project: String
    let task: String
    let back: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            HStack(spacing: 10) {
                Button(action: back) {
                    Label("Back", systemImage: "chevron.backward")
                }
                .buttonStyle(.borderless)
                .keyboardShortcut(.cancelAction)
                .help("Back to the palette (Esc)")
                Text(item.title)
                    .font(theme.title(.headline, .bold))
                    .lineLimit(1)
                Spacer()
                if let go = item.go {
                    Button("Open") {
                        app.palette = false
                        app.select(go)
                    }
                    .help("Shows it in the window")
                }
            }
            HStack {
                Text("\(project) · ready to review")
                    .foregroundStyle(theme.muted)
                Spacer(minLength: 8)
                TaskActs(project: project, task: task, status: "review")
            }
            TaskOutcome(project: project, task: task)
        }
        .padding(16)
    }
}

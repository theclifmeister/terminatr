import SwiftUI
import TerminatrKit
import TerminatrUI

/// What a session's screen shows (T1001): the Mac's tabs, and the
/// inspector, which on the phone has no panel to be in.
enum SessionPage: String, CaseIterable {
    case conversation = "Conversation"
    case terminal = "Terminal"
    case inspector = "Inspector"
}

/// A coordinator, a program's director (T973) or a thread (T840): its
/// conversation, its terminal or its inspector, as small tabs under its
/// bar (T1001). Its bar and tabs are the screen's own and flat, never the
/// navigation bar's glass: Back (or the list, folded), its title
/// (SessionTitle), a coordinator's menu with Save & Clear (T868). It opens
/// on the conversation (-terminal YES opens it on the terminal, the
/// Simulator's screenshots); the tab is kept for each session while the
/// server is (Places), so going back to one shows what it showed. While the
/// terminal shows, Repaint asks the pane afresh. On a phone's width the tab
/// bar hides here, so the composer has the bottom; beside a list (an iPad,
/// an open Duo) the bar stays.
struct SessionScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    @Environment(\.dynamicTypeSize) private var typeSize
    @Environment(\.horizontalSizeClass) private var width
    @Environment(\.showParent) private var showParent
    @Environment(\.showList) private var showList
    @Environment(\.back) private var back
    @Environment(\.isPresented) private var pushed
    @Environment(\.dismiss) private var dismiss
    let ref: SessionRef
    /// Opened from a need: the conversation opens where it was asked.
    var need: Need?
    @State private var tab: SessionPage = UserDefaults.standard.bool(forKey: "terminal") ? .terminal
        : UserDefaults.standard.bool(forKey: "inspector") ? .inspector : .conversation
    @State private var repaints = 0
    @State private var clearing = false

    var body: some View {
        Group {
            switch tab {
            case .conversation: ConversationScreen(ref: ref, need: need)
            case .terminal: TerminalScreen(ref: ref, repaints: repaints)
            case .inspector: SessionInspector(ref: ref, asking: $clearing)
            }
        }
        .safeAreaInset(edge: .top, spacing: 0) {
            VStack(spacing: 0) {
                bar
                tabs
                if ref.isCoordinator || ref.director, !ref.isRemote, let clear = model.clears[PhoneModel.clearKey(ref)] {
                    SaveClearStatus(clear: clear)
                        .padding(.horizontal, Spacing.gutter)
                        .padding(.vertical, Spacing.tight)
                }
            }
            .background(theme.chrome.ignoresSafeArea(edges: .top))
        }
        .navigationTitle(model.title(ref))
        .toolbar(.hidden, for: .navigationBar)
        .swipesBack()
        .toolbar(width == .compact ? .hidden : .automatic, for: .tabBar)
        .asksSaveClear(ref, isPresented: $clearing)
        .kept($tab, "tab \(ref)")
        // Opening a sleeping coordinator wakes it, as enter on its row in
        // the TUI (T980): it spins until it runs. Only on opening: one
        // that falls asleep while shown stays asleep.
        .task(id: ref) {
            if ref.isCoordinator, !ref.isRemote, model.wakes(ref.project) {
                try? await model.wake(ref.project)
            }
        }
        .environment(\.showTerminal, ShowTerminal { tab = .terminal })
    }

    /// The bar: `.phbar`, Back or the list, the title, its buttons.
    private var bar: some View {
        HStack(spacing: 9) {
            if let showList {
                icon("List", "list.bullet", label: showList.shown ? "Hide the list" : "Show the list", action: showList.toggle)
            } else if let go = back?.action ?? (pushed ? { dismiss() } : nil) {
                Button(action: go) {
                    FlatIcon(.back)
                        .foregroundStyle(theme.muted)
                        .padding(14)
                        .contentShape(Rectangle())
                        .padding(-14)
                }
                .buttonStyle(.plain)
                .hoverEffect()
                .accessibilityLabel("Back")
                .accessibilityIdentifier("BackButton")
            }
            SessionTitle(model.title(ref), place: model.place(ref), role: ref.role, state: model.attention(ref), context: model.context(ref))
            if ref.isCoordinator || ref.director, !ref.isRemote, model.clearsCoordinators, model.leadSession(ref) != nil {
                Menu {
                    SaveClearButton(ref: ref, asking: $clearing)
                } label: {
                    glyph("ellipsis")
                }
                .menuStyle(.button)
                .buttonStyle(.plain)
                .accessibilityLabel("\(model.title(ref)) actions")
            }
            if tab == .terminal, model.sessionID(ref) != nil {
                icon("Repaint", "arrow.clockwise", hint: "Draws its terminal afresh") { repaints += 1 }
            }
        }
        // `.phbar`: 8 over, 9 under and its line.
        .padding(.horizontal, Flat.listGutter)
        .padding(.top, Spacing.item)
        .padding(.bottom, Spacing.row)
        .overlay(alignment: .bottom) { theme.line.frame(height: 1) }
    }

    /// The tabs: `.mini`, and the way to its project or program, beside a
    /// list that isn't theirs (Columns.showParent).
    private var tabs: some View {
        let layout = typeSize.isAccessibilitySize
            ? AnyLayout(VStackLayout(alignment: .leading, spacing: Spacing.tight))
            : AnyLayout(HStackLayout(spacing: Spacing.item))
        return layout {
            MiniTabs(SessionPage.allCases.map { (id: $0, title: $0.rawValue) }, selection: $tab)
                .fixedSize(horizontal: !typeSize.isAccessibilitySize, vertical: true)
                .accessibilityElement(children: .contain)
                .accessibilityLabel("View")
            if !typeSize.isAccessibilitySize { Spacer(minLength: 0) }
            if let showParent { parentChip(showParent) }
        }
        .padding(.horizontal, Flat.listGutter)
        .padding(.top, 7)
        .padding(.bottom, Spacing.item)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(alignment: .bottom) { theme.line.frame(height: 1) }
    }

    private func glyph(_ symbol: String) -> some View {
        Image(systemName: symbol)
            .font(.system(size: 15 * min(theme.scale, 1.3), weight: .medium))
            .foregroundStyle(theme.muted)
            .frame(width: 18, height: 21)
            .padding(.horizontal, 7)
            .padding(.vertical, 12)
            .contentShape(Rectangle())
            .padding(.horizontal, -7)
            .padding(.vertical, -12)
    }

    private func icon(_ title: String, _ symbol: String, label: String? = nil, hint: String = "", action: @escaping () -> Void) -> some View {
        Button(action: action) { glyph(symbol) }
            .buttonStyle(.plain)
            .hoverEffect()
            .accessibilityLabel(label ?? title)
            .accessibilityHint(hint)
    }

    /// In the screen, with the name on it, not the system's bar: an open
    /// iPhone Duo's bar is the rail, where an item is a bare icon that can
    /// outlive its screen.
    private func parentChip(_ parent: ShowParent) -> some View {
        Button(action: parent.go) {
            Label(ref.project, systemImage: ref.director ? "square.stack.3d.up" : "folder")
                .font(theme.sized(12.5))
                .foregroundStyle(theme.ink)
                .lineLimit(1)
                .padding(.vertical, 12)
                .contentShape(Rectangle())
                .padding(.vertical, -12)
        }
        .buttonStyle(.plain)
        .hoverEffect()
        .accessibilityLabel("Open \(ref.director ? "program" : "project") \(ref.project)")
    }
}

/// A session's Inspector tab (T1001): what the Mac's right panel says of
/// it. A thread's task, steps, branch, PR and todos; a coordinator's
/// project; a director's program; a paired server's what that server lets
/// this one see. Under them its context, with Save & Clear on a coordinator
/// or director and Open PR on a thread. Answering stays in the conversation
/// and on Needs you.
struct SessionInspector: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    @Environment(\.openURL) private var openURL
    let ref: SessionRef
    @Binding var asking: Bool

    private var session: AppAPI.Session? {
        model.sessionID(ref).flatMap { id in model.sessions.first { $0.id == id } }
    }

    private var thread: AppAPI.Thread? {
        ref.thread.isEmpty || ref.isRemote ? nil : model.project(ref.project)?.threads.first { $0.record.id == ref.thread }
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                if ref.isRemote {
                    remote
                } else if ref.director {
                    director
                } else if ref.isCoordinator {
                    coordinator
                } else {
                    threadFacts
                }
                agent
            }
            .padding(.horizontal, Spacing.gutter)
            .padding(.vertical, 14)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .refreshable { await model.refresh() }
        .safeAreaInset(edge: .bottom, spacing: 0) { foot }
        .background(theme.pane)
    }

    // MARK: the head of each

    private func head(_ title: String, goal: String = "") -> some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(title)
                .font(theme.sized(15, .semibold))
                .foregroundStyle(theme.ink)
                .fixedSize(horizontal: false, vertical: true)
            if !goal.isEmpty {
                Text(goal)
                    .font(theme.sized(13))
                    .foregroundStyle(theme.muted)
                    .fixedSize(horizontal: false, vertical: true)
            }
            state.padding(.top, 3)
        }
    }

    @ViewBuilder private var state: some View {
        if let a = model.attention(ref) {
            HStack(spacing: Spacing.tight) {
                if let role = ref.role { RoleLabel(role, points: 12.5) }
                FlatDot(a.tone)
                Text(a.word.prefix(1).uppercased() + a.word.dropFirst())
                    .font(theme.sized(12.5))
                    .foregroundStyle(theme.muted)
            }
            .accessibilityElement(children: .combine)
        } else if let role = ref.role {
            RoleLabel(role, points: 12.5)
        }
    }

    // MARK: each

    @ViewBuilder private var coordinator: some View {
        let p = model.project(ref.project)
        head(ref.project, goal: p?.goal ?? "")
        if let p {
            InspectorSection("Project") {
                FactList {
                    FactRow("Threads", "\(p.threads.filter { !$0.record.done }.count) open")
                    let needs = Need.count(model.needs, project: p.slug)
                    if needs > 0 { FactRow("Needs you", "\(needs)") }
                    if p.questions > 0 { FactRow("Questions", "\(p.questions) open") }
                    FactRow("On deck", "\(Dashboard.onDeck(p).count)")
                    FactRow("Backlog", "\(Dashboard.backlog(p).count)")
                    if model.listsDone { FactRow("Done", "\(Dashboard.doneCount(p))") }
                    if p.usage.costUsd > 0 { FactRow("Cost", String(format: "$%.2f", p.usage.costUsd)) }
                }
            }
        }
    }

    @ViewBuilder private var director: some View {
        let g = model.program(ref.project)
        head(ref.project, goal: g?.goal ?? "")
        if let g {
            InspectorSection("Program") {
                FactList {
                    FactRow("Authority", g.authority)
                    FactRow("Projects", "\(g.members.count)")
                    let needs = model.programNeeds(g).count
                    if needs > 0 { FactRow("Needs you", "\(needs)") }
                    if g.questions > 0 { FactRow("Questions", "\(g.questions) open") }
                }
            }
        }
    }

    @ViewBuilder private var threadFacts: some View {
        let t = thread
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: Spacing.item) {
                if let task = t?.task {
                    Text(task.ref)
                        .font(theme.monoSized(12.5, .medium))
                        .foregroundStyle(theme.action)
                }
                Text(ref.thread)
                    .font(theme.monoSized(12.5))
                    .foregroundStyle(theme.muted)
            }
            Text(t?.task?.title ?? t?.record.title ?? ref.thread)
                .font(theme.sized(15, .semibold))
                .foregroundStyle(theme.ink)
                .fixedSize(horizontal: false, vertical: true)
            state.padding(.top, 2)
        }
        if let task = t?.task, !task.steps.isEmpty {
            let current = task.steps.first { !$0.done }?.n
            InspectorSection("Steps · \(task.steps.filter(\.done).count) of \(task.steps.count)") {
                ForEach(task.steps, id: \.n) { s in
                    CheckRow(text: s.text, done: s.done, current: s.n == current)
                }
            }
        }
        if let t {
            InspectorSection("Thread") {
                FactList {
                    if !t.record.branch.isEmpty { FactRow("Branch", t.record.branch, mono: true) }
                    FactRow("PR", t.pr > 0 ? "#\(t.pr)" + (t.checks.isEmpty ? "" : " · checks \(t.checks)") : "none yet")
                    FactRow("Started", t.record.created.formatted(date: .abbreviated, time: .shortened))
                    ForEach(t.record.usage.factRows, id: \.label) { FactRow($0.label, $0.value) }
                }
            }
            if let todos = t.status?.todos, !todos.isEmpty {
                InspectorSection("Todos") {
                    ForEach(Array(todos.enumerated()), id: \.offset) { _, d in
                        CheckRow(text: d.text, done: d.status == "completed", current: d.status == "in_progress")
                    }
                }
            }
        }
    }

    @ViewBuilder private var remote: some View {
        let peer = model.peers.first { $0.alias == ref.server }
        let s = ref.session(in: model.peers)
        let p = peer?.projects.first { $0.project == ref.project }
        head(ref.project, goal: p?.goal ?? "")
        InspectorSection("Paired server") {
            FactList {
                FactRow("Server", peer.map { $0.name.isEmpty ? $0.alias : "\($0.name) (\($0.alias))" } ?? ref.server)
                FactRow("Link", peer?.state ?? "not paired")
                if p?.stale == true { FactRow("Read", "stale") }
            }
            Text("Its conversation stays there: only the pane crosses the link.")
                .font(theme.sized(12))
                .foregroundStyle(theme.muted)
                .fixedSize(horizontal: false, vertical: true)
        }
        if let s {
            InspectorSection("Agent") {
                FactList {
                    FactRow("Agent", [s.agent, s.model].filter { !$0.isEmpty }.joined(separator: " · "))
                    if !s.current.isEmpty { FactRow("Now", s.current) }
                    if s.todosTotal > 0 { FactRow("Todos", "\(s.todosDone) of \(s.todosTotal)") }
                    if s.queuedPrompts > 0 { FactRow("Queued", "\(s.queuedPrompts) prompts") }
                    FactRow("Session", s.id, mono: true)
                }
            }
        }
    }

    @ViewBuilder private var agent: some View {
        if !ref.isRemote, let s = session {
            InspectorSection("Agent") {
                FactList {
                    FactRow("Agent", [s.agent, s.model].filter { !$0.isEmpty }.joined(separator: " · "))
                    if s.queuedPrompts > 0 { FactRow("Queued", "\(s.queuedPrompts) prompts") }
                    FactRow("Session", s.id, mono: true)
                }
            }
        }
    }

    // MARK: the foot

    private var prURL: URL? {
        guard let s = thread?.reportPr, let u = URL(string: s), SafeMarkdown.allowed(u) else { return nil }
        return u
    }

    @ViewBuilder private var foot: some View {
        let context = model.context(ref)
        let lead = (ref.isCoordinator || ref.director) && !ref.isRemote && model.clearsCoordinators && model.leadSession(ref) != nil
        let url = prURL
        if context != nil || lead || url != nil {
            HStack(spacing: Spacing.item) {
                if let context {
                    HStack(spacing: Spacing.item) {
                        Text("Context")
                        Meter(fraction: Double(context.percent) / 100, colour: context.tone.map { theme.colour($0) } ?? theme.muted)
                            .frame(height: 4)
                        Text(context.word).monospacedDigit()
                    }
                    .font(theme.sized(13))
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
                    .padding(.horizontal, 11)
                    .frame(minHeight: 34 * theme.scale)
                    .frame(maxWidth: .infinity)
                    .overlay(RoundedRectangle(cornerRadius: Radius.row).strokeBorder(theme.line, lineWidth: 1))
                    .accessibilityElement(children: .ignore)
                    .accessibilityLabel("Context")
                    .accessibilityValue(context.spoken)
                } else {
                    Spacer(minLength: 0)
                }
                if lead {
                    Button("Save & Clear") { asking = true }
                        .buttonStyle(.secondary)
                } else if let url {
                    Button("Open PR") { openURL(url) }
                        .buttonStyle(.secondary)
                }
            }
            .padding(.horizontal, 12)
            .padding(.vertical, Spacing.row)
            .frame(maxWidth: .infinity)
            .background(theme.chrome.ignoresSafeArea(edges: .bottom))
            .overlay(alignment: .top) { theme.line.frame(height: 1) }
        }
    }
}

/// Switches a session's screen to its terminal: an answer its
/// conversation's card couldn't type offers it (NeedActs).
struct ShowTerminal {
    let go: () -> Void
}

extension EnvironmentValues {
    @Entry var showTerminal: ShowTerminal?
}

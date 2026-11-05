import SwiftUI
import TerminatrKit
import TerminatrUI

/// The right panel (T1000): the inspector of whatever the main pane
/// shows, a session's or, since T1000, a project's or program's too.
struct InspectorPanel: View {
    @Environment(AppModel.self) private var app
    let selection: Selection?

    var body: some View {
        switch selection {
        case .session(let ref):
            let m = app.model(ref)
            Inspector(model: m, session: app.session(of: m))
                .id(ref)
        case .project(let slug, let server) where server.isEmpty:
            if let p = app.project(slug) {
                ProjectInspector(project: p)
                    .id(slug)
            } else {
                InspectorFrame(head: InspectorHead(chip: "", count: "")) { EmptyView() }
            }
        case .project(let slug, let server):
            if let peer = app.peer(server), let p = peer.projects.first(where: { $0.project == slug }) {
                RemoteProjectInspector(peer: peer, project: p)
                    .id(server + "/" + slug)
            } else {
                InspectorFrame(head: InspectorHead(chip: "", count: "")) { EmptyView() }
            }
        case .program(let slug):
            if let g = app.program(slug) {
                ProgramInspector(program: g)
                    .id(slug)
            } else {
                InspectorFrame(head: InspectorHead(chip: "", count: "")) { EmptyView() }
            }
        case nil:
            EmptyView()
        }
    }
}

/// The panel's frame, to the reference's measure: 8 round with its line,
/// a header of 47 with its chip and count, what scrolls, and a footer of
/// 59 with a context meter and one button, when it has them.
struct InspectorFrame<Content: View>: View {
    @Environment(\.theme) private var theme
    let head: InspectorHead
    /// How much of its context the session has used.
    var context: ContextUse?
    /// The footer's button.
    var foot: InspectorFootButton?
    @ViewBuilder var content: Content

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: 8)
        VStack(spacing: 0) {
            header
            ScrollView {
                VStack(alignment: .leading, spacing: 18) {
                    content
                }
                .padding(.horizontal, 15)
                .padding(.vertical, 14)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .scrollEdges()
            .frame(maxHeight: .infinity)
            if context != nil || foot != nil {
                footer
            }
        }
        .guardingLinks()
        .background(theme.pane)
        .clipShape(shape)
        .overlay { shape.strokeBorder(theme.line, lineWidth: 1).allowsHitTesting(false) }
    }

    private var header: some View {
        HStack(spacing: 9) {
            Text("Inspector")
                .font(theme.sized(14, .semibold))
                .accessibilityAddTraits(.isHeader)
            if !head.chip.isEmpty {
                Text(head.chip)
                    .font(.system(size: 11.5 * theme.scale, design: .monospaced))
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
                    .padding(.horizontal, 6)
                    .padding(.vertical, 1)
                    .overlay(RoundedRectangle(cornerRadius: 4).strokeBorder(theme.line, lineWidth: 1))
            }
            Spacer(minLength: 0)
            if !head.count.isEmpty {
                Text(head.count)
                    .font(.system(size: 12.5 * theme.scale, design: .monospaced))
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
            }
        }
        .padding(.horizontal, 15)
        .frame(height: 46)
        .padding(.bottom, 1)
        .overlay(alignment: .bottom) { theme.line.frame(height: 1) }
        .accessibilityElement(children: .combine)
    }

    private var footer: some View {
        HStack(spacing: 8) {
            if let context {
                HStack(spacing: 8) {
                    Text("Context")
                    Meter(fraction: Double(context.percent) / 100, colour: context.tone.map { theme.colour($0) } ?? theme.muted)
                        .frame(height: 4)
                    Text(context.word)
                        .monospacedDigit()
                }
                .font(theme.sized(13))
                .foregroundStyle(theme.muted)
                .lineLimit(1)
                .padding(.horizontal, 11)
                .frame(height: 34)
                .frame(maxWidth: .infinity)
                .overlay(RoundedRectangle(cornerRadius: 6).strokeBorder(theme.line, lineWidth: 1))
                .help(context.spoken)
                .accessibilityElement(children: .ignore)
                .accessibilityLabel("Context")
                .accessibilityValue(context.spoken)
            } else {
                Spacer(minLength: 0)
            }
            if let foot {
                foot
            }
        }
        .padding(.horizontal, 12)
        .frame(height: 58)
        .padding(.top, 1)
        .overlay(alignment: .top) { theme.line.frame(height: 1) }
    }
}

/// The footer's button: Save & Clear on a coordinator, Open PR on a
/// thread. 79 × 35, 6 round.
struct InspectorFootButton: View {
    @Environment(\.theme) private var theme
    @Environment(\.isEnabled) private var enabled
    @Environment(\.openURL) private var openURL
    let title: String
    let help: String
    var url: URL?
    var action: (() -> Void)?

    var body: some View {
        Button {
            if let url { openURL(url) } else { action?() }
        } label: {
            Text(title)
                .font(theme.sized(title.count > 8 ? 12 : 13))
                .foregroundStyle(enabled ? theme.ink : theme.muted)
                .lineLimit(1)
                .frame(width: 79, height: 35)
                .background(theme.tab, in: RoundedRectangle(cornerRadius: 6))
                .contentShape(RoundedRectangle(cornerRadius: 6))
        }
        .buttonStyle(.plain)
        .disabled(url == nil && action == nil)
        .help(help)
    }
}

/// Beside the session: a thread's task and steps, its branch, PR and
/// context, the agent's todos; a coordinator's project and what needs
/// the user. A paired server's session shows what that server lets this
/// one see of it (peer.list): its link, its switch, the session's state.
struct Inspector: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let model: SessionModel
    let session: AppAPI.Session?

    var body: some View {
        InspectorFrame(head: head, context: context, foot: foot) {
            if model.ref.isRemote {
                remote
            } else if model.ref.isCoordinator {
                if let p = app.project(model.ref.project) {
                    ProjectFacts(project: p, state: sessionState)
                } else {
                    Text(model.ref.project).font(theme.sized(15, .semibold))
                }
            } else if model.ref.director {
                director
            } else {
                thread
            }
            agent
            if !model.transcript.todos.isEmpty {
                InspectorSection("Todos") {
                    ForEach(Array(model.transcript.todos.enumerated()), id: \.offset) { _, t in
                        CheckRow(text: t.text, done: t.status == "completed", current: t.status == "in_progress")
                    }
                }
            }
        }
    }

    // MARK: the header and the footer

    private var head: InspectorHead {
        let ref = model.ref
        let use = context?.percent
        if ref.isRemote {
            let s = ref.session(in: app.peers)
            if ref.isCoordinator {
                let needs = app.peer(ref.server)?.projects.first { $0.project == ref.project }?.needsYou ?? 0
                return .project(needs: needs, context: use)
            }
            let todos = s.map { $0.todosTotal > 0 ? "\($0.todosDone) of \($0.todosTotal)" : "" } ?? ""
            return InspectorHead(chip: s?.task.isEmpty == false ? s!.task : ref.thread, count: todos)
        }
        if ref.isCoordinator { return .project(needs: app.needs(of: ref.project).count, context: use) }
        if ref.director {
            return .project(needs: app.program(ref.project).map { app.needs(of: $0).count } ?? 0, context: use)
        }
        let t = app.thread(ref)
        return .thread(ref: t?.task?.ref, id: ref.thread, steps: t?.task?.steps ?? [])
    }

    private var context: ContextUse? {
        if model.ref.isRemote, let s = model.ref.session(in: app.peers), s.contextWindow > 0 {
            return ContextUse(percent: s.context * 100 / s.contextWindow, level: .plain)
        }
        return ContextUse.of(session, hint: app.contextHint)
    }

    private var foot: InspectorFootButton? {
        let ref = model.ref
        if ref.isCoordinator || ref.director {
            guard !ref.isRemote, model.sessionID != nil, app.controlsProjects else { return nil }
            let who = ref.director ? "the director" : "the coordinator"
            return InspectorFootButton(title: "Save & Clear",
                                       help: ["Save & Clear: \(who) saves what it learned, then its conversation is cleared, as tm clear does",
                                              context.map { "(\($0.spoken))" }].compactMap { $0 }.joined(separator: " ")) {
                app.ask(ref.director ? .saveClearDirector(program: ref.project) : .saveClear(project: ref.project))
            }
        }
        guard !ref.isRemote, let t = app.thread(ref) else { return nil }
        let url = prURL(t)
        return InspectorFootButton(title: "Open PR", help: url == nil ? "No PR yet" : "Open the PR", url: url)
    }

    // MARK: a thread

    @ViewBuilder
    private var thread: some View {
        let t = app.thread(model.ref)
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 8) {
                if let task = t?.task {
                    Text(task.ref)
                        .font(.system(size: 12.5 * theme.scale, weight: .medium, design: .monospaced))
                        .foregroundStyle(theme.action)
                }
                Text(model.ref.thread)
                    .font(.system(size: 12.5 * theme.scale, design: .monospaced))
                    .foregroundStyle(theme.muted)
            }
            Text(t?.task?.title ?? t?.record.title ?? model.ref.thread)
                .font(theme.sized(15, .semibold))
                .fixedSize(horizontal: false, vertical: true)
            sessionState
                .padding(.top, 2)
        }
        if let task = t?.task, !task.steps.isEmpty {
            let current = task.steps.first { !$0.done }?.n
            InspectorSection("Steps · \(task.steps.filter(\.done).count) of \(task.steps.count)") {
                ForEach(task.steps, id: \.n) { s in
                    CheckRow(text: s.text, done: s.done, current: s.n == current)
                }
            }
        }
        if let t, let r = t.record as AppAPI.ThreadRecord? {
            InspectorSection("Thread") {
                FactList {
                    if !r.branch.isEmpty { FactRow("Branch", r.branch, mono: true) }
                    FactRow("PR", pr(t))
                    FactRow("State", app.attention(t).word)
                    FactRow("Started", r.created.formatted(date: .abbreviated, time: .shortened))
                    if r.usage.costUsd > 0 { FactRow("Cost", String(format: "$%.2f", r.usage.costUsd)) }
                }
            }
        }
    }

    private func pr(_ t: AppAPI.Thread) -> String {
        if t.pr > 0 { return "#\(t.pr)" + (t.checks.isEmpty ? "" : " · checks \(t.checks)") }
        if let w = model.watch, !w.pr.isEmpty { return w.pr }
        return "none yet"
    }

    private func prURL(_ t: AppAPI.Thread) -> URL? {
        let s = model.watch?.prUrl.isEmpty == false ? model.watch!.prUrl : t.reportPr
        guard let u = URL(string: s), SafeMarkdown.allowed(u) else { return nil }
        return u
    }

    // MARK: a program's director

    /// A director's: its program, its goal and its needs, as a
    /// coordinator's has its project's.
    @ViewBuilder
    private var director: some View {
        if let g = app.program(model.ref.project) {
            ProgramFactsView(program: g, state: sessionState)
        } else {
            Text(model.ref.project).font(theme.sized(15, .semibold))
        }
    }

    // MARK: a paired server's

    @ViewBuilder
    private var remote: some View {
        let ref = model.ref
        let peer = app.peer(ref.server)
        let s = ref.session(in: app.peers)
        let project = peer?.projects.first { $0.project == ref.project }
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 8) {
                if let task = s?.task, !task.isEmpty {
                    Text(task)
                        .font(.system(size: 12.5 * theme.scale, weight: .medium, design: .monospaced))
                        .foregroundStyle(theme.action)
                }
                Text(ref.isCoordinator ? "coordinator" : ref.thread)
                    .font(.system(size: 12.5 * theme.scale, design: .monospaced))
                    .foregroundStyle(theme.muted)
            }
            Text(ref.project).font(theme.sized(15, .semibold))
            if let goal = project?.goal, !goal.isEmpty {
                Text(goal).font(theme.sized(13)).foregroundStyle(theme.muted)
            }
            if let s, !s.state.isEmpty {
                let blocked = s.state == "blocked" || s.asking
                StateLine(word: s.asking ? "asks" : blocked ? (s.reason.isEmpty ? "blocked" : s.reason) : s.state,
                          colour: blocked ? theme.approval : s.state == "working" ? theme.good : theme.idle)
                    .padding(.top, 2)
            }
        }
        InspectorSection("Paired server") {
            FactList {
                FactRow("Server", peer.map { $0.name.isEmpty ? $0.alias : "\($0.name) (\($0.alias))" } ?? ref.server)
                FactRow("Link", peer?.state ?? "not paired")
                FactRow("Panes", panes(app.remotePane(ref)))
                if project?.stale == true { FactRow("Read", "stale") }
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
                    FactRow("Started", s.created.formatted(date: .abbreviated, time: .shortened))
                    FactRow("Session", s.id, mono: true)
                }
            }
        }
    }

    private func panes(_ r: RemotePane) -> String {
        switch r {
        case .write: "write"
        case .view: "view only"
        case .closed: "closed"
        }
    }

    // MARK: the agent

    @ViewBuilder
    private var agent: some View {
        if let s = session {
            InspectorSection("Agent") {
                FactList {
                    FactRow("Agent", [s.agent, s.model].filter { !$0.isEmpty }.joined(separator: " · "))
                    if s.queuedPrompts > 0 { FactRow("Queued", "\(s.queuedPrompts) prompts") }
                    FactRow("Session", s.id, mono: true)
                }
            }
        }
    }

    // MARK: parts

    /// The session's state, with its dot.
    @ViewBuilder
    private var sessionState: some View {
        if let s = session, !s.state.isEmpty {
            let blocked = s.state == "blocked"
            let word = blocked ? (s.reason == "permission" ? "needs approval" : s.reason.isEmpty ? "blocked" : s.reason) : s.state
            StateLine(word: word, colour: blocked ? theme.approval : s.state == "working" ? theme.good : theme.idle)
        }
    }
}

/// A project of this server in the panel: what its coordinator's panel
/// says, the context and Save & Clear of its coordinator while it runs.
struct ProjectInspector: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let project: AppAPI.Project

    var body: some View {
        let session = app.coordinatorSession(project.slug)
        let context = ContextUse.of(session, hint: app.contextHint)
        let state = app.coordinator(of: project)
        InspectorFrame(head: .project(needs: app.needs(of: project.slug).count, context: context?.percent), context: context,
                       foot: session != nil && app.controlsProjects ? InspectorFootButton(
                        title: "Save & Clear",
                        help: "Save & Clear: the coordinator saves what it learned, then its conversation is cleared, as tm clear does") {
                            app.ask(.saveClear(project: project.slug))
                        } : nil) {
            ProjectFacts(project: project, state: StateLine(word: "Coordinator \(state.word)", colour: theme.colour(state.tone)))
        }
    }
}

/// A project's title, goal and state, what needs the user of it, and its
/// counts: a project's panel and its coordinator's.
struct ProjectFacts<State: View>: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let project: AppAPI.Project
    let state: State

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(project.slug).font(theme.sized(15, .semibold))
            if !project.goal.isEmpty {
                Text(project.goal)
                    .font(theme.sized(13))
                    .foregroundStyle(theme.muted)
                    .fixedSize(horizontal: false, vertical: true)
            }
            state.padding(.top, 3)
        }
        let needs = app.needs(of: project.slug)
        if !needs.isEmpty {
            InspectorSection("Needs you · \(needs.count)") {
                ForEach(needs) { PanelNeedRow(need: $0) }
            }
        }
        InspectorSection("Project") {
            FactList {
                FactRow("Threads", "\(project.threads.filter { !$0.record.done }.count) open")
                if project.questions > 0 { FactRow("Questions", "\(project.questions) open") }
                FactRow("On deck", "\(Dashboard.onDeck(project).count)")
                FactRow("Backlog", "\(Dashboard.backlog(project).count)")
                if app.listsDone { FactRow("Done", "\(Dashboard.doneCount(project))") }
                if project.usage.costUsd > 0 { FactRow("Cost", String(format: "$%.2f", project.usage.costUsd)) }
            }
        }
    }
}

/// A need in the panel: a click shows what it names.
private struct PanelNeedRow: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let need: Need

    var body: some View {
        Button { app.open(project: need.project, session: need.session) } label: {
            HStack(alignment: .firstTextBaseline, spacing: 7) {
                Dot(colour: theme.colour(need.kind))
                VStack(alignment: .leading, spacing: 0) {
                    Text(need.what).font(theme.sized(13)).lineLimit(2)
                    Text(need.detail).font(theme.sized(12.5)).foregroundStyle(theme.muted).lineLimit(1)
                }
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}

/// A paired server's project in the panel: what that server lists of it.
struct RemoteProjectInspector: View {
    @Environment(\.theme) private var theme
    let peer: AppAPI.Peer
    let project: AppAPI.PeerProject

    var body: some View {
        InspectorFrame(head: .project(needs: project.stale ? 0 : project.needsYou, context: nil)) {
            VStack(alignment: .leading, spacing: 3) {
                Text(project.project).font(theme.sized(15, .semibold))
                if !project.goal.isEmpty {
                    Text(project.goal)
                        .font(theme.sized(13))
                        .foregroundStyle(theme.muted)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            InspectorSection("Project") {
                FactList {
                    FactRow("Threads", "\(project.threads)")
                    if project.paused { FactRow("State", "paused") }
                    if project.stale { FactRow("Read", "stale") }
                }
            }
            InspectorSection("Paired server") {
                FactList {
                    FactRow("Server", peer.name.isEmpty ? peer.alias : "\(peer.name) (\(peer.alias))")
                    FactRow("Link", peer.state)
                }
            }
        }
    }
}

/// A program in the panel, with its director's context and Save & Clear
/// while it runs.
struct ProgramInspector: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let program: AppAPI.Program

    var body: some View {
        let session = app.directorSession(program.slug)
        let context = ContextUse.of(session, hint: app.contextHint)
        let state = Attention.of(director: session, in: program)
        InspectorFrame(head: .project(needs: app.needs(of: program).count, context: context?.percent), context: context,
                       foot: session != nil && app.controlsProjects ? InspectorFootButton(
                        title: "Save & Clear",
                        help: "Save & Clear: the director saves what it learned, then its conversation is cleared, as tm clear does") {
                            app.ask(.saveClearDirector(program: program.slug))
                        } : nil) {
            ProgramFactsView(program: program, state: StateLine(word: "Director \(state.word)", colour: theme.colour(state.tone)))
        }
    }
}

/// A program's title, goal and state, and its facts.
struct ProgramFactsView<State: View>: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let program: AppAPI.Program
    let state: State

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text("director")
                .font(.system(size: 12.5 * theme.scale, design: .monospaced))
                .foregroundStyle(theme.muted)
            Text(program.slug).font(theme.sized(15, .semibold))
            if !program.goal.isEmpty {
                Text(program.goal)
                    .font(theme.sized(13))
                    .foregroundStyle(theme.muted)
                    .fixedSize(horizontal: false, vertical: true)
            }
            state.padding(.top, 3)
        }
        InspectorSection("Program") {
            FactList {
                FactRow("Authority", program.authority)
                FactRow("Projects", "\(program.members.count)")
                if program.questions > 0 { FactRow("Questions", "\(program.questions) open") }
            }
            Button("Show the Program") { app.select(.program(program.slug)) }
                .buttonStyle(.link)
                .font(theme.sized(13))
        }
    }
}

// MARK: the panel's parts

/// A state in words beside its dot.
struct StateLine: View {
    @Environment(\.theme) private var theme
    let word: String
    let colour: Color

    var body: some View {
        HStack(spacing: 6) {
            Dot(colour: colour)
            Text(word.prefix(1).uppercased() + word.dropFirst())
                .font(theme.sized(12.5))
                .foregroundStyle(theme.muted)
        }
        .accessibilityElement(children: .combine)
    }
}

/// A group of the panel under its label.
struct InspectorSection<Content: View>: View {
    @Environment(\.theme) private var theme
    let title: String
    @ViewBuilder var content: Content

    init(_ title: String, @ViewBuilder content: () -> Content) {
        self.title = title
        self.content = content()
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(title)
                .font(theme.sized(12))
                .foregroundStyle(theme.muted)
                .accessibilityAddTraits(.isHeader)
            VStack(alignment: .leading, spacing: 5) {
                content
            }
        }
    }
}

/// Facts in their two columns: the label's 62 and the value.
struct FactList<Content: View>: View {
    @ViewBuilder var content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            content
        }
    }
}

struct FactRow: View {
    @Environment(\.theme) private var theme
    let label: String
    let value: String
    var mono = false

    init(_ label: String, _ value: String, mono: Bool = false) {
        self.label = label
        self.value = value
        self.mono = mono
    }

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Text(label)
                .foregroundStyle(theme.muted)
                .frame(minWidth: 62, alignment: .leading)
            Text(value)
                .font(mono ? .system(size: 12 * theme.scale, design: .monospaced) : theme.sized(13))
                .lineLimit(1)
                .truncationMode(.middle)
                .textSelection(.enabled)
                .help(value)
        }
        .font(theme.sized(13))
        .accessibilityElement(children: .combine)
    }
}

/// A step or a todo: done, current or to do.
struct CheckRow: View {
    @Environment(\.theme) private var theme
    let text: String
    let done: Bool
    let current: Bool

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 7) {
            Image(systemName: done ? "checkmark.circle" : current ? "circle.inset.filled" : "circle")
                .font(.system(size: 12))
                .foregroundStyle(done ? theme.good : current ? theme.ink : theme.faint)
                .frame(width: 15)
            Text(text)
                .font(theme.sized(13))
                .foregroundStyle(done ? theme.muted : theme.ink)
                .fixedSize(horizontal: false, vertical: true)
        }
        .accessibilityElement(children: .combine)
        .accessibilityValue(done ? "done" : current ? "current" : "")
    }
}

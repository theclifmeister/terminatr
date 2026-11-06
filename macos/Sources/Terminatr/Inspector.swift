import SwiftUI
import TerminatrKit
import TerminatrUI

/// The right panel (T1000, T1016): action first. What needs the user, or a
/// thread's current step, in a card; everything else folded with its
/// one-line summary. The header says what's shown, the footer its context.
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
                InspectorFrame(head: .empty) { EmptyView() }
            }
        case .project(let slug, let server):
            if let peer = app.peer(server), let p = peer.projects.first(where: { $0.project == slug }) {
                RemoteProjectInspector(peer: peer, project: p)
                    .id(server + "/" + slug)
            } else {
                InspectorFrame(head: .empty) { EmptyView() }
            }
        case .program(let slug):
            if let g = app.program(slug) {
                ProgramInspector(program: g)
                    .id(slug)
            } else {
                InspectorFrame(head: .empty) { EmptyView() }
            }
        case nil:
            EmptyView()
        }
    }
}

/// The panel's frame: 8 round with its line, a header at the main pane's
/// height, what scrolls, and a footer at the composer's height at rest (58
/// with its line, so their top lines meet) with the context and one plain
/// button centred in it, when it has them.
struct InspectorFrame<Content: View>: View {
    @Environment(\.theme) private var theme
    let head: InspectorHead
    /// How much of its context the session has used.
    var context: ContextUse?
    /// The footer's button.
    var foot: InspectorFootButton?
    @ViewBuilder var content: Content

    var body: some View {
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
        .modifier(PanelGround())
    }

    private var header: some View {
        PanelHead {
            switch head.name {
            case let .role(role, name):
                RoleMark(role, points: 13)
                Text(name)
                    .font(theme.sized(13, .medium))
                    .lineLimit(1)
                    .truncationMode(.middle)
            case let .ids(task, thread):
                if !task.isEmpty {
                    Text(task)
                        .font(theme.monoSized(13, .medium))
                        .foregroundStyle(theme.action)
                        .lineLimit(1)
                        .layoutPriority(1)
                }
                Text(thread)
                    .font(theme.monoSized(13))
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
                    .truncationMode(.middle)
            case nil:
                EmptyView()
            }
            Spacer(minLength: 0)
            if let state = head.state, !state.word.isEmpty {
                StateLine(word: state.word, colour: state.tone == .working ? theme.good : theme.colour(state.tone))
                    .lineLimit(1)
                    .fixedSize()
            }
        }
        .help(head.who)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(head.spoken)
        .accessibilityAddTraits(.isHeader)
    }

    private var footer: some View {
        HStack(spacing: 10) {
            if let context {
                HStack(spacing: 10) {
                    Text("\(context.word) context")
                        .monospacedDigit()
                        .lineLimit(1)
                        .fixedSize()
                    Meter(fraction: context.fraction, colour: context.tone.map { theme.colour($0) } ?? theme.muted)
                        .frame(minWidth: 20)
                        .frame(height: 4)
                }
                .help(context.spoken)
                .accessibilityElement(children: .ignore)
                .accessibilityLabel(context.spoken)
            } else {
                Spacer(minLength: 0)
            }
            if let foot {
                foot
            }
        }
        .font(theme.sized(12.5))
        .foregroundStyle(theme.muted)
        .modifier(PanelFootBar())
    }
}

/// The panel's ground: 8 round with its line. Both its views.
struct PanelGround: ViewModifier {
    @Environment(\.theme) private var theme

    func body(content: Content) -> some View {
        let shape = RoundedRectangle(cornerRadius: 8)
        content
            .background(theme.pane)
            .clipShape(shape)
            .overlay { shape.strokeBorder(theme.line, lineWidth: 1).allowsHitTesting(false) }
    }
}

/// The panel's header, at the main pane's height with its line on the same
/// row of pixels (T1011). Both its views.
struct PanelHead<Content: View>: View {
    @Environment(\.theme) private var theme
    @ViewBuilder var content: Content

    var body: some View {
        HStack(spacing: 8) {
            content
        }
        .padding(.horizontal, 13)
        .frame(height: 33)
        .padding(.bottom, 1)
        .overlay(alignment: .bottom) { theme.line.frame(height: 1) }
    }
}

/// The panel's footer, at the composer's height at rest: its line above and
/// the panel's own below, what's in it centred between them. Both its views.
struct PanelFootBar: ViewModifier {
    @Environment(\.theme) private var theme

    func body(content: Content) -> some View {
        content
            .padding(.horizontal, 15)
            .frame(height: 56)
            .padding(.vertical, 1)
            .overlay(alignment: .top) { theme.line.frame(height: 1) }
    }
}

/// The footer's button, only its words: Save & Clear on a coordinator,
/// Open PR on a thread.
struct InspectorFootButton: View {
    @Environment(\.openURL) private var openURL
    let title: String
    let help: String
    var url: URL?
    var action: (() -> Void)?

    var body: some View {
        Button(title) {
            if let url { openURL(url) } else { action?() }
        }
        .buttonStyle(FlatTextButtonStyle(points: 12.5))
        .fixedSize()
        .disabled(url == nil && action == nil)
        .help(help)
    }
}

/// A group folded under its name, with what's in it in one line while
/// closed (T1016). Closed by default; each name's stays as the user left
/// it, across launches and across the panels that have it.
struct InspectorFold<Content: View>: View {
    @Environment(\.theme) private var theme
    let title: String
    let summary: String
    @ViewBuilder var content: Content
    @AppStorage private var open: Bool

    init(_ title: String, summary: String, @ViewBuilder content: () -> Content) {
        self.title = title
        self.summary = summary
        self.content = content()
        _open = AppStorage(wrappedValue: false, "inspectorFold." + title)
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Button {
                open.toggle()
            } label: {
                HStack(spacing: 8) {
                    Image(systemName: open ? "arrowtriangle.down.fill" : "arrowtriangle.right.fill")
                        .font(.system(size: 8 * theme.scale))
                        .foregroundStyle(theme.muted)
                        .frame(width: 10 * theme.scale)
                    Text(title)
                        .font(theme.sized(13, .medium))
                        .lineLimit(1)
                        .fixedSize()
                    Spacer(minLength: 0)
                    if !open {
                        Text(summary)
                            .font(theme.sized(12.5))
                            .foregroundStyle(theme.muted)
                            .lineLimit(1)
                            .truncationMode(.tail)
                    }
                }
                .padding(.vertical, 8)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .help(open ? "Folds it" : summary)
            .accessibilityLabel(title)
            .accessibilityValue(open ? "expanded" : summary.isEmpty ? "collapsed" : "collapsed, \(summary.replacingOccurrences(of: " · ", with: ", "))")
            .accessibilityHint(open ? "Folds it" : "Shows it")
            if open {
                VStack(alignment: .leading, spacing: 5) {
                    content
                }
                .padding(.leading, 18)
                .padding(.top, 2)
                .padding(.bottom, 10)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(alignment: .top) { theme.lineSoft.frame(height: 1) }
    }
}

/// Folds one under another, the panel's lower part.
private struct Folds<Content: View>: View {
    @ViewBuilder var content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            content
        }
    }
}

/// What's going on now, in a card: a thread's current step.
private struct NowCard: View {
    @Environment(\.theme) private var theme
    let label: String
    let text: String

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(label)
                .font(theme.monoSized(12))
                .foregroundStyle(theme.muted)
            Text(text)
                .font(theme.sized(14, .medium))
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(.horizontal, 13)
        .padding(.vertical, 11)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(RoundedRectangle(cornerRadius: 6).strokeBorder(theme.line, lineWidth: 1))
        .accessibilityElement(children: .combine)
    }
}

/// Beside the session: a thread's need or current step, and folded its
/// steps and todos, its PR and branch, its agent; a coordinator's or
/// director's needs, and folded its project or program and its agent. A
/// paired server's session shows what that server lets this one see of it
/// (peer.list): its needs or what it does now, and folded its link and
/// switch, its agent.
struct Inspector: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let model: SessionModel
    let session: AppAPI.Session?

    var body: some View {
        let head = head
        InspectorFrame(head: head, context: context, foot: foot) {
            if model.ref.isRemote {
                remote
            } else if model.ref.isCoordinator {
                if let p = app.project(model.ref.project) {
                    NeedCards(needs: app.needs(of: p.slug), settled: app.settledLinks(p.slug))
                    Folds {
                        ProjectFold(project: p)
                        agent(head)
                        todos
                    }
                } else {
                    Folds {
                        agent(head)
                        todos
                    }
                }
            } else if model.ref.director {
                if let g = app.program(model.ref.project) {
                    NeedCards(needs: app.needs(of: g), named: true)
                    Folds {
                        ProgramFold(program: g)
                        agent(head)
                        todos
                    }
                } else {
                    Folds {
                        agent(head)
                        todos
                    }
                }
            } else {
                thread(head)
            }
        }
    }

    // MARK: the header and the footer

    private var head: InspectorHead {
        let ref = model.ref
        if ref.isRemote {
            let s = ref.session(in: app.peers)
            let state = s.flatMap { $0.state.isEmpty ? nil : Attention.session(state: $0.state, reason: $0.reason, asking: $0.asking) }
            if ref.isCoordinator { return .coordinator(project: ref.project, server: ref.server, state: state) }
            return .thread(task: s?.task.isEmpty == false ? s?.task : nil, id: ref.thread, server: ref.server, state: state)
        }
        let own = session.flatMap { $0.state.isEmpty ? nil : Attention.session(state: $0.state, reason: $0.reason) }
        if ref.isCoordinator {
            return .coordinator(project: ref.project, state: app.project(ref.project).map { app.coordinator(of: $0) } ?? own)
        }
        if ref.director {
            return .director(program: ref.project, state: app.program(ref.project).map { Attention.of(director: session, in: $0) } ?? own)
        }
        let t = app.thread(ref)
        return .thread(task: t?.task?.ref, id: ref.thread, state: t.map { app.attention($0) } ?? own)
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
    private func thread(_ head: InspectorHead) -> some View {
        let t = app.thread(model.ref)
        let needs = t.map { Need.of(session: $0.record.session, task: $0.task?.ref ?? "", in: app.needs(of: model.ref.project)) } ?? []
        // A need's card has the title already.
        if needs.isEmpty {
            Text(Need.plain(t?.task?.title ?? t?.record.title ?? model.ref.thread))
                .font(theme.sized(15, .semibold))
                .fixedSize(horizontal: false, vertical: true)
            if let now = now(t) {
                NowCard(label: now.label, text: now.text)
            }
        } else {
            NeedCards(needs: needs)
        }
        Folds {
            if let steps = t?.task?.steps, !steps.isEmpty {
                let current = steps.first { !$0.done }?.n
                InspectorFold("Steps", summary: InspectorSummary.done(steps.filter(\.done).count, of: steps.count)) {
                    ForEach(steps, id: \.n) { s in
                        CheckRow(text: s.text, done: s.done, current: s.n == current)
                    }
                    todoRows(labelled: true)
                }
            } else {
                todos
            }
            if let t {
                InspectorFold("Pull request", summary: pr(t)) {
                    FactList {
                        FactRow("PR", pr(t))
                        if !t.record.branch.isEmpty { FactRow("Branch", t.record.branch, mono: true) }
                    }
                }
            }
            agent(head)
            if let t, let short = t.record.usage.short {
                InspectorFold("Usage", summary: short) {
                    FactList {
                        ForEach(t.record.usage.factRows, id: \.label) { FactRow($0.label, $0.value) }
                    }
                }
            }
        }
    }

    /// What the card says: the current step, or without steps the todo
    /// the agent is on.
    private func now(_ t: AppAPI.Thread?) -> (label: String, text: String)? {
        if let steps = t?.task?.steps, !steps.isEmpty {
            guard let i = steps.firstIndex(where: { !$0.done }) else { return nil }
            return ("Step \(i + 1) of \(steps.count)", steps[i].text)
        }
        let todos = model.transcript.todos
        guard let i = todos.firstIndex(where: { $0.status == "in_progress" }) else { return nil }
        return ("Todo \(i + 1) of \(todos.count)", todos[i].text)
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

    // MARK: a paired server's

    @ViewBuilder
    private var remote: some View {
        let ref = model.ref
        let peer = app.peer(ref.server)
        let s = ref.session(in: app.peers)
        let project = peer?.projects.first { $0.project == ref.project }
        let all = app.needs(of: "\(ref.server)/\(ref.project)")
        let needs = ref.isCoordinator ? all : s.map { Need.of(session: $0.id, task: $0.task, in: all) } ?? []
        if !needs.isEmpty {
            NeedCards(needs: needs)
        } else if !ref.isCoordinator, let s, !s.current.isEmpty {
            NowCard(label: "Now", text: s.current)
        }
        Folds {
            let server = peer.map { $0.name.isEmpty ? $0.alias : "\($0.name) (\($0.alias))" } ?? ref.server
            InspectorFold("Paired server", summary: InspectorSummary.paired(server: server, link: peer?.state ?? "not paired", stale: project?.stale == true)) {
                FactList {
                    FactRow("Server", server)
                    FactRow("Project", ref.project)
                    FactRow("Link", peer?.state ?? "not paired")
                    FactRow("Panes", panes(app.remotePane(ref)))
                    if let far = FarFiles.of(peer, project: ref.project, caps: app.caps) { FactRow("Files", far.fact) }
                    if let peer, app.caps.contains("peer-worktrees") { FactRow("Yours", FarFiles.yours(peer)) }
                    if project?.stale == true { FactRow("Read", "stale") }
                }
                Text("Its conversation stays there: only the pane crosses the link.")
                    .font(theme.sized(12))
                    .foregroundStyle(theme.muted)
                    .fixedSize(horizontal: false, vertical: true)
                if app.caps.contains("peer-worktrees") {
                    Text("A server's own user turns its files on or off, in the Network popup or with tm peer files.")
                        .font(theme.sized(12))
                        .foregroundStyle(theme.muted)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
            if let s {
                InspectorFold("Agent", summary: InspectorSummary.agent(s.agent, model: s.model, queued: s.queuedPrompts)) {
                    FactList {
                        FactRow("Agent", [s.agent, s.model].filter { !$0.isEmpty }.joined(separator: " · "))
                        if !s.current.isEmpty { FactRow("Now", s.current) }
                        if s.todosTotal > 0 { FactRow("Todos", "\(s.todosDone) of \(s.todosTotal)") }
                        if s.queuedPrompts > 0 { FactRow("Queued", InspectorSummary.prompts(s.queuedPrompts)) }
                        FactRow("Started", s.created.formatted(date: .abbreviated, time: .shortened))
                        FactRow("Session", s.id, mono: true)
                    }
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

    /// Its agent and session, and a thread's start and cost, which it has
    /// with or without a session. The session's own state when the header
    /// says something else.
    @ViewBuilder
    private func agent(_ head: InspectorHead) -> some View {
        let t = model.ref.role == nil ? app.thread(model.ref) : nil
        if session != nil || t != nil {
            let summary = session.map { InspectorSummary.agent($0.agent, model: $0.model, queued: $0.queuedPrompts) } ?? "no session"
            InspectorFold("Agent", summary: summary) {
                FactList {
                    if let s = session {
                        FactRow("Agent", [s.agent, s.model].filter { !$0.isEmpty }.joined(separator: " · "))
                        if !s.state.isEmpty {
                            let word = Attention.session(state: s.state, reason: s.reason).word
                            if word != head.state?.word { FactRow("State", word) }
                        }
                        if s.queuedPrompts > 0 { FactRow("Queued", InspectorSummary.prompts(s.queuedPrompts)) }
                        FactRow("Session", s.id, mono: true)
                    }
                    if let t {
                        FactRow("Started", t.record.created.formatted(date: .abbreviated, time: .shortened))
                    }
                }
            }
        }
    }

    // MARK: the todos

    /// The agent's todos in their own fold: a session with no steps.
    @ViewBuilder
    private var todos: some View {
        let list = model.transcript.todos
        if !list.isEmpty {
            InspectorFold("Todos", summary: InspectorSummary.done(list.filter { $0.status == "completed" }.count, of: list.count)) {
                todoRows(labelled: false)
            }
        }
    }

    /// The todos' rows, under their label among a thread's steps.
    @ViewBuilder
    private func todoRows(labelled: Bool) -> some View {
        let list = model.transcript.todos
        if !list.isEmpty {
            if labelled {
                Text("Todos · \(list.filter { $0.status == "completed" }.count) of \(list.count)")
                    .font(theme.sized(12))
                    .foregroundStyle(theme.muted)
                    .accessibilityAddTraits(.isHeader)
                    .padding(.top, 5)
            }
            ForEach(Array(list.enumerated()), id: \.offset) { _, t in
                CheckRow(text: t.text, done: t.status == "completed", current: t.status == "in_progress")
            }
        }
    }
}

/// A project of this server in the panel: its coordinator's state in the
/// header, what needs the user, its counts folded, the context and Save &
/// Clear of its coordinator while it runs.
struct ProjectInspector: View {
    @Environment(AppModel.self) private var app
    let project: AppAPI.Project

    var body: some View {
        let session = app.coordinatorSession(project.slug)
        InspectorFrame(head: .project(project.slug, state: app.coordinator(of: project)),
                       context: ContextUse.of(session, hint: app.contextHint),
                       foot: session != nil && app.controlsProjects ? InspectorFootButton(
                        title: "Save & Clear",
                        help: "Save & Clear: the coordinator saves what it learned, then its conversation is cleared, as tm clear does") {
                            app.ask(.saveClear(project: project.slug))
                        } : nil) {
            NeedCards(needs: app.needs(of: project.slug), settled: app.settledLinks(project.slug))
            Folds { ProjectFold(project: project) }
        }
    }
}

/// A project's counts: a project's panel and its coordinator's.
struct ProjectFold: View {
    @Environment(AppModel.self) private var app
    let project: AppAPI.Project

    var body: some View {
        let open = project.threads.filter { !$0.record.done }.count
        InspectorFold("Project", summary: InspectorSummary.project(threads: open, onDeck: Dashboard.onDeck(project).count)) {
            FactList {
                FactRow("Threads", "\(open) open")
                if project.questions > 0 { FactRow("Questions", "\(project.questions) open") }
                FactRow("On deck", "\(Dashboard.onDeck(project).count)")
                FactRow("Backlog", "\(Dashboard.backlog(project).count)")
                if app.listsDone { FactRow("Done", "\(Dashboard.doneCount(project))") }
                if project.usage.costUsd > 0 { FactRow("Cost", String(format: "$%.2f", project.usage.costUsd)) }
            }
        }
    }
}

/// A paired server's project in the panel: what that server lists of it,
/// its needs answered there where it lets this one.
struct RemoteProjectInspector: View {
    @Environment(\.theme) private var theme
    @Environment(AppModel.self) private var app
    let peer: AppAPI.Peer
    let project: AppAPI.PeerProject

    var body: some View {
        InspectorFrame(head: .project(project.project, server: peer.alias, state: state)) {
            NeedCards(needs: app.needs(of: "\(peer.alias)/\(project.project)"))
            Folds {
                InspectorFold("Project", summary: InspectorSummary.peerProject(threads: project.threads, paused: project.paused, stale: project.stale)) {
                    FactList {
                        FactRow("Threads", "\(project.threads)")
                        if project.paused { FactRow("State", "paused") }
                        if project.stale { FactRow("Read", "stale") }
                    }
                }
                let server = peer.name.isEmpty ? peer.alias : "\(peer.name) (\(peer.alias))"
                InspectorFold("Paired server", summary: InspectorSummary.paired(server: server, link: peer.state)) {
                    FactList {
                        FactRow("Server", server)
                        FactRow("Link", peer.state)
                        if let far = FarFiles.of(peer, project: project.project, caps: app.caps) { FactRow("Files", far.fact) }
                        if app.caps.contains("peer-worktrees") { FactRow("Yours", FarFiles.yours(peer)) }
                    }
                    if app.caps.contains("peer-worktrees") {
                        Text("A server's own user turns its files on or off, in the Network popup or with tm peer files.")
                            .font(theme.sized(12))
                            .foregroundStyle(theme.muted)
                            .fixedSize(horizontal: false, vertical: true)
                    }
                }
            }
        }
    }

    /// What that server's read says of its coordinator: stale or paused
    /// before what its session does.
    private var state: Attention? {
        if project.stale { return Attention(word: "stale", tone: .idle, why: "", waits: false) }
        if project.paused { return Attention(word: "paused", tone: .idle, why: "", waits: false) }
        guard let s = project.sessions.first(where: { $0.role == "coordinator" }), !s.state.isEmpty else { return nil }
        return Attention.session(state: s.state, reason: s.reason, asking: s.asking)
    }
}

/// A program in the panel: its director's state in the header, what needs
/// the user across its projects, its facts folded, its director's context
/// and Save & Clear while it runs.
struct ProgramInspector: View {
    @Environment(AppModel.self) private var app
    let program: AppAPI.Program

    var body: some View {
        let session = app.directorSession(program.slug)
        InspectorFrame(head: .program(program.slug, state: Attention.of(director: session, in: program)),
                       context: ContextUse.of(session, hint: app.contextHint),
                       foot: session != nil && app.controlsProjects ? InspectorFootButton(
                        title: "Save & Clear",
                        help: "Save & Clear: the director saves what it learned, then its conversation is cleared, as tm clear does") {
                            app.ask(.saveClearDirector(program: program.slug))
                        } : nil) {
            NeedCards(needs: app.needs(of: program), named: true)
            Folds { ProgramFold(program: program) }
        }
    }
}

/// A program's facts: a program's panel and its director's.
struct ProgramFold: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let program: AppAPI.Program

    var body: some View {
        InspectorFold("Program", summary: InspectorSummary.program(projects: program.members.count, questions: program.questions)) {
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

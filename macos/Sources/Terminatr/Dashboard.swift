import SwiftUI
import TerminatrKit

/// A project in the window's detail (design T746, T748 milestone C):
/// its goal; what needs the user, as cards that act in place (the
/// project's share of the one needs list, the tasks in review as their
/// own group); its coordinator, with Start when none runs; its threads
/// grouped by state, idle and stopped ones folded; its board, on deck
/// shown and the backlog folded (C2), each task with its action; and
/// the inbox items for the user. An empty project offers a next step.
/// A thread's card shows its thread in the window.
struct ProjectDashboard: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let project: AppAPI.Project
    /// The folded groups the user opened: "idle", "stopped", "backlog".
    @State private var unfolded: Set<String> = []

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                header
                let needs = app.needs(of: project.slug)
                let running = app.coordinatorSession(project.slug) != nil
                if Dashboard.isEmpty(project, needs: needs.count) {
                    NextStepCard(project: project, step: Dashboard.next(project, coordinatorRuns: running))
                }
                if !needs.isEmpty {
                    section("Needs you · \(needs.count)") {
                        ForEach(needs.filter { !$0.isReview }) { NeedCard(need: $0) }
                        let review = needs.filter(\.isReview)
                        if !review.isEmpty {
                            label("Review · \(review.count)")
                            ForEach(review) { NeedCard(need: $0) }
                        }
                    }
                }
                section("Coordinator") {
                    CoordinatorRow(project: project, attention: app.coordinator(of: project), running: running)
                }
                let groups = Dashboard.groups(project.threads, attention: app.attention) { t in
                    !t.record.session.isEmpty && app.sessions.contains { $0.id == t.record.session }
                }
                if !groups.isEmpty {
                    section("Threads · \(project.threads.count)") {
                        ForEach(groups, id: \.group) { g in
                            fold(g.group.rawValue, g.threads.count, key: g.group.folded ? "\(g.group)" : nil) {
                                ForEach(g.threads, id: \.record.id) { threadCard($0) }
                            }
                        }
                    }
                }
                let deck = Dashboard.onDeck(project), backlog = Dashboard.backlog(project)
                if !deck.isEmpty || !backlog.isEmpty {
                    section("Board") {
                        if !deck.isEmpty {
                            fold("On deck", deck.count, key: nil) {
                                ForEach(deck) { BoardRow(project: project.slug, task: $0) }
                            }
                        }
                        if !backlog.isEmpty {
                            fold("Backlog", backlog.count, key: "backlog") {
                                ForEach(backlog) { BoardRow(project: project.slug, task: $0) }
                            }
                        }
                    }
                }
                let inbox = project.items.filter(\.forYou)
                if !inbox.isEmpty {
                    section("Inbox · \(inbox.count) for you") {
                        ForEach(inbox.prefix(Self.inboxShown)) { it in
                            InboxLine(item: it)
                        }
                        if inbox.count > Self.inboxShown {
                            Text("\(inbox.count - Self.inboxShown) more in tm")
                                .font(.system(size: 12))
                                .foregroundStyle(theme.muted)
                        }
                    }
                }
            }
            .padding(.horizontal, 28)
            .padding(.top, 18)
            .padding(.bottom, 28)
            .frame(maxWidth: 760, alignment: .leading)
            .frame(maxWidth: .infinity)
        }
    }

    static let inboxShown = 8

    private func threadCard(_ t: AppAPI.Thread) -> some View {
        let sel = Selection.card(t, in: project.slug)
        return ThreadCard(thread: t, attention: app.attention(t))
            .contentShape(RoundedRectangle(cornerRadius: 14))
            .onTapGesture { app.select(sel) }
            .accessibilityAddTraits(.isButton)
            .accessibilityAction { app.select(sel) }
            .help("Show \(t.task?.ref ?? t.record.id)")
            .contextMenu { NewWindowButton(ref: SessionRef(project: project.slug, thread: t.record.id)) }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline) {
                Text(project.slug)
                    .font(theme.title(30, .heavy))
                Spacer()
            }
            if !project.goal.isEmpty {
                Text(project.goal)
                    .foregroundStyle(theme.muted)
                    .lineLimit(2)
            }
            if let board = board {
                Text(board)
                    .font(theme.mono(11.5))
                    .foregroundStyle(theme.muted)
            }
            if !project.error.isEmpty {
                Text(project.error)
                    .foregroundStyle(theme.attention)
            }
        }
        .padding(.bottom, 4)
    }

    /// "2 need you · 3 in motion · 5 on deck", as the TUI's row; what
    /// needs you is the needs list's count, as the sidebar's badge.
    private var board: String? {
        let c = project.counts
        guard !c.isEmpty else { return nil }
        return "\(app.needs(of: project.slug).count) need you · \(c["in_motion"] ?? 0) in motion · \(c["on_deck"] ?? 0) on deck · \(c["backlog"] ?? 0) in the backlog"
    }

    private func section<C: View>(_ title: String, @ViewBuilder _ content: () -> C) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(title.uppercased())
                .font(theme.mono(10.5, .medium))
                .tracking(1)
                .foregroundStyle(theme.muted)
                .padding(.top, 6)
            content()
        }
    }

    private func label(_ title: String) -> some View {
        Text(title.uppercased())
            .font(theme.mono(10, .medium))
            .tracking(1)
            .foregroundStyle(theme.muted)
            .padding(.top, 2)
    }

    /// A group under its label: with a key, folded until the user opens
    /// it (the key is kept while the dashboard shows).
    @ViewBuilder
    private func fold<C: View>(_ title: String, _ n: Int, key: String?, @ViewBuilder _ content: () -> C) -> some View {
        if let key {
            let open = unfolded.contains(key)
            Button {
                if open { unfolded.remove(key) } else { unfolded.insert(key) }
            } label: {
                HStack(spacing: 5) {
                    Image(systemName: "chevron.right")
                        .rotationEffect(.degrees(open ? 90 : 0))
                        .font(.system(size: 9, weight: .bold))
                    Text("\(title) · \(n)".uppercased())
                        .font(theme.mono(10, .medium))
                        .tracking(1)
                }
                .foregroundStyle(theme.muted)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .padding(.top, 2)
            .accessibilityValue(open ? "open" : "folded")
            if open { content() }
        } else {
            label("\(title) · \(n)")
            content()
        }
    }
}

/// The project's coordinator as the dashboard's row: its state as the
/// dot and tag, what it does or waits on, and Start when none runs
/// (project.open, as tm project open) or Open when it does.
struct CoordinatorRow: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let project: AppAPI.Project
    let attention: Attention
    let running: Bool

    var body: some View {
        let ref = SessionRef(project: project.slug)
        HStack(alignment: .center, spacing: 8) {
            Circle()
                .fill(theme.colour(attention.tone))
                .frame(width: 8, height: 8)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 2) {
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Text("Coordinator")
                        .font(theme.title(15, .bold))
                    Text(attention.word)
                        .font(theme.mono(11))
                        .foregroundStyle(attention.waits ? theme.colour(attention.tone) : theme.muted)
                }
                if !attention.why.isEmpty {
                    Text(attention.why)
                        .font(.system(size: 12.5, weight: attention.waits ? .medium : .regular))
                        .foregroundStyle(attention.waits ? theme.colour(attention.tone) : theme.muted)
                        .lineLimit(2)
                }
            }
            Spacer(minLength: 8)
            if running {
                Button("Open") { app.select(.session(ref)) }
                    .help("Show the coordinator")
            } else {
                StartButton(project: project)
            }
        }
        .padding(14)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 14))
        .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 14))
        .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(theme.edge, lineWidth: 1))
        .contextMenu { NewWindowButton(ref: ref) }
    }
}

/// Starts the project's coordinator (project.open) and shows it; an
/// inactive project is refused, since opening never activates.
struct StartButton: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let project: AppAPI.Project
    var title = "Start"
    @State private var busy = false
    @State private var error: String?

    var body: some View {
        VStack(alignment: .trailing, spacing: 4) {
            Button {
                start()
            } label: {
                if busy {
                    ProgressView().controlSize(.small)
                } else {
                    Text(title)
                }
            }
            .buttonStyle(.borderedProminent)
            .tint(theme.action)
            .disabled(busy || !project.active)
            .help(project.active ? "Starts the coordinator with the project's agent and model, as tm project open does"
                                 : "The project is inactive: tm project active \(project.slug) turns it on")
            if let error {
                Text(error)
                    .font(.system(size: 11.5))
                    .foregroundStyle(theme.attention)
                    .multilineTextAlignment(.trailing)
            }
        }
    }

    private func start() {
        busy = true
        error = nil
        Task {
            do {
                try await app.startCoordinator(project.slug)
            } catch {
                self.error = Dashboard.words(error)
            }
            busy = false
        }
    }
}

/// A task on the board: its id, title and its action (Delegate…).
private struct BoardRow: View {
    @Environment(\.theme) private var theme
    let project: String
    let task: AppAPI.Task

    var body: some View {
        HStack(alignment: .center, spacing: 8) {
            Text(task.ref)
                .font(theme.mono(12, .medium))
                .foregroundStyle(theme.action)
                .frame(minWidth: 44, alignment: .leading)
            Text(task.title)
                .font(.system(size: 13))
                .lineLimit(1)
                .textSelection(.enabled)
            Spacer(minLength: 8)
            TaskActs(project: project, task: task.ref, status: task.status, compact: true)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 6)
        .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 9))
        .overlay(RoundedRectangle(cornerRadius: 9).strokeBorder(theme.edge, lineWidth: 1))
        .accessibilityElement(children: .combine)
    }
}

/// An empty project's next step: turn it on, start its coordinator, or
/// tell the coordinator what to build.
private struct NextStepCard: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let project: AppAPI.Project
    let step: Dashboard.NextStep

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(headline)
                .font(theme.title(18, .heavy))
            Text(detail)
                .foregroundStyle(theme.muted)
                .fixedSize(horizontal: false, vertical: true)
            switch step {
            case .activate:
                Text("tm project active \(project.slug)")
                    .font(theme.mono(12.5))
                    .textSelection(.enabled)
                    .padding(8)
                    .background(theme.command, in: RoundedRectangle(cornerRadius: 8))
            case .start:
                StartButton(project: project, title: "Start the Coordinator")
            case .message:
                Button("Message the Coordinator") {
                    app.select(.session(SessionRef(project: project.slug)))
                    app.focusComposer()
                }
                .buttonStyle(.borderedProminent)
                .tint(theme.action)
            }
        }
        .padding(18)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 14))
        .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 14))
        .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(theme.edge, lineWidth: 1))
    }

    private var headline: String {
        switch step {
        case .activate: "This project is inactive"
        case .start, .message: "Nothing here yet"
        }
    }

    private var detail: String {
        switch step {
        case .activate: "Its coordinator and threads don't run while it is inactive. Turn it on in a terminal:"
        case .start: "Start the coordinator and tell it what to build: it fills the board and starts threads for the tasks."
        case .message: "Tell the coordinator what to build: it fills the board and starts threads for the tasks."
        }
    }
}

/// An item of the project's inbox: a thread's report, a link's message.
private struct InboxLine: View {
    @Environment(\.theme) private var theme
    let item: AppAPI.Item

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Circle()
                .fill(item.needsUser ? theme.attention : theme.idle)
                .frame(width: 6, height: 6)
                .accessibilityHidden(true)
            Text(item.subject.isEmpty ? item.kind : item.subject)
                .font(.system(size: 12.5, weight: .medium))
                .lineLimit(1)
            Text(item.shownSummary)
                .font(.system(size: 12))
                .foregroundStyle(theme.muted)
                .lineLimit(1)
            Spacer(minLength: 6)
            Text(item.created, format: .relative(presentation: .named))
                .font(theme.mono(11))
                .foregroundStyle(theme.muted)
        }
        .accessibilityElement(children: .combine)
    }
}

/// A paired server's shared project in the detail: what peer.list says
/// of it, and its coordinator and threads, each shown in the window.
struct RemoteProjectDashboard: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let peer: AppAPI.Peer
    let project: AppAPI.PeerProject

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                VStack(alignment: .leading, spacing: 4) {
                    HStack(alignment: .firstTextBaseline, spacing: 8) {
                        Text(peer.name.isEmpty ? peer.alias : peer.name)
                            .font(theme.mono(12, .medium))
                            .padding(.horizontal, 7)
                            .padding(.vertical, 1)
                            .background(theme.cardTint, in: Capsule())
                        Text(project.project)
                            .font(theme.title(30, .heavy))
                    }
                    if !project.goal.isEmpty {
                        Text(project.goal)
                            .foregroundStyle(theme.muted)
                            .lineLimit(2)
                    }
                    Text(facts)
                        .font(theme.mono(11.5))
                        .foregroundStyle(theme.muted)
                }
                if project.sessions.isEmpty {
                    Text("Nothing runs there now.")
                        .foregroundStyle(theme.muted)
                }
                ForEach(project.sessions.compactMap { s in SessionRef(s, of: peer.alias).map { (s, $0) } }, id: \.0.id) { s, ref in
                    PeerSessionCard(session: s, ref: ref)
                        .contentShape(RoundedRectangle(cornerRadius: 14))
                        .onTapGesture { app.select(.session(ref)) }
                        .accessibilityAddTraits(.isButton)
                        .accessibilityAction { app.select(.session(ref)) }
                        .contextMenu { NewWindowButton(ref: ref) }
                }
            }
            .padding(.horizontal, 28)
            .padding(.top, 18)
            .padding(.bottom, 28)
            .frame(maxWidth: 760, alignment: .leading)
            .frame(maxWidth: .infinity)
        }
        .opacity(project.stale ? 0.7 : 1)
    }

    private var facts: String {
        var f = ["paired", project.threads == 1 ? "1 thread" : "\(project.threads) threads"]
        if project.needsYou > 0 { f.append("\(project.needsYou) need you") }
        if project.paused { f.append("paused") }
        if project.stale { f.append("stale") }
        if peer.state != "up" { f.append("offline") }
        return f.joined(separator: " · ")
    }
}

private struct PeerSessionCard: View {
    @Environment(\.theme) private var theme
    let session: AppAPI.PeerSession
    let ref: SessionRef

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            if !session.task.isEmpty {
                Text(session.task)
                    .font(theme.mono(12.5, .medium))
                    .foregroundStyle(theme.action)
            }
            Text(ref.isCoordinator ? "Coordinator" : ref.thread)
                .font(theme.title(15, .bold))
            if !session.current.isEmpty {
                Text(session.current)
                    .font(.system(size: 12.5))
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
            }
            Spacer(minLength: 8)
            Text(session.asking ? "asks" : session.state)
                .font(theme.mono(11))
                .foregroundStyle(session.asking || session.state == "blocked" ? theme.approval : theme.muted)
        }
        .padding(14)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 14))
        .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 14))
        .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(theme.edge, lineWidth: 1))
        .accessibilityElement(children: .combine)
    }
}

/// A project's one line in the sidebar: its coordinator's state as the
/// dot, its slug, and what of the needs list is its as the badge.
struct ProjectRow: View {
    @Environment(\.theme) private var theme
    let project: AppAPI.Project
    let dot: Color
    /// The coordinator's state, for VoiceOver and the tooltip.
    let state: String
    let needs: Int
    let selected: Bool

    var body: some View {
        HStack(spacing: 8) {
            Circle()
                .fill(dot)
                .overlay(Circle().strokeBorder(selected ? theme.onAction.opacity(0.8) : .clear, lineWidth: 1))
                .frame(width: 8, height: 8)
                .accessibilityHidden(true)
            Text(project.slug)
                .font(.system(size: 13, weight: .semibold))
                .lineLimit(1)
            Spacer(minLength: 4)
            if needs > 0 {
                Text("\(needs)")
                    .font(theme.mono(11, .medium))
                    .padding(.horizontal, 6)
                    .padding(.vertical, 1)
                    .background(theme.attention, in: Capsule())
                    .foregroundStyle(theme.onAttention)
                    .accessibilityLabel("\(needs) need you")
            }
        }
        .padding(.vertical, 2)
        .foregroundStyle(selected ? theme.onAction : theme.ink)
        .help("Coordinator: \(state)")
        .accessibilityElement(children: .combine)
        .accessibilityValue("coordinator \(state)")
    }
}

/// A thread on the dashboard: its dot, tag and one line of why come
/// from its attention, so one that waits on you says so.
struct ThreadCard: View {
    @Environment(\.theme) private var theme
    let thread: AppAPI.Thread
    let attention: Attention

    var body: some View {
        let r = thread.record, s = thread.status
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Circle()
                    .fill(theme.colour(attention.tone))
                    .frame(width: 8, height: 8)
                    .accessibilityHidden(true)
                if let task = thread.task {
                    Text(task.ref)
                        .font(theme.mono(12.5, .medium))
                        .foregroundStyle(theme.action)
                }
                Text(r.title)
                    .font(theme.title(15, .bold))
                    .lineLimit(1)
                Spacer(minLength: 8)
                Text(r.id)
                    .font(theme.mono(11.5))
                    .foregroundStyle(theme.muted)
                    .textSelection(.enabled)
            }
            if attention.waits {
                Label(attention.why, systemImage: "exclamationmark.bubble")
                    .font(.system(size: 12.5, weight: .medium))
                    .foregroundStyle(theme.colour(attention.tone))
                    .lineLimit(2)
            } else if !attention.why.isEmpty {
                Text(attention.why)
                    .font(.system(size: 12.5))
                    .foregroundStyle(theme.muted)
                    .lineLimit(2)
            }
            HStack(spacing: 10) {
                if let s {
                    Meter(fraction: Double(s.percent) / 100)
                        .frame(width: 120, height: 5)
                    Text(progress(s))
                        .font(theme.mono(11))
                        .foregroundStyle(theme.muted)
                }
                Spacer(minLength: 0)
                Text(facts)
                    .font(theme.mono(11))
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
            }
        }
        .padding(14)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 14))
        .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 14))
        .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(theme.edge, lineWidth: 1))
        .accessibilityElement(children: .combine)
    }

    private func progress(_ s: AppAPI.ThreadStatus) -> String {
        s.stepsTotal > 0 ? "\(s.percent)% · step \(min(s.stepsDone + 1, s.stepsTotal)) of \(s.stepsTotal)" : "\(s.percent)%"
    }

    private var facts: String {
        var f = [attention.word, thread.record.agent]
        if thread.pr > 0 {
            f.append("PR #\(thread.pr)" + (thread.checks.isEmpty ? "" : " · \(thread.checks)"))
        }
        return f.filter { !$0.isEmpty }.joined(separator: " · ")
    }
}

struct Meter: View {
    @Environment(\.theme) private var theme
    let fraction: Double

    var body: some View {
        GeometryReader { g in
            ZStack(alignment: .leading) {
                Capsule().fill(theme.line)
                Capsule()
                    .fill(LinearGradient(colors: theme.meter, startPoint: .leading, endPoint: .trailing))
                    .frame(width: g.size.width * min(max(fraction, 0), 1))
            }
        }
        .accessibilityHidden(true)
    }
}

/// The bridge's state at the foot of the sidebar.
struct StatusLine: View {
    @Environment(AppModel.self) private var model
    @Environment(\.theme) private var theme

    /// Whether the sidebar shows it: not while connected and all fits.
    static func shows(_ model: AppModel) -> Bool {
        if case .connected = model.phase { return model.compatibility.message != nil }
        return true
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            line
            if case .connected = model.phase, let behind = model.compatibility.message {
                // Ink, with the attention colour on the symbol: as text,
                // it is under 4.5:1 on the light sidebar (Theme.bloom).
                Label {
                    Text(behind)
                        .lineLimit(4)
                        .fixedSize(horizontal: false, vertical: true)
                } icon: {
                    Image(systemName: "exclamationmark.triangle.fill")
                        .foregroundStyle(theme.attention)
                }
                .font(.system(size: 10.5))
                .foregroundStyle(theme.ink)
            }
        }
    }

    private var line: some View {
        HStack(spacing: 6) {
            switch model.phase {
            case .connecting:
                ProgressView().controlSize(.mini)
                Text("Connecting…")
            case .connected(let h):
                Circle().fill(theme.good).frame(width: 7, height: 7)
                Text("tm \(h.tm)" + (h.server.map { $0.version == h.tm ? "" : " · server \($0.version)" } ?? ""))
                    .font(theme.mono(11))
            case .failed:
                Circle().fill(theme.attention).frame(width: 7, height: 7)
                Text("Not connected")
                Spacer()
                Button("Retry") { model.connect() }
                    .buttonStyle(.link)
                    .foregroundStyle(theme.action)
            }
        }
        .font(.system(size: 11.5))
        .foregroundStyle(theme.muted)
    }
}

/// The detail pane with no project to show: connecting, failed (with
/// why), or a server with none.
struct Placeholder: View {
    @Environment(AppModel.self) private var model
    @Environment(\.theme) private var theme
    let phase: AppModel.Phase
    let empty: Bool

    var body: some View {
        VStack(spacing: 12) {
            switch phase {
            case .connecting:
                ProgressView()
                Text("Starting tm app bridge…").foregroundStyle(theme.muted)
            case .failed(let why):
                Text("Can't reach terminatr")
                    .font(theme.title(22, .heavy))
                Text(why)
                    .multilineTextAlignment(.center)
                    .foregroundStyle(theme.muted)
                    .textSelection(.enabled)
                    .frame(maxWidth: 440)
                HStack {
                    Button("Retry") { model.connect() }
                        .buttonStyle(.borderedProminent)
                        .tint(theme.action)
                    SettingsLink { Text("Settings…") }
                }
            case .connected:
                Text(empty ? "No projects yet" : "Pick a project")
                    .font(theme.title(22, .heavy))
                if empty {
                    Text("Create one with tm project new in a terminal.")
                        .foregroundStyle(theme.muted)
                }
            }
        }
        .padding(28)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 18))
        .padding(40)
    }
}

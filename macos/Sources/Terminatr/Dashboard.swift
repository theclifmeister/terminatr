import SwiftUI
import TerminatrKit

/// A project in the window's detail (design T746): its goal, what
/// needs the user (the project's share of the one needs list: its
/// sessions' questions, approvals and held queues as well as its tasks,
/// with the tasks in review as their own group), its threads as cards,
/// the inbox items for the user and its board's counts. A card shows
/// its thread in the window; the first, the coordinator's, its session.
struct ProjectDashboard: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let project: AppAPI.Project

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                header
                let needs = app.needs(of: project.slug)
                if !needs.isEmpty {
                    section("Needs you · \(needs.count)") {
                        ForEach(needs.filter { !$0.isReview }) { n in
                            NeedCard(need: n) { app.open(project: n.project, session: n.session) }
                        }
                        let review = needs.filter(\.isReview)
                        if !review.isEmpty {
                            Text("REVIEW · \(review.count)")
                                .font(theme.mono(10, .medium))
                                .tracking(1)
                                .foregroundStyle(theme.muted)
                                .padding(.top, 2)
                            ForEach(review) { n in
                                NeedCard(need: n) { app.open(project: n.project, session: n.session) }
                            }
                        }
                    }
                }
                section("Threads") {
                    let coordinator = SessionRef(project: project.slug)
                    CoordinatorCard(attention: app.coordinator(of: project))
                        .contentShape(RoundedRectangle(cornerRadius: 14))
                        .onTapGesture { app.select(.session(coordinator)) }
                        .accessibilityAddTraits(.isButton)
                        .accessibilityAction { app.select(.session(coordinator)) }
                        .help("Show the coordinator")
                        .contextMenu { NewWindowButton(ref: coordinator) }
                    if project.threads.isEmpty {
                        Text("No threads.")
                            .foregroundStyle(theme.muted)
                    }
                    ForEach(project.threads, id: \.record.id) { t in
                        let sel = Selection.card(t, in: project.slug)
                        ThreadCard(thread: t, attention: app.attention(t))
                            .contentShape(RoundedRectangle(cornerRadius: 14))
                            .onTapGesture { app.select(sel) }
                            .accessibilityAddTraits(.isButton)
                            .accessibilityAction { app.select(sel) }
                            .help("Show \(t.task?.ref ?? t.record.id)")
                            .contextMenu { NewWindowButton(ref: SessionRef(project: project.slug, thread: t.record.id)) }
                    }
                }
                let inbox = project.items.filter(\.forYou)
                if !inbox.isEmpty {
                    section("Inbox · \(inbox.count)") {
                        ForEach(inbox.prefix(Self.inboxShown)) { it in
                            InboxLine(item: it)
                        }
                        if inbox.count > Self.inboxShown {
                            Text("\(inbox.count - Self.inboxShown) more in tm")
                                .font(theme.body(12))
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
        return "\(app.needs(of: project.slug).count) need you · \(c["in_motion"] ?? 0) in motion · \(c["on_deck"] ?? 0) on deck"
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
}

/// A need on the dashboard: what it is and its task's title; a click
/// shows what it names.
private struct NeedCard: View {
    @Environment(\.theme) private var theme
    let need: Need
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Image(systemName: NeedRow.symbol(need.kind))
                    .foregroundStyle(NeedRow.colour(need.kind, theme))
                    .frame(width: 16)
                    .accessibilityHidden(true)
                Text(need.what)
                    .font(theme.body(13, .semibold))
                    .lineLimit(1)
                Text(need.detail)
                    .font(theme.body(12.5))
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
                Spacer(minLength: 8)
                Text(NeedRow.verb(need.kind))
                    .font(theme.body(12, .semibold))
                    .foregroundStyle(theme.action)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 8)
            .contentShape(Rectangle())
            .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 10))
            .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 10))
            .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(theme.edge, lineWidth: 1))
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .combine)
        .accessibilityHint(NeedRow.verb(need.kind))
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
                .font(theme.body(12.5, .medium))
                .lineLimit(1)
            Text(item.shownSummary)
                .font(theme.body(12))
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
                    .font(theme.body(12.5))
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
                .font(theme.body(13, .semibold))
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

/// The project's coordinator, the dashboard's first card: its state as
/// the dot and tag, and what it does or waits on.
struct CoordinatorCard: View {
    @Environment(\.theme) private var theme
    let attention: Attention

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Circle()
                    .fill(theme.colour(attention.tone))
                    .frame(width: 8, height: 8)
                    .accessibilityHidden(true)
                Text("Coordinator")
                    .font(theme.title(15, .bold))
                Spacer(minLength: 8)
                Text(attention.word)
                    .font(theme.mono(11))
                    .foregroundStyle(attention.waits ? theme.colour(attention.tone) : theme.muted)
            }
            if attention.waits {
                Label(attention.why, systemImage: "exclamationmark.bubble")
                    .font(theme.body(12.5, .medium))
                    .foregroundStyle(theme.colour(attention.tone))
                    .lineLimit(2)
            } else if !attention.why.isEmpty {
                Text(attention.why)
                    .font(theme.body(12.5))
                    .foregroundStyle(theme.muted)
                    .lineLimit(2)
            }
        }
        .padding(14)
        .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 14))
        .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 14))
        .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(theme.edge, lineWidth: 1))
        .accessibilityElement(children: .combine)
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
                    .font(theme.body(12.5, .medium))
                    .foregroundStyle(theme.colour(attention.tone))
                    .lineLimit(2)
            } else if !attention.why.isEmpty {
                Text(attention.why)
                    .font(theme.body(12.5))
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
                .font(theme.body(10.5))
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
        .font(theme.body(11.5))
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

import SwiftUI
import TerminatrKit

/// Beside the session: a thread's task and steps, its branch, PR and
/// context, the agent's todos; a coordinator's project and what needs
/// the user.
struct Inspector: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let model: SessionModel
    let session: AppAPI.Session?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 16) {
                if model.ref.isCoordinator {
                    coordinator
                } else {
                    thread
                }
                agent
                if !model.transcript.todos.isEmpty {
                    section("Todos") {
                        ForEach(Array(model.transcript.todos.enumerated()), id: \.offset) { _, t in
                            check(t.text, done: t.status == "completed", current: t.status == "in_progress")
                        }
                    }
                }
            }
            .padding(16)
            .padding(.top, 22)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .guardingLinks()
    }

    // MARK: a thread

    @ViewBuilder
    private var thread: some View {
        let t = app.thread(model.ref)
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 6) {
                if let task = t?.task {
                    Text(task.ref).font(theme.mono(12.5, .medium)).foregroundStyle(theme.action)
                }
                Text(model.ref.thread).font(theme.mono(12)).foregroundStyle(theme.muted)
            }
            Text(t?.task?.title ?? t?.record.title ?? model.ref.thread)
                .font(theme.title(16, .heavy))
                .fixedSize(horizontal: false, vertical: true)
            pill
        }
        if let task = t?.task, !task.steps.isEmpty {
            let current = task.steps.first { !$0.done }?.n
            section("Steps · \(task.steps.filter(\.done).count) of \(task.steps.count)") {
                ForEach(task.steps, id: \.n) { s in
                    check(s.text, done: s.done, current: s.n == current)
                }
            }
        }
        if let r = t?.record {
            section("Thread") {
                if !r.branch.isEmpty { fact("Branch", r.branch, mono: true) }
                fact("PR", pr(t!), mono: false)
                if let url = prURL(t!) {
                    Link("Open the PR", destination: url)
                        .font(.system(size: 12))
                        .foregroundStyle(theme.action)
                }
                fact("State", r.state, mono: false)
                fact("Started", r.created.formatted(date: .abbreviated, time: .shortened), mono: false)
                if r.usage.costUsd > 0 { fact("Cost", String(format: "$%.2f", r.usage.costUsd), mono: false) }
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

    // MARK: a coordinator

    @ViewBuilder
    private var coordinator: some View {
        let p = app.project(model.ref.project)
        VStack(alignment: .leading, spacing: 6) {
            Text(model.ref.project).font(theme.title(16, .heavy))
            if let goal = p?.goal, !goal.isEmpty {
                Text(goal).font(.system(size: 12)).foregroundStyle(theme.muted)
            }
            pill
        }
        if let p {
            if !p.needsYou.isEmpty {
                section("Needs you · \(p.needsYou.count)") {
                    ForEach(p.needsYou, id: \.id) { t in
                        HStack(alignment: .firstTextBaseline, spacing: 6) {
                            Text(t.ref).font(theme.mono(11.5, .medium)).foregroundStyle(theme.attention)
                            Text(t.title).font(.system(size: 12)).lineLimit(2)
                        }
                    }
                }
            }
            section("Project") {
                fact("Threads", "\(p.threads.filter { !$0.record.done }.count) open", mono: false)
                if p.questions > 0 { fact("Questions", "\(p.questions) open", mono: false) }
                if p.usage.costUsd > 0 { fact("Cost", String(format: "$%.2f", p.usage.costUsd), mono: false) }
            }
        }
    }

    // MARK: the agent

    @ViewBuilder
    private var agent: some View {
        if let s = session {
            section("Agent") {
                fact("Agent", [s.agent, s.model].filter { !$0.isEmpty }.joined(separator: " · "), mono: false)
                if s.contextWindow > 0 {
                    let pct = Int((Double(s.context) / Double(s.contextWindow) * 100).rounded())
                    HStack(spacing: 8) {
                        Text("Context").foregroundStyle(theme.muted)
                        Meter(fraction: Double(s.context) / Double(s.contextWindow))
                            .frame(height: 5)
                        Text("\(pct)%").font(theme.mono(11.5))
                    }
                    .font(.system(size: 12))
                }
                if s.queuedPrompts > 0 { fact("Queued", "\(s.queuedPrompts) prompts", mono: false) }
                fact("Session", s.id, mono: true)
            }
        }
    }

    // MARK: parts

    @ViewBuilder
    private var pill: some View {
        if let s = session, !s.state.isEmpty {
            let blocked = s.state == "blocked"
            let word = blocked ? (s.reason == "permission" ? "needs approval" : s.reason.isEmpty ? "blocked" : s.reason) : s.state
            Text(word)
                .font(theme.mono(11, .medium))
                .padding(.horizontal, 8)
                .padding(.vertical, 2)
                .background(blocked ? theme.approval : theme.line, in: Capsule())
                .foregroundStyle(blocked ? theme.onAttention : theme.ink)
        }
    }

    private func section<C: View>(_ title: String, @ViewBuilder _ content: () -> C) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(title.uppercased())
                .font(theme.mono(10.5, .medium))
                .tracking(1)
                .foregroundStyle(theme.muted)
            content()
        }
    }

    private func check(_ text: String, done: Bool, current: Bool) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 7) {
            Image(systemName: done ? "checkmark.circle.fill" : current ? "circle.inset.filled" : "circle")
                .foregroundStyle(done ? theme.good : current ? theme.action : theme.idle)
                .font(.system(size: 11))
            Text(text)
                .font(.system(size: 12, weight: current ? .semibold : .regular))
                .foregroundStyle(done ? theme.muted : theme.ink)
                .fixedSize(horizontal: false, vertical: true)
        }
        .accessibilityElement(children: .combine)
        .accessibilityValue(done ? "done" : current ? "current" : "")
    }

    private func fact(_ label: String, _ value: String, mono: Bool) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(label).foregroundStyle(theme.muted)
            Text(value)
                .font(mono ? theme.mono(11.5) : .system(size: 12))
                .lineLimit(2)
                .truncationMode(.middle)
                .textSelection(.enabled)
        }
        .font(.system(size: 12))
    }
}

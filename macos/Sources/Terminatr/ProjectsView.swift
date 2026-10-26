import SwiftUI
import TerminatrKit

/// The projects window: every project in a frosted sidebar, the
/// selected one's threads as cards, all over the theme's backdrop. A
/// card opens its thread's window, the header the coordinator's.
struct ProjectsView: View {
    @Environment(AppModel.self) private var model
    @Environment(\.theme) private var theme
    @Environment(\.openWindow) private var openWindow
    @State private var selected: String?

    var body: some View {
        HStack(spacing: 0) {
            sidebar
                .frame(width: 236)
                .background(.ultraThinMaterial)
                .background(theme.sidebarTint)
                .overlay(alignment: .trailing) { theme.line.frame(width: 1) }
            detail
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .foregroundStyle(theme.ink)
        .background { Backdrop() }
        .onChange(of: model.projects.map(\.slug), initial: true) { _, slugs in
            if selected.map({ !slugs.contains($0) }) ?? true {
                selected = slugs.first
            }
        }
    }

    private var project: AppAPI.Project? {
        model.projects.first { $0.slug == selected }
    }

    // MARK: sidebar

    private var sidebar: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text("terminatr")
                .font(theme.title(20, .heavy))
                .padding(.horizontal, 16)
                .padding(.top, 38) // under the traffic lights
                .padding(.bottom, 10)
            Text("PROJECTS")
                .font(theme.mono(10.5, .medium))
                .tracking(1)
                .foregroundStyle(theme.muted)
                .padding(.horizontal, 16)
                .padding(.bottom, 4)
            ScrollView {
                LazyVStack(spacing: 2) {
                    ForEach(model.projects, id: \.slug) { p in
                        ProjectRow(project: p, selected: p.slug == selected)
                            .contentShape(Rectangle())
                            .onTapGesture { selected = p.slug }
                            .accessibilityAddTraits(p.slug == selected ? [.isButton, .isSelected] : .isButton)
                    }
                }
                .padding(.horizontal, 8)
            }
            Spacer(minLength: 0)
            StatusLine()
                .padding(12)
        }
    }

    // MARK: detail

    @ViewBuilder
    private var detail: some View {
        if let project {
            ScrollView {
                VStack(alignment: .leading, spacing: 14) {
                    header(project)
                    if project.threads.isEmpty {
                        Text("No threads.")
                            .foregroundStyle(theme.muted)
                    }
                    ForEach(project.threads, id: \.record.id) { t in
                        ThreadCard(thread: t)
                            .contentShape(RoundedRectangle(cornerRadius: 14))
                            .onTapGesture { openWindow(id: "session", value: SessionRef(project: project.slug, thread: t.record.id)) }
                            .accessibilityAddTraits(.isButton)
                            .help("Open \(t.task?.ref ?? t.record.id)'s window")
                    }
                }
                .padding(.horizontal, 28)
                .padding(.top, 34)
                .padding(.bottom, 28)
                .frame(maxWidth: 760, alignment: .leading)
            }
        } else {
            Placeholder(phase: model.phase, empty: model.projects.isEmpty)
        }
    }

    private func header(_ p: AppAPI.Project) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline) {
                Text(p.slug)
                    .font(theme.title(30, .heavy))
                Spacer()
                Button("Coordinator") { openWindow(id: "session", value: SessionRef(project: p.slug)) }
                    .buttonStyle(.borderedProminent)
                    .tint(theme.action)
                    .help("Open the coordinator's window")
            }
            if !p.goal.isEmpty {
                Text(p.goal)
                    .foregroundStyle(theme.muted)
                    .lineLimit(2)
            }
            if !p.error.isEmpty {
                Text(p.error)
                    .foregroundStyle(theme.attention)
            }
        }
        .padding(.bottom, 4)
    }
}

struct ProjectRow: View {
    @Environment(\.theme) private var theme
    let project: AppAPI.Project
    let selected: Bool

    var body: some View {
        HStack(spacing: 8) {
            VStack(alignment: .leading, spacing: 1) {
                Text(project.slug)
                    .font(.system(size: 13, weight: .semibold))
                    .lineLimit(1)
                Text(summary)
                    .font(.system(size: 11.5))
                    .foregroundStyle(selected ? theme.onAction.opacity(0.85) : theme.muted)
                    .lineLimit(1)
            }
            Spacer(minLength: 4)
            if !project.needsYou.isEmpty {
                Text("\(project.needsYou.count)")
                    .font(theme.mono(11, .medium))
                    .padding(.horizontal, 6)
                    .padding(.vertical, 1)
                    .background(theme.attention, in: Capsule())
                    .foregroundStyle(theme.onAttention)
                    .accessibilityLabel("\(project.needsYou.count) need you")
            }
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 6)
        .foregroundStyle(selected ? theme.onAction : theme.ink)
        .background {
            if selected {
                RoundedRectangle(cornerRadius: 9).fill(theme.action)
                    .shadow(color: theme.action.opacity(0.5), radius: 7, y: 4)
            }
        }
    }

    private var summary: String {
        let open = project.threads.filter { !$0.record.done }.count
        return open == 1 ? "1 thread" : "\(open) threads"
    }
}

struct ThreadCard: View {
    @Environment(\.theme) private var theme
    let thread: AppAPI.Thread

    var body: some View {
        let r = thread.record, s = thread.status
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Circle()
                    .fill(dot)
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
            if let needs = s?.needsYou, !needs.isEmpty {
                Label(needs, systemImage: "exclamationmark.bubble")
                    .font(.system(size: 12.5, weight: .medium))
                    .foregroundStyle(theme.attention)
                    .lineLimit(2)
            } else if let line = s.map({ $0.current.isEmpty ? $0.activity : $0.current }), !line.isEmpty {
                Text(line)
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

    private var dot: Color {
        if let n = thread.status?.needsYou, !n.isEmpty { return theme.attention }
        if thread.record.done { return theme.good }
        return thread.record.session.isEmpty ? theme.idle : theme.question
    }

    private func progress(_ s: AppAPI.ThreadStatus) -> String {
        s.stepsTotal > 0 ? "\(s.percent)% · step \(min(s.stepsDone + 1, s.stepsTotal)) of \(s.stepsTotal)" : "\(s.percent)%"
    }

    private var facts: String {
        var f = [thread.record.state, thread.record.agent]
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

    var body: some View {
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

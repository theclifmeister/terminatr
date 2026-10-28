import SwiftUI
import TerminatrKit

/// A task's detail as the Mac's task view and the phone's task screen
/// show it, under its header: what it is blocked on, for a task in
/// review where its PR stands and how to check it, its steps and its
/// notes. missing stands in for a partial task's notes and steps while
/// task.get reads them, or says why it didn't. Read only.
public struct TaskDetailView<Missing: View>: View {
    @Environment(\.theme) private var theme
    let detail: TaskDetail
    let missing: Missing

    public init(_ detail: TaskDetail, @ViewBuilder missing: () -> Missing) {
        self.detail = detail
        self.missing = missing()
    }

    public var body: some View {
        let d = detail
        VStack(alignment: .leading, spacing: 18) {
            if let on = d.blockedOn {
                callout(on.isEmpty ? "No note says. Ask the coordinator in its conversation." : on,
                        label: "Blocked on", symbol: "hand.raised.fill", colour: theme.attention, faint: on.isEmpty)
            }
            if let ship = d.ship {
                shipRow(ship, url: d.prURL)
            }
            if let checks = d.checks {
                section("How to check") {
                    if checks.isEmpty {
                        Text("The report doesn't say. Ask the coordinator in its conversation.")
                            .foregroundStyle(theme.muted)
                    } else {
                        ForEach(Array(checks.enumerated()), id: \.offset) { _, c in
                            HStack(alignment: .firstTextBaseline, spacing: 8) {
                                Text("•").foregroundStyle(theme.muted)
                                MarkdownView(text: c)
                            }
                        }
                    }
                }
            }
            if !d.task.steps.isEmpty {
                section("Steps · \(d.stepsDone)") {
                    ForEach(d.task.steps, id: \.n) { s in
                        HStack(alignment: .firstTextBaseline, spacing: 8) {
                            Image(systemName: s.done ? "checkmark.circle.fill" : "circle")
                                .foregroundStyle(s.done ? theme.good : theme.muted)
                                .accessibilityLabel(s.done ? "done" : "to do")
                            Text("\(s.n). \(s.text)")
                                .foregroundStyle(s.done ? theme.muted : theme.ink)
                                .fixedSize(horizontal: false, vertical: true)
                                .textSelection(.enabled)
                        }
                        .accessibilityElement(children: .combine)
                    }
                }
            }
            if !d.notes.isEmpty {
                section("Notes") { MarkdownView(text: d.notes) }
            } else if d.partial {
                missing
            }
        }
        .font(theme.body(.body))
    }

    private func shipRow(_ ship: TaskDetail.Ship, url: URL?) -> some View {
        let (symbol, colour): (String, Color) = switch ship.state {
        case .merged: ("checkmark.circle.fill", theme.good)
        case .open: ("arrow.triangle.pull", theme.action)
        case .closed: ("xmark.circle.fill", theme.attention)
        case .unknown: ("arrow.triangle.pull", theme.muted)
        }
        return HStack(alignment: .firstTextBaseline, spacing: 8) {
            Image(systemName: symbol)
                .foregroundStyle(colour)
                .accessibilityHidden(true)
            if let url {
                Link(ship.text, destination: url)
                    .help(url.absoluteString)
            } else {
                Text(ship.text)
            }
        }
        .font(theme.body(.body, .medium))
        .foregroundStyle(ship.state == .closed ? theme.attention : theme.ink)
    }

    private func callout(_ text: String, label: String, symbol: String, colour: Color, faint: Bool) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Image(systemName: symbol)
                .foregroundStyle(colour)
                .accessibilityHidden(true)
            (Text("\(label): ").bold() + Text(text))
                .foregroundStyle(faint ? theme.muted : theme.ink)
                .fixedSize(horizontal: false, vertical: true)
                .textSelection(.enabled)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 9)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(colour.opacity(0.12), in: RoundedRectangle(cornerRadius: 8))
        .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(colour.opacity(theme.contrast ? 1 : 0.3), lineWidth: 1))
        .accessibilityElement(children: .combine)
    }

    private func section<C: View>(_ title: String, @ViewBuilder _ content: () -> C) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            Text(title.uppercased())
                .font(theme.mono(.caption2, .medium))
                .tracking(1)
                .foregroundStyle(theme.muted)
            content()
        }
    }
}

extension TaskDetail {
    /// A status as the TUI says it: "on deck", "in motion", "backlog".
    public static func words(_ status: String) -> String {
        switch status {
        case "ready": "on deck"
        case "started": "in motion"
        case "open": "backlog"
        default: status
        }
    }

    /// "web · thread t-0713 · owner me · updated 2026-10-27".
    public var facts: String {
        var f = [project]
        if !task.thread.isEmpty { f.append("thread \(task.thread)") }
        if !task.owner.isEmpty { f.append("owner \(task.owner)") }
        if !task.updated.isEmpty { f.append("updated \(task.updated)") }
        return f.joined(separator: " · ")
    }

    /// The PR's link: the report's PR line, a web link only.
    var prURL: URL? {
        guard let s = thread?.reportPr, let u = URL(string: s), SafeMarkdown.allowed(u) else { return nil }
        return u
    }
}

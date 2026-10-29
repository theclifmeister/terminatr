import SwiftUI
import TerminatrKit

/// A run of tool calls as a chip in the conversation (T848, direction
/// A): up to three of its tools' symbols overlapped, its calls counted
/// by tool ("Read ×1 · Bash ×1") and its state, a check and "done", a
/// spinner, or a mark and "failed", on a glass capsule; a chevron turns
/// while it is open. The capsule hugs its words; its tap target is 44
/// pt tall. The phone's conversation taps it open (the Mac's keeps
/// ToolRunHeader).
public struct RunChip: View {
    @Environment(\.theme) private var theme
    let run: Transcript.ToolRun
    let open: Bool

    public init(run: Transcript.ToolRun, open: Bool) {
        self.run = run
        self.open = open
    }

    public var body: some View {
        HStack(spacing: Spacing.item) {
            HStack(spacing: -6) {
                ForEach(Array(run.counts.prefix(3).enumerated()), id: \.offset) { _, c in
                    Image(systemName: Self.symbol(c.tool))
                        .font(.system(size: 10 * theme.scale, weight: .semibold))
                        .foregroundStyle(theme.action)
                        .frame(width: 22 * theme.scale, height: 22 * theme.scale)
                        .background(Circle().fill(theme.glass))
                        .background(Circle().fill(theme.wash(.working)))
                        .overlay(Circle().strokeBorder(theme.edge, lineWidth: 1))
                }
            }
            Text(run.headline)
                .font(theme.mono(.footnote, .medium))
                .foregroundStyle(theme.ink)
                .lineLimit(1)
                .truncationMode(.tail)
            state
                .font(theme.mono(.footnote))
                .fixedSize()
            Image(systemName: "chevron.right")
                .font(theme.body(.caption2, .bold))
                .foregroundStyle(theme.muted)
                .rotationEffect(.degrees(open ? 90 : 0))
        }
        .padding(.leading, 5)
        .padding(.trailing, 12)
        .padding(.vertical, 4)
        .surface(.glass, in: Capsule())
        .frame(minHeight: tapTarget)
        .contentShape(Rectangle())
    }

    @ViewBuilder private var state: some View {
        switch run.state {
        case .running:
            ProgressView().controlSize(.mini)
        case .failed:
            Label("failed", systemImage: "exclamationmark.triangle.fill")
                .labelStyle(Tight())
                .foregroundStyle(theme.attention)
        case .done:
            Label("done", systemImage: "checkmark")
                .labelStyle(Tight())
                .foregroundStyle(theme.muted)
        }
    }

    /// A tool's symbol: what it does, else a wrench.
    public static func symbol(_ tool: String) -> String {
        switch tool {
        case "Read", "NotebookRead": "doc.text"
        case "Write", "Edit", "MultiEdit", "NotebookEdit": "pencil"
        case "Bash", "BashOutput", "KillShell": "terminal"
        case "Grep", "Glob", "LS": "magnifyingglass"
        case "WebFetch", "WebSearch": "globe"
        case "Task", "Agent": "person.2"
        case "TodoWrite": "checklist"
        default: "wrench.adjustable"
        }
    }
}

/// A label's symbol close before its words.
private struct Tight: LabelStyle {
    func makeBody(configuration: Configuration) -> some View {
        HStack(spacing: 3) {
            configuration.icon.imageScale(.small)
            configuration.title
        }
    }
}

import SwiftUI
import TerminatrKit

/// A run of tool calls in the conversation (T848, T1001), the design
/// page's flat tool row: a chevron turned while it is open, its calls
/// counted by tool ("Read ×1 · Bash ×1") in mono, and while it isn't done its state, a spinner, or a mark and "failed". It hugs
/// its words; 44 pt to tap. The phone's conversation taps it open (the Mac's keeps
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
            FlatIcon(.tool, side: 13)
                .foregroundStyle(theme.muted)
                .rotationEffect(.degrees(open ? 90 : 0))
            Text(run.headline)
                .font(theme.monoSized(12.5))
                .foregroundStyle(theme.muted)
                .lineLimit(1)
                .truncationMode(.tail)
                .frame(minHeight: 18.75 * theme.scale)
            state
                .font(theme.monoSized(12.5))
                .fixedSize()
        }
        .padding(.vertical, 12)
        .contentShape(Rectangle())
        .padding(.vertical, -12)
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
            EmptyView()
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

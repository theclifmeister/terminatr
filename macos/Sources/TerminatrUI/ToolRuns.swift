import SwiftUI
import TerminatrKit

/// A tool-run group's header, one flat line: its calls counted by tool
/// ("Read ×5 · Bash ×3") with their state, and a chevron turned while
/// open. The
/// Mac app's ToolRunView folds it; the phone's shows it as it is.
public struct ToolRunHeader: View {
    @Environment(\.theme) private var theme
    let run: Transcript.ToolRun
    let open: Bool

    public init(run: Transcript.ToolRun, open: Bool) {
        self.run = run
        self.open = open
    }

    public var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Image(systemName: "chevron.right")
                .font(theme.body(.glyph, .semibold))
                .foregroundStyle(theme.muted)
                .rotationEffect(.degrees(open ? 90 : 0))
            Text(run.headline)
                .font(theme.mono(.subheadline))
                .foregroundStyle(theme.muted)
                .lineLimit(1)
                .truncationMode(.tail)
            switch run.state {
            case .running:
                WorkingSpinner()
            case .failed:
                Text("· failed")
                    .font(theme.mono(.footnote))
                    .foregroundStyle(theme.attention)
            case .done:
                Text("· done")
                    .font(theme.mono(.footnote))
                    .foregroundStyle(theme.muted)
            }
            Spacer(minLength: 0)
        }
        .contentShape(Rectangle())
    }

    /// The run's state in words, for VoiceOver.
    public static func stateWord(_ run: Transcript.ToolRun) -> String {
        switch run.state {
        case .running: "running"
        case .failed: "failed"
        case .done: "done"
        }
    }
}

/// A tool call as one flat muted line in mono: the tool, its main argument and its
/// result's first line. An AskUserQuestion call without its answer
/// waits on the user, not on a spinner: it says so, with a link that
/// shows the question card; once answered or declined, it is its
/// AnsweredQuestionCard.
public struct ToolLine: View {
    @Environment(\.theme) private var theme
    let entry: Transcript.Entry
    var answer: (() -> Void)?

    public init(entry: Transcript.Entry, answer: (() -> Void)? = nil) {
        self.entry = entry
        self.answer = answer
    }

    public var body: some View {
        if let outcome = entry.questionOutcome {
            AnsweredQuestionCard(outcome: outcome)
        } else {
            #if os(iOS)
            phoneLine
            #else
            line
            #endif
        }
    }

    #if os(iOS)
    /// The phone's, the design page's `.tool` (T1008): a shell command's
    /// words alone, another tool's after its name; a failure and the spinner
    /// after them, a result that went well says nothing.
    private var phoneLine: some View {
        HStack(spacing: Spacing.item) {
            FlatIcon(.tool, side: 13)
                .foregroundStyle(theme.muted)
            Text(entry.tool == "Bash" ? entry.text : "\(entry.tool.isEmpty ? "tool" : entry.tool) \(entry.text)")
                .font(theme.monoSized(12.5))
                .foregroundStyle(theme.muted)
                .lineLimit(1)
                .truncationMode(.middle)
                .textSelection(.enabled)
                .frame(minHeight: 18.75 * theme.scale)
            if let r = entry.result {
                if r.error {
                    Text(summary(r))
                        .font(theme.monoSized(12.5))
                        .foregroundStyle(theme.attention)
                        .lineLimit(1)
                }
            } else if entry.asksUser {
                Text("· Waiting for your answer")
                    .font(theme.monoSized(12.5))
                    .foregroundStyle(theme.question)
                    .lineLimit(1)
            } else {
                WorkingSpinner()
            }
            Spacer(minLength: 0)
        }
        .accessibilityElement(children: .combine)
    }
    #endif

    private var line: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Image(systemName: "chevron.right")
                .font(theme.body(.glyph, .semibold))
                .foregroundStyle(theme.muted)
                .accessibilityHidden(true)
            // The tool's name keeps its line on a phone's width: the
            // argument truncates first.
            Text(entry.tool.isEmpty ? "tool" : entry.tool)
                .font(theme.mono(.subheadline))
                .foregroundStyle(theme.muted)
                .lineLimit(1)
                .layoutPriority(1)
            Text(entry.text)
                .font(theme.mono(.subheadline))
                .foregroundStyle(theme.muted)
                .lineLimit(1)
                .truncationMode(.middle)
                .textSelection(.enabled)
            if let r = entry.result {
                Text(summary(r))
                    .font(theme.mono(.footnote))
                    .foregroundStyle(r.error ? theme.attention : theme.muted)
                    .lineLimit(1)
            } else if entry.asksUser {
                Text("· Waiting for your answer")
                    .font(theme.mono(.footnote))
                    .foregroundStyle(theme.question)
                    .lineLimit(1)
                if let answer {
                    Button("Answer", action: answer)
                        #if os(macOS)
                        .buttonStyle(.link)
                        #else
                        .buttonStyle(.borderless)
                        #endif
                        .font(theme.body(.footnote, .semibold))
                        .foregroundStyle(theme.action)
                        .help("Shows the question card")
                }
            } else {
                WorkingSpinner()
            }
            Spacer(minLength: 0)
        }
        .accessibilityElement(children: .combine)
    }

    private func summary(_ r: Transcript.Result) -> String {
        var parts: [String] = []
        if r.error { parts.append("error") }
        if !r.text.isEmpty { parts.append(r.text) }
        if r.lines > 1 { parts.append("\(r.lines) lines") }
        return "· " + (parts.isEmpty ? "done" : parts.joined(separator: " · "))
    }
}

/// The spinner of a running tool row. It takes the place of the
/// finished state's "· done" text — same font, so the same line height
/// and baseline — and centres on that line, so the row keeps its height
/// and the spinner its spot when the call finishes. A bare ProgressView
/// in a baseline-aligned row would hang from the baseline, below the
/// label.
struct WorkingSpinner: View {
    @Environment(\.theme) private var theme

    var body: some View {
        Text("· done")
            .font(theme.mono(.footnote))
            .hidden()
            .overlay(alignment: .leading) {
                ProgressView().controlSize(.mini)
            }
    }
}

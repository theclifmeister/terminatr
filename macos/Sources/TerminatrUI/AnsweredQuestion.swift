import SwiftUI
import TerminatrKit

/// A question menu once it ended (Entry.questionOutcome), in place of
/// its tool line: "Answered", then each question (its header over its
/// text) with what the user picked, a check by each label, their own
/// words in quotes, their notes faint; or "Declined" with the result's
/// line. The Mac app and the phone draw it alike.
public struct AnsweredQuestionCard: View {
    @Environment(\.theme) private var theme
    let outcome: Transcript.Entry.QuestionOutcome

    public init(outcome: Transcript.Entry.QuestionOutcome) {
        self.outcome = outcome
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            switch outcome {
            case .answered(let answers):
                stamp("Answered", symbol: "checkmark.circle.fill", colour: theme.good)
                ForEach(Array(answers.enumerated()), id: \.offset) { _, a in
                    question(a)
                }
            case .declined(let line):
                stamp("Declined", symbol: "xmark.circle", colour: theme.muted)
                if !line.isEmpty {
                    Text(line)
                        .font(theme.body(.footnote))
                        .foregroundStyle(theme.muted)
                        .lineLimit(2)
                }
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 9)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(theme.command, in: RoundedRectangle(cornerRadius: 9))
        .overlay(alignment: .leading) {
            // The answered look: a rule in the colour of done.
            if case .answered = outcome {
                UnevenRoundedRectangle(topLeadingRadius: 9, bottomLeadingRadius: 9)
                    .fill(theme.good)
                    .frame(width: 3)
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(spoken)
    }

    private func stamp(_ word: String, symbol: String, colour: Color) -> some View {
        Label {
            Text(word).font(theme.mono(.footnote, .medium))
        } icon: {
            Image(systemName: symbol).font(theme.body(.footnote, .semibold))
        }
        .foregroundStyle(colour)
    }

    private func question(_ a: AppAPI.TranscriptAnswer) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            if !a.header.isEmpty {
                Text(a.header.uppercased())
                    .font(theme.mono(.caption2, .medium))
                    .foregroundStyle(theme.muted)
            }
            Text(a.question)
                .font(theme.body(.subheadline))
                .foregroundStyle(theme.ink)
                .lineLimit(4)
                .textSelection(.enabled)
            ForEach(Array(a.picked.enumerated()), id: \.offset) { _, label in
                pick(Text(label), symbol: "checkmark")
            }
            if !a.typed.isEmpty {
                pick(Text("\u{201C}\(a.typed)\u{201D}").italic(), symbol: "text.cursor")
            }
            if !a.notes.isEmpty {
                Text(a.notes)
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.muted)
                    .lineLimit(3)
            }
        }
    }

    private func pick(_ text: Text, symbol: String) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            Image(systemName: symbol)
                .font(theme.body(.caption, .bold))
                .foregroundStyle(theme.good)
            text
                .font(theme.body(.subheadline, .semibold))
                .foregroundStyle(theme.ink)
                .textSelection(.enabled)
        }
    }

    private var spoken: String {
        switch outcome {
        case .answered(let answers): "Question answered. " + answers.map(\.spoken).joined(separator: " ")
        case .declined: "Question declined."
        }
    }
}

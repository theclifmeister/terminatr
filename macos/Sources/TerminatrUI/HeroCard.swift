import SwiftUI
import TerminatrKit

/// What a session waits on, in its conversation (T848), as the design
/// page's `.ask` (T1008): a 6 round line round the question (or what an
/// approval asks), an approval's command in mono under it, and its answers; over the question the
/// tool and how long it has waited, as the page's `.who` (the user's own):
/// numbered AnswerOption rows, or small buttons (compactAnswers); a line of
/// what the server said under them. VoiceOver calls it word ("Approval").
/// Its answers never take a keyboard's bare keys: the screen has a text field.
public struct HeroCard<Answers: View>: View {
    @Environment(\.theme) private var theme
    let word: String
    let line: String
    let title: String
    let command: String
    let note: (text: String, failed: Bool)?
    let answers: Answers

    public init(word: String, line: String = "", title: String, command: String = "", note: (text: String, failed: Bool)? = nil,
                @ViewBuilder answers: () -> Answers) {
        self.word = word
        self.line = line
        self.title = title
        self.command = command
        self.note = note
        self.answers = answers()
    }

    public var body: some View {
        let shape = RoundedRectangle(cornerRadius: Radius.row)
        VStack(alignment: .leading, spacing: 9) {
            VStack(alignment: .leading, spacing: 2) {
                if !line.isEmpty {
                    Text(line)
                        .font(theme.sized(12))
                        .foregroundStyle(theme.muted)
                        .lines(1)
                        .pageLeading(12)
                }
                if !title.isEmpty {
                    Text(title)
                        .font(theme.sized(Flat.text))
                        .foregroundStyle(theme.ink)
                        .pageLeading(Flat.text)
                        .fixedSize(horizontal: false, vertical: true)
                        .textSelection(.enabled)
                }
            }
            if !command.isEmpty {
                Text(command)
                    .font(theme.monoSized(12))
                    .foregroundStyle(theme.muted)
                    .lines(6)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            answers
                .environment(\.answerKeys, false)
            if let note {
                // As a deck card's: a refusal in ink after the mark.
                HStack(alignment: .firstTextBaseline, spacing: Spacing.tight) {
                    if note.failed {
                        Image(systemName: "exclamationmark.triangle.fill")
                            .foregroundStyle(theme.attention)
                            .accessibilityHidden(true)
                    }
                    Text(note.text)
                        .foregroundStyle(note.failed ? theme.ink : theme.muted)
                        .fixedSize(horizontal: false, vertical: true)
                }
                .font(theme.sized(12.5))
            }
        }
        .padding(.horizontal, 13)
        .padding(.vertical, 11)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay { shape.strokeBorder(theme.line, lineWidth: theme.contrast ? 1.5 : 1) }
        // VoiceOver: a group named for what it is, so its answers are
        // heard as the question's or the approval's.
        .accessibilityElement(children: .contain)
        .accessibilityLabel(word)
    }
}

extension HeroCard where Answers == EmptyView {
    public init(word: String, line: String = "", title: String, command: String = "", note: (text: String, failed: Bool)? = nil) {
        self.init(word: word, line: line, title: title, command: command, note: note) { EmptyView() }
    }
}

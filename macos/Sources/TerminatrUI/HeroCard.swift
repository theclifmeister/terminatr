import SwiftUI
import TerminatrKit

/// What a session waits on, as the hero of its conversation (T848,
/// direction A): a lifted deck card ringed in the tone, its StateGlyph,
/// its word and tag ("Asks · Ranking"), the tool and how long it has
/// waited in mono ("AskUserQuestion · waiting 6m"), then the question
/// (or what an approval asks) as its title, an approval's command as a
/// block, and its answers: numbered AnswerOption rows, or the tone's
/// buttons; a line of what the server said under them. While it waits
/// its glow breathes (Glow, waiting). Its answers never take a
/// keyboard's bare keys: the screen has a text field.
public struct HeroCard<Answers: View>: View {
    @Environment(\.theme) private var theme
    let tone: Attention.Tone
    let symbol: String?
    let word: String
    let line: String
    let title: String
    let command: String
    let note: (text: String, failed: Bool)?
    let waiting: String?
    let answers: Answers

    public init(tone: Attention.Tone, symbol: String? = nil, word: String, line: String = "", title: String,
                command: String = "", note: (text: String, failed: Bool)? = nil, waiting: String? = nil,
                @ViewBuilder answers: () -> Answers) {
        self.tone = tone
        self.symbol = symbol
        self.word = word
        self.line = line
        self.title = title
        self.command = command
        self.note = note
        self.waiting = waiting
        self.answers = answers()
    }

    public var body: some View {
        let shape = RoundedRectangle(cornerRadius: Radius.card, style: .continuous)
        VStack(alignment: .leading, spacing: 11) {
            HStack(spacing: 11) {
                StateGlyph(tone, symbol: symbol)
                    .symbolVariant(.fill)
                VStack(alignment: .leading, spacing: 2) {
                    Text(word)
                        .font(theme.body(.headline, .bold))
                        .foregroundStyle(theme.ink)
                        .lines(1)
                    if !line.isEmpty {
                        Text(line)
                            .font(theme.mono(.footnote, .medium))
                            .foregroundStyle(theme.muted)
                            .lines(1)
                    }
                }
                Spacer(minLength: 0)
            }
            .accessibilityElement(children: .combine)
            if !title.isEmpty {
                Text(title)
                    .font(theme.title(.title3, .bold))
                    .foregroundStyle(theme.ink)
                    .fixedSize(horizontal: false, vertical: true)
                    .textSelection(.enabled)
            }
            if !command.isEmpty {
                Text(command)
                    .font(theme.mono(.callout))
                    .foregroundStyle(theme.ink)
                    .lines(6)
                    .padding(.horizontal, Spacing.row)
                    .padding(.vertical, Spacing.item)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(theme.command, in: RoundedRectangle(cornerRadius: Radius.row - 4, style: .continuous))
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
                .font(theme.body(.footnote))
            }
        }
        .padding(Spacing.card + 2)
        .frame(maxWidth: .infinity, alignment: .leading)
        .surface(.raised, in: shape)
        .overlay { shape.strokeBorder(theme.colour(tone), lineWidth: theme.contrast ? 2.5 : 2) }
        .modifier(Glow(key: waiting, colour: theme.colour(tone), shape: RoundedRectangle(cornerRadius: Radius.card, style: .continuous)))
        // VoiceOver: a group named for what it is, so its answers are
        // heard as the question's or the approval's.
        .accessibilityElement(children: .contain)
        .accessibilityLabel(word)
    }
}

extension HeroCard where Answers == EmptyView {
    public init(tone: Attention.Tone, symbol: String? = nil, word: String, line: String = "", title: String,
                command: String = "", note: (text: String, failed: Bool)? = nil, waiting: String? = nil) {
        self.init(tone: tone, symbol: symbol, word: word, line: line, title: title, command: command, note: note,
                  waiting: waiting) { EmptyView() }
    }
}

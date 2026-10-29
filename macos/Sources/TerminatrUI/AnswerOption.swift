import SwiftUI
import TerminatrKit

/// A key's cap (T845, from direction B): "1" to "9" on a question's
/// options, Y and N on an approval's answers. In mono, the tone's ink
/// on the tone; on a primary button, the button's tone on its ink.
public struct Keycap: View {
    @Environment(\.theme) private var theme
    let key: String
    let tone: Attention.Tone
    let inverted: Bool

    public init(_ key: String, tone: Attention.Tone = .question, inverted: Bool = false) {
        self.key = key
        self.tone = tone
        self.inverted = inverted
    }

    public var body: some View {
        let s = 24 * theme.scale
        Text(key)
            .font(theme.mono(.callout, .medium))
            .foregroundStyle(inverted ? theme.colour(tone) : theme.on(tone))
            .frame(minWidth: s, minHeight: s)
            .background(inverted ? theme.on(tone) : theme.colour(tone), in: RoundedRectangle(cornerRadius: Radius.key * theme.scale, style: .continuous))
            .accessibilityHidden(true)
    }
}

/// A question's option as a row to tap (T845): its keycap, its label and
/// a line of description, at least 44 pt tall. Picked (before sending, on
/// a question that takes several), it's washed in the question colour
/// with an edge of it. With a keyboard (the iPad's) its key picks it,
/// where the environment's answerKeys says so. A pick is a selection tap.
/// Without an action it is the row to read only (a question the phone
/// can't answer, T848).
public struct AnswerOption: View {
    @Environment(\.theme) private var theme
    @State private var taps = 0
    let key: Int
    let label: String
    let detail: String
    let picked: Bool
    let action: (() -> Void)?

    /// key: 1 to 9; one past that has no cap or shortcut.
    public init(key: Int, label: String, detail: String = "", picked: Bool = false, action: (() -> Void)?) {
        self.key = key
        self.label = label
        self.detail = detail
        self.picked = picked
        self.action = action
    }

    public init(key: Int, option: AppAPI.QuestionOption, picked: Bool = false, action: (() -> Void)?) {
        self.init(key: key, label: option.label, detail: option.description, picked: picked, action: action)
    }

    public var body: some View {
        if let action {
            Button { taps += 1; action() } label: { row }
                .buttonStyle(Pressable())
                .answerKey((1...9).contains(key) ? KeyEquivalent(Character(String(key))) : nil)
                .feedback(.selection, trigger: taps)
                .accessibilityLabel(spoken)
                .accessibilityAddTraits(picked ? .isSelected : [])
        } else {
            row
                .accessibilityElement(children: .ignore)
                .accessibilityLabel(spoken)
        }
    }

    private var spoken: String {
        detail.isEmpty ? "\(key). \(label)" : "\(key). \(label), \(detail)"
    }

    private var row: some View {
        let shape = RoundedRectangle(cornerRadius: Radius.row, style: .continuous)
        return HStack(alignment: .center, spacing: Spacing.row) {
            if (1...9).contains(key) {
                Keycap(String(key))
            }
            VStack(alignment: .leading, spacing: 1) {
                Text(label)
                    .font(theme.body(.headline, .semibold))
                    .foregroundStyle(theme.ink)
                    .lines(2)
                if !detail.isEmpty {
                    Text(detail)
                        .font(theme.body(.subheadline))
                        .foregroundStyle(theme.muted)
                        .lines(2)
                }
            }
            .multilineTextAlignment(.leading)
            Spacer(minLength: 0)
            if picked {
                Image(systemName: "checkmark")
                    .font(theme.body(.headline, .bold))
                    .foregroundStyle(theme.ink)
                    .accessibilityHidden(true)
            }
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 9)
        .frame(maxWidth: .infinity, minHeight: tapTarget, alignment: .leading)
        .background(picked ? theme.wash(.question, ThemeColours.pickedWash) : theme.ink.opacity(ThemeColours.inkWash), in: shape)
        .overlay(shape.strokeBorder(picked ? theme.question : theme.line, lineWidth: picked ? 1.5 : 1))
        .contentShape(shape)
    }
}

/// A row that dims and (without Reduce Motion) sinks a little while
/// pressed, as the role buttons do.
struct Pressable: ButtonStyle {
    @Environment(\.accessibilityReduceMotion) private var still

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .opacity(configuration.isPressed ? 0.9 : 1)
            .scaleEffect(configuration.isPressed && !still ? 0.98 : 1)
            .animation(still ? nil : .spring(response: 0.25, dampingFraction: 0.7), value: configuration.isPressed)
    }
}

import SwiftUI
import TerminatrKit

/// A card's frame: its tag, a wash and an edge in the card's colour.
/// A card that waits on the user (a question, an approval, a review)
/// names itself in `waiting`: its glow breathes, and the newest such
/// card's edge runs (Glow).
public struct CardFrame<Content: View>: View {
    @Environment(\.theme) private var theme
    let tag: String
    let colour: Color
    let tint: Color
    let waiting: String?
    let content: Content

    public init(tag: String, colour: Color, tint: Color, waiting: String? = nil, @ViewBuilder content: () -> Content) {
        self.tag = tag
        self.colour = colour
        self.tint = tint
        self.waiting = waiting
        self.content = content()
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 9) {
            Text(tag.uppercased())
                .font(theme.mono(.caption2, .medium))
                .tracking(1)
                .foregroundStyle(colour)
            content
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .frosted(RoundedRectangle(cornerRadius: 14))
        .background(tint, in: RoundedRectangle(cornerRadius: 14))
        .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(colour.opacity(theme.contrast ? 1 : 0.4), lineWidth: 1))
        .modifier(Glow(key: waiting, colour: colour, shape: RoundedRectangle(cornerRadius: 14)))
        // VoiceOver: a group named for the card, so its buttons are
        // heard as the approval's or the question's.
        .accessibilityElement(children: .contain)
        .accessibilityLabel(tag)
    }
}

/// A question's option: its label, and its description under it.
public struct OptionLabel: View {
    @Environment(\.theme) private var theme
    let option: AppAPI.QuestionOption

    public init(_ option: AppAPI.QuestionOption) {
        self.option = option
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 1) {
            Text(option.label)
            if !option.description.isEmpty {
                Text(option.description)
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.muted)
            }
        }
    }
}

/// One question of a session's open menu, to read: its tag, the
/// question and its options. The Mac app's QuestionCard answers it;
/// the phone's first version only shows it.
public struct QuestionSummary: View {
    @Environment(\.theme) private var theme
    let menu: AppAPI.Question
    let index: Int
    /// The card's name while it waits on the user (CardFrame).
    let waiting: String?

    public init(menu: AppAPI.Question, index: Int, waiting: String? = nil) {
        self.menu = menu
        self.index = index
        self.waiting = waiting
    }

    public var body: some View {
        let item = menu.questions[index]
        CardFrame(tag: QuestionPlan.tag(menu, index: index), colour: theme.question, tint: theme.questionTint, waiting: waiting) {
            Text(item.question)
                .font(theme.title(.headline, .bold))
                .textSelection(.enabled)
                .fixedSize(horizontal: false, vertical: true)
            ForEach(Array(item.options.enumerated()), id: \.offset) { i, o in
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Text("\(i + 1).")
                        .font(theme.mono(.subheadline, .medium))
                        .foregroundStyle(theme.question)
                    OptionLabel(o)
                }
                .accessibilityElement(children: .combine)
                .accessibilityLabel(QuestionPlan.spoken(item, option: i))
            }
        }
    }
}

extension Need.Kind {
    /// The need's SF Symbol.
    public var symbol: String {
        switch self {
        case .question: "questionmark.bubble"
        case .approval: "hand.raised"
        case .review: "checkmark.seal"
        case .blocked: "exclamationmark.octagon"
        case .checks: "xmark.circle"
        case .held: "pause.circle"
        case .network: "point.3.connected.trianglepath.dotted"
        case .link: "envelope.badge"
        case .refusal: "shield.lefthalf.filled"
        }
    }

    /// What opening it is for.
    public var verb: String {
        switch self {
        case .question: "Answer"
        case .approval: "Approve"
        case .review: "Review"
        case .refusal: "Answer"
        case .blocked, .checks, .held, .network, .link: "Open"
        }
    }
}

extension Theme {
    /// A need's colour: its symbol, its card's edge.
    public func colour(_ k: Need.Kind) -> Color {
        switch k {
        case .question: question
        case .approval: approval
        case .review: good
        case .blocked, .checks, .refusal: attention
        case .held, .network: muted
        case .link: action
        }
    }
}

/// A need's card's head: its symbol, what it is, and a second line
/// (the task's title, an approval's question).
public struct NeedSummary: View {
    @Environment(\.theme) private var theme
    let need: Need
    let detail: String

    public init(need: Need, detail: String? = nil) {
        self.need = need
        self.detail = detail ?? need.detail
    }

    public var body: some View {
        HStack(alignment: .center, spacing: 8) {
            Image(systemName: need.kind.symbol)
                .foregroundStyle(theme.colour(need.kind))
                .frame(width: 16)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 1) {
                Text(need.what)
                    .font(theme.body(.body, .semibold))
                    .lineLimit(1)
                if !detail.isEmpty {
                    Text(detail)
                        .font(theme.body(.subheadline))
                        .foregroundStyle(theme.muted)
                        .lineLimit(2)
                }
            }
        }
    }
}

import SwiftUI
import TerminatrKit

/// A card's frame, the page's: its tag and a thin still outline in the
/// card's colour, orange for an approval and blue for a question, on the
/// pane (T1002).
public struct CardFrame<Content: View>: View {
    @Environment(\.theme) private var theme
    let tag: String
    let colour: Color
    let content: Content

    public init(tag: String, colour: Color, @ViewBuilder content: () -> Content) {
        self.tag = tag
        self.colour = colour
        self.content = content()
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 9) {
            Text(tag)
                .font(theme.body(.caption))
                .foregroundStyle(colour)
            content
        }
        .padding(.horizontal, 13)
        .padding(.vertical, 11)
        .frame(maxWidth: .infinity, alignment: .leading)
        .solid(RoundedRectangle(cornerRadius: Radius.row))
        .overlay(RoundedRectangle(cornerRadius: Radius.row).strokeBorder(colour, lineWidth: theme.contrast ? 1.5 : 1))
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

    public init(menu: AppAPI.Question, index: Int) {
        self.menu = menu
        self.index = index
    }

    public var body: some View {
        let item = menu.questions[index]
        CardFrame(tag: QuestionPlan.tag(menu, index: index), colour: theme.question) {
            Text(item.question)
                .font(theme.body(.body, .medium))
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
        }
    }

    /// What opening it is for.
    public var verb: String {
        switch self {
        case .question: "Answer"
        case .approval: "Approve"
        case .review: "Review"
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
        case .blocked, .checks: attention
        case .held, .network: muted
        case .link: action
        }
    }
}

/// A need's row: its dot in the need's colour, what it is, and beside it
/// the task's title or an approval's question, on one line.
public struct NeedSummary: View {
    @Environment(\.theme) private var theme
    let need: Need
    let detail: String

    public init(need: Need, detail: String? = nil) {
        self.need = need
        self.detail = detail ?? need.detail
    }

    public var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Circle()
                .fill(theme.colour(need.kind))
                .frame(width: 7, height: 7)
                .accessibilityHidden(true)
            Text(need.what)
                .font(theme.body(.body))
                .lines(1)
                .layoutPriority(1)
            if !detail.isEmpty {
                Text(detail)
                    .font(theme.body(.subheadline))
                    .foregroundStyle(theme.muted)
                    .lines(1)
            }
        }
    }
}

import SwiftUI
import TerminatrKit

/// A session's title in its bar (T848, T1001, the design page's `.phbar`):
/// a role's mark, its title at 14.5, a thread's ref in mono before it, its
/// project after a role's, in muted words; its state's dot and context use
/// at the end. VoiceOver reads it as one header; held, the large content
/// viewer shows it whole, as the bar's text grows only to a point.
public struct SessionTitle: View {
    @Environment(\.theme) private var theme
    let title: String
    let place: String
    /// A coordinator's or director's: its mark before the title (T1003).
    let role: SessionRole?
    let state: Attention?
    let context: ContextUse?

    /// The most the bar's text grows.
    static let most: CGFloat = 1.3

    public init(_ title: String, place: String, role: SessionRole? = nil, state: Attention? = nil, context: ContextUse? = nil) {
        self.title = title
        self.place = place
        self.role = role
        self.state = state
        self.context = context
    }

    public var body: some View {
        var t = theme
        t.scale = min(theme.scale, Self.most)
        return HStack(spacing: Spacing.item) {
            if let role {
                RoleMark(role, points: 13, colour: t.ink)
            }
            if !lead.isEmpty {
                Text(lead)
                    .font(.system(size: 12.5 * t.scale, design: .monospaced))
                    .foregroundStyle(t.muted)
                    .lineLimit(1)
                    .fixedSize()
            }
            Text(title)
                .font(t.sized(14.5, .medium))
                .foregroundStyle(t.ink)
                .lineLimit(1)
            if !rest.isEmpty {
                Text(rest)
                    .font(t.sized(12.5))
                    .foregroundStyle(t.muted)
                    .lineLimit(1)
            }
            Spacer(minLength: 0)
            if let state {
                FlatDot(state.tone)
            }
            if let context {
                Text(context.word)
                    .font(t.sized(12.5))
                    .monospacedDigit()
                    .foregroundStyle(context.tone.map { t.colour($0) } ?? t.muted)
                    .lineLimit(1)
                    .fixedSize()
            }
        }
        .environment(\.theme, t)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(spoken)
        .accessibilityAddTraits(.isHeader)
        .accessibilityShowsLargeContentViewer {
            Text(title)
        }
    }

    /// What goes before the title, a thread's ref: place's first word
    /// ("T14 · acme-web"); a role's place is its project's, which goes after.
    private var lead: String {
        role != nil ? "" : place.components(separatedBy: " · ").first ?? ""
    }

    private var rest: String {
        role != nil ? place : ""
    }

    private var spoken: String {
        [title, place.replacingOccurrences(of: " · ", with: ", "), state?.word ?? "", context?.spoken ?? ""]
            .filter { !$0.isEmpty }.joined(separator: ", ")
    }
}

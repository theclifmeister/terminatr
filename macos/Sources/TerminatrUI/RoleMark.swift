import SwiftUI
import TerminatrKit

/// A role's mark (T1003): its symbol at one weight in a fixed-width slot,
/// so what follows starts at the same place for either role. In ink by
/// default, never an accent: colour is for state. VoiceOver reads the
/// role's word beside it, not the mark.
public struct RoleMark: View {
    @Environment(\.theme) private var theme
    let role: SessionRole
    let points: CGFloat
    let slot: CGFloat?
    let alignment: Alignment
    let colour: Color?

    /// slot nil: wide enough for either symbol at this size. It doesn't
    /// grow with the text size when given: a column's width.
    public init(_ role: SessionRole, points: CGFloat = 12, slot: CGFloat? = nil, alignment: Alignment = .center, colour: Color? = nil) {
        self.role = role
        self.points = points
        self.slot = slot
        self.alignment = alignment
        self.colour = colour
    }

    public var body: some View {
        let size = points * theme.scale
        Image(systemName: role.symbol)
            .font(.system(size: size, weight: .medium))
            .foregroundStyle(colour ?? theme.ink)
            .frame(width: slot ?? Self.slot(size), alignment: alignment)
            .accessibilityHidden(true)
    }

    /// The width that holds either symbol at size.
    static func slot(_ size: CGFloat) -> CGFloat { size * 1.45 }
}

/// A role's mark and word in medium ink (T1003): how a dashboard, an
/// inspector and a header name it. VoiceOver says the word.
public struct RoleLabel: View {
    @Environment(\.theme) private var theme
    let role: SessionRole
    let points: CGFloat

    public init(_ role: SessionRole, points: CGFloat = 13) {
        self.role = role
        self.points = points
    }

    public var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: Spacing.hair) {
            RoleMark(role, points: points * 0.92, alignment: .leading)
            Text(role.word)
                .font(theme.sized(points, .medium))
                .foregroundStyle(theme.ink)
                .lineLimit(1)
                .fixedSize()
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(role.word)
    }
}

/// A project's or program's line on its row (T1003, T1001): the role's
/// mark and word, a dot in its slot (the strongest of what it speaks for),
/// then its state's word and after it what stays (" · no threads"). Nothing before the words moves as
/// the state changes. At accessibility sizes the role over its state.
/// The card says it all to VoiceOver.
public struct RoleLine: View {
    @Environment(\.theme) private var theme
    @Environment(\.dynamicTypeSize) private var typeSize
    let role: SessionRole
    let state: Attention
    let dot: Attention.Tone?
    let after: String
    let selected: Bool

    public init(_ role: SessionRole, state: Attention, dot: Attention.Tone? = nil, after: String = "", selected: Bool = false) {
        self.role = role
        self.state = state
        self.dot = dot
        self.after = after
        self.selected = selected
    }

    public var body: some View {
        if typeSize.isAccessibilitySize {
            VStack(alignment: .leading, spacing: 1) {
                roleWord
                HStack(spacing: Spacing.tight) {
                    FlatDot(dot ?? state.tone)
                    Text(state.word + after)
                        .lines(2)
                }
                .font(theme.sized(12.5))
                .foregroundStyle(selected ? theme.ink : theme.muted)
            }
        } else {
            HStack(spacing: Spacing.hair) {
                roleWord
                FlatDot(dot ?? state.tone)
                    .padding(.horizontal, 2)
                // After it where it fits, never pushing the row out of its
                // panel; in a narrow column the state's word alone.
                ViewThatFits(in: .horizontal) {
                    Text(state.word + after)
                        .lineLimit(1)
                        .fixedSize()
                    Text(state.word)
                        .lineLimit(1)
                }
                .font(theme.sized(12.5))
                .foregroundStyle(selected ? theme.ink : theme.muted)
            }
        }
    }

    private var roleWord: some View {
        HStack(spacing: Spacing.hair) {
            RoleMark(role, points: 11.5, alignment: .leading)
            Text(role.word)
                .font(theme.sized(12.5, .medium))
                .foregroundStyle(theme.ink)
                .lineLimit(1)
                .fixedSize()
        }
    }
}

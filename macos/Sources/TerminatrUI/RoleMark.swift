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

/// A project's or program's line on its card (T1003): the role, its
/// state's symbol in a slot of its own, or the turning mark while it
/// works, then what it does and after it what stays (" · no threads"):
/// the state's words are cut before after is. Nothing before the words
/// moves as the state changes. At accessibility sizes one text, wrapped
/// at its words. The card says it all to VoiceOver.
public struct RoleLine: View {
    @Environment(\.theme) private var theme
    @Environment(\.dynamicTypeSize) private var typeSize
    let role: SessionRole
    let state: Attention
    let after: String

    public init(_ role: SessionRole, state: Attention, after: String = "") {
        self.role = role
        self.state = state
        self.after = after
    }

    public var body: some View {
        let points = CGFloat(TextStyle.subheadline.points)
        let words = state.why.isEmpty ? state.word : state.why
        HStack(spacing: Spacing.hair) {
            RoleMark(role, points: points * 0.92, alignment: .leading)
            Text(role.word)
                .font(theme.body(.subheadline, .medium))
                .foregroundStyle(theme.ink)
                .lineLimit(1)
                .fixedSize()
            mark
                .frame(width: points * theme.scale * 1.15)
            Group {
                if typeSize.isAccessibilitySize {
                    Text(words + after)
                        .lines(1)
                } else {
                    HStack(spacing: 0) {
                        Text(words)
                            .lines(1)
                        Text(after)
                            .lineLimit(1)
                            .fixedSize()
                    }
                }
            }
            .font(theme.body(.subheadline))
            .foregroundStyle(theme.muted)
        }
    }

    @ViewBuilder private var mark: some View {
        if state.tone == .working {
            BusyMark(size: 6 * theme.scale)
        } else {
            Image(systemName: state.tone.symbol)
                .font(theme.body(.footnote))
                .foregroundStyle(state.tone == .idle ? theme.muted : theme.colour(state.tone))
                .accessibilityHidden(true)
        }
    }
}

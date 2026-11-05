import SwiftUI
import TerminatrKit

/// A flat row's state: a 7 pt dot in the state's colour (the design page's
/// `.dot`), green while it works, faint while idle, a moon asleep. The
/// row's words say the state too.
public struct FlatDot: View {
    @Environment(\.theme) private var theme
    let tone: Attention.Tone

    public init(_ tone: Attention.Tone) {
        self.tone = tone
    }

    public var body: some View {
        let side = 7 * theme.scale
        if tone == .asleep {
            Image(systemName: "moon.zzz.fill")
                .font(.system(size: side * 1.4, weight: .semibold))
                .foregroundStyle(theme.muted)
                .frame(width: side, height: side)
                .accessibilityHidden(true)
        } else {
            Circle()
                .fill(colour)
                .frame(width: side, height: side)
                .accessibilityHidden(true)
        }
    }

    private var colour: Color {
        switch tone {
        case .working: theme.good
        case .idle: theme.faint
        default: theme.colour(tone)
        }
    }
}

/// The screen's main row (T1001): a project's coordinator, a program's
/// director, the session the user talks to. It stands first under the
/// header and apart from the rows around it by its size, its role's mark
/// as a tile in ink, and the theme's focus line round it (the line round
/// the focused pane): no accent colour of its own, as colour is for state.
/// What the role does under its word, and at its end what waits on the
/// user, else how long its turn has run, then its context use and a
/// chevron. At accessibility sizes the end goes under the words.
public struct LeadRow: View {
    @Environment(\.theme) private var theme
    @Environment(\.dynamicTypeSize) private var typeSize
    let role: SessionRole
    let state: Attention
    let busy: Busy?
    let context: ContextUse?
    let dormant: Bool
    let selected: Bool

    public init(_ role: SessionRole, state: Attention, busy: Busy? = nil, context: ContextUse? = nil, dormant: Bool = false, selected: Bool = false) {
        self.role = role
        self.state = state
        self.busy = busy
        self.context = context
        self.dormant = dormant
        self.selected = selected
    }

    public var body: some View {
        let shape = RoundedRectangle(cornerRadius: Radius.tile)
        Group {
            if typeSize.isAccessibilitySize {
                VStack(alignment: .leading, spacing: Spacing.item) {
                    HStack(spacing: Spacing.row + 2) {
                        mark
                        Spacer(minLength: 0)
                        chevron
                    }
                    words
                    end
                }
            } else {
                HStack(spacing: Spacing.row + 2) {
                    mark
                    words
                    Spacer(minLength: Spacing.hair)
                    end
                    chevron
                }
            }
        }
        .padding(.horizontal, 13)
        .padding(.vertical, 11)
        .frame(maxWidth: .infinity, minHeight: 64 * theme.scale, alignment: .leading)
        .background(selected ? theme.selection : theme.pane, in: shape)
        .overlay { shape.strokeBorder(dormant ? theme.line : theme.focus, lineWidth: theme.contrast ? 2 : 1) }
        .contentShape(shape)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(role.word)
        .accessibilityValue(dormant ? "inactive" : [busy.map { $0.spoken(now: Date()) } ?? state.spoken, context?.spoken]
            .compactMap { $0 }.joined(separator: ", "))
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    private var mark: some View {
        let side = 38 * theme.scale
        return RoleMark(role, points: 18, slot: side, colour: dormant ? theme.muted : theme.pane)
            .frame(width: side, height: side)
            .background(dormant ? theme.tab : theme.ink, in: RoundedRectangle(cornerRadius: Radius.row))
    }

    private var words: some View {
        let ink = selected ? theme.ink : theme.muted
        return VStack(alignment: .leading, spacing: 2) {
            Text(role.word)
                .font(theme.sized(17, .semibold))
                .foregroundStyle(dormant ? theme.muted : theme.ink)
                .lineLimit(1)
                .fixedSize()
            HStack(spacing: Spacing.tight) {
                FlatDot(dormant ? .idle : state.tone)
                Text(state.why.isEmpty ? state.word : state.why)
                    .font(theme.sized(13))
                    .foregroundStyle(ink)
                    .lines(1)
            }
        }
    }

    @ViewBuilder private var end: some View {
        if !dormant {
            HStack(spacing: Spacing.item) {
                if state.waits {
                    Chip(state.word, colour: theme.colour(state.tone))
                } else if let busy {
                    TimelineView(.periodic(from: .now, by: 1)) { tl in
                        if let t = busy.elapsed(now: tl.date) {
                            Text(t)
                                .font(.system(size: 12.5 * theme.scale, design: .monospaced))
                                .foregroundStyle(selected ? theme.ink : theme.muted)
                                .monospacedDigit()
                                .lineLimit(1)
                                .fixedSize()
                        }
                    }
                    .fixedSize()
                }
                if let context {
                    // The word goes in a narrow column.
                    ViewThatFits(in: .horizontal) {
                        contextWords("\(context.word) context", context)
                        contextWords(context.word, context)
                    }
                    .layoutPriority(-1)
                }
            }
        }
    }

    private func contextWords(_ words: String, _ context: ContextUse) -> some View {
        Text(words)
            .font(theme.sized(12.5))
            .monospacedDigit()
            .foregroundStyle(context.tone.map { theme.colour($0) } ?? (selected ? theme.ink : theme.muted))
            .lineLimit(1)
            .fixedSize()
    }

    private var chevron: some View {
        Image(systemName: "chevron.right")
            .font(.system(size: 13 * theme.scale, weight: .semibold))
            .foregroundStyle(selected ? theme.ink : theme.muted)
            .accessibilityHidden(true)
    }
}

/// A few small tabs in a row (the design page's `.mini`): 12.5, 5 round,
/// the picked one on the tab's fill in ink, each 44 pt to tap. At the
/// widths where they don't fit (accessibility sizes) they wrap. Picking
/// one is a selection tap.
public struct MiniTabs<ID: Hashable>: View {
    @Environment(\.theme) private var theme
    let items: [(id: ID, title: String)]
    @Binding var selection: ID

    public init(_ items: [(id: ID, title: String)], selection: Binding<ID>) {
        self.items = items
        _selection = selection
    }

    public var body: some View {
        FlowRow(spacing: Spacing.hair) {
            ForEach(items, id: \.id) { item in
                let on = item.id == selection
                Button {
                    selection = item.id
                } label: {
                    Text(item.title)
                        .font(theme.sized(12.5))
                        .foregroundStyle(on ? theme.ink : theme.muted)
                        .lineLimit(1)
                        .padding(.horizontal, 10)
                        .padding(.vertical, 4)
                        .background { if on { RoundedRectangle(cornerRadius: 5).fill(theme.tab) } }
                        // 44 pt to tap, the row keeps its height.
                        .padding(.vertical, 8)
                        .contentShape(Rectangle())
                        .padding(.vertical, -8)
                }
                .buttonStyle(.plain)
                .accessibilityAddTraits(on ? .isSelected : [])
            }
        }
        .feedback(.selection, trigger: selection)
    }
}

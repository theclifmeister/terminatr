import SwiftUI
import TerminatrKit

/// The composer dock's parts (T848, direction A with direction B's key
/// strip): the dock floats over the conversation's end, with no bar
/// under it, a circle to switch it between the prompt and the keys, the
/// field on a deck card's glass, and a round send button.
extension View {
    /// The dock's field: a deck card's glass in a capsule, at least 44 pt
    /// tall.
    public func dockField() -> some View {
        modifier(DockField())
    }
}

private struct DockField: ViewModifier {
    func body(content: Content) -> some View {
        content
            .padding(.leading, Spacing.card)
            .padding(.trailing, Spacing.hair)
            .frame(maxWidth: .infinity, minHeight: tapTarget, alignment: .leading)
            .surface(.card, in: RoundedRectangle(cornerRadius: tapTarget / 2, style: .continuous))
    }
}

/// The keys a terminal wants that a phone's keyboard hasn't, as a strip
/// of keycaps that stays in view above the dock while keys mode is on,
/// the keyboard up or not (T848): each a glass cap with an ink wash, its
/// word in mono or its arrow, ⌃C filled in the attention colour. Each is
/// 44 pt tall and shares the width; where they don't fit at 44 pt wide
/// (accessibility sizes, a narrow column) the strip scrolls sideways. A
/// key is a light tap.
public struct KeyStrip: View {
    @Environment(\.theme) private var theme
    @State private var taps = 0
    let keys: [TerminalKey]
    let action: (TerminalKey) -> Void

    public init(_ keys: [TerminalKey] = TerminalKey.allCases, action: @escaping (TerminalKey) -> Void) {
        self.keys = keys
        self.action = action
    }

    public var body: some View {
        ViewThatFits(in: .horizontal) {
            HStack(spacing: Spacing.hair) { caps(fill: true) }
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: Spacing.hair) { caps(fill: false) }
            }
        }
        .feedback(.impact(weight: .light), trigger: taps)
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Terminal keys")
    }

    private func caps(fill: Bool) -> some View {
        ForEach(keys) { k in
            let shape = RoundedRectangle(cornerRadius: Radius.key + 2, style: .continuous)
            Button { taps += 1; action(k) } label: {
                Group {
                    if let cap = k.cap {
                        Text(cap).font(theme.mono(.subheadline, .semibold))
                    } else {
                        Image(systemName: k.symbol).font(theme.body(.subheadline, .semibold))
                    }
                }
                .lineLimit(1)
                .fixedSize()
                .foregroundStyle(k == .ctrlC ? theme.onAttention : theme.ink)
                .padding(.horizontal, Spacing.item)
                .frame(minWidth: tapTarget, maxWidth: fill ? .infinity : nil, minHeight: tapTarget)
                .background { cap(k, shape) }
                .contentShape(shape)
            }
            .buttonStyle(Pressable())
            .accessibilityLabel(k.spoken)
        }
    }

    @ViewBuilder private func cap(_ k: TerminalKey, _ shape: RoundedRectangle) -> some View {
        if k == .ctrlC {
            shape.fill(theme.attention)
        } else {
            shape.fill(theme.ink.opacity(ThemeColours.inkWash))
                .surface(.glass, in: shape)
        }
    }
}

/// The dock's choice of how a prompt goes while the agent works (T848):
/// Queue (after its turn) or Steer (into it), small segments in the
/// field, the picked one ink with the base's words. Each is 44 pt tall
/// to tap. Picking is a selection tap.
public struct DockModes<ID: Hashable>: View {
    @Environment(\.theme) private var theme
    let items: [(id: ID, label: String)]
    @Binding var selection: ID

    public init(_ items: [(id: ID, label: String)], selection: Binding<ID>) {
        self.items = items
        _selection = selection
    }

    public var body: some View {
        HStack(spacing: 0) {
            ForEach(items, id: \.id) { item in
                let on = item.id == selection
                Button { selection = item.id } label: {
                    Text(item.label)
                        .font(theme.body(.footnote, .semibold))
                        .foregroundStyle(on ? theme.base : theme.ink)
                        .lineLimit(1)
                        .fixedSize()
                        .padding(.horizontal, 10)
                        .padding(.vertical, 5)
                        .background { if on { Capsule().fill(theme.ink) } }
                        .frame(minHeight: tapTarget)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityAddTraits(on ? .isSelected : [])
            }
        }
        .padding(.horizontal, 3)
        .background(theme.ink.opacity(ThemeColours.inkWash), in: Capsule().inset(by: 6))
        .feedback(.selection, trigger: selection)
    }
}

/// A line over the dock: what the last send did, the queue, a held
/// queue, a takeover's warning, on a glass capsule; a failure in ink
/// after the attention mark.
public struct DockNote: View {
    @Environment(\.theme) private var theme
    let text: String
    let failed: Bool

    public init(_ text: String, failed: Bool = false) {
        self.text = text
        self.failed = failed
    }

    public var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: Spacing.tight) {
            if failed {
                Image(systemName: "exclamationmark.triangle.fill")
                    .foregroundStyle(theme.attention)
                    .accessibilityHidden(true)
            }
            Text(text)
                .foregroundStyle(failed ? theme.ink : theme.muted)
                .lineLimit(3)
        }
        .font(theme.body(.footnote, .medium))
        .padding(.horizontal, Spacing.row)
        .padding(.vertical, Spacing.tight)
        .surface(.glass, in: RoundedRectangle(cornerRadius: Radius.tile, style: .continuous))
    }
}

/// The dock's row: its mode circle, its field and its send, side by
/// side; at accessibility sizes, where the circles grow, the field
/// takes a line of its own over them, so it keeps its width.
public struct DockRow<Mode: View, Field: View, Send: View>: View {
    @Environment(\.dynamicTypeSize) private var typeSize
    let mode: Mode
    let field: Field
    let send: Send

    public init(@ViewBuilder mode: () -> Mode, @ViewBuilder field: () -> Field, @ViewBuilder send: () -> Send) {
        self.mode = mode()
        self.field = field()
        self.send = send()
    }

    public var body: some View {
        if typeSize.isAccessibilitySize {
            VStack(spacing: Spacing.item) {
                field
                HStack {
                    mode
                    Spacer(minLength: 0)
                    send
                }
            }
        } else {
            HStack(alignment: .bottom, spacing: Spacing.item) {
                mode
                field
                send
            }
        }
    }
}

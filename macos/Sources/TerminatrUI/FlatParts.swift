import SwiftUI
import TerminatrKit

/// The flat look's measures for what stands in a pane (T1002), from the
/// design page: rows 42 high in hairline lists, labels and status at 13,
/// rows and text at 14, chips 4 round, inner controls 6, panes 8.
public enum Flat {
    /// A list's row.
    public static let row: CGFloat = 42
    /// How wide the conversation's text, tool rows and cards run.
    public static let reading: CGFloat = 820
    /// How wide a user's prompt runs.
    public static let prompt: CGFloat = 640
    /// A label's and a status's size.
    public static let label: CGFloat = 13
    /// A row's and running text's size.
    public static let text: CGFloat = 14
    /// A chip's size, as the inspector's header's.
    public static let chip: CGFloat = 11.5
}

/// A small word in a 4 round hairline, or filled when it says a state:
/// a priority, a status, a server's name.
public struct Chip: View {
    @Environment(\.theme) private var theme
    let word: String
    let colour: Color?
    let fill: Color?
    let mono: Bool

    /// colour: its word and line (muted when nil); fill: a solid chip's
    /// fill, the word then colour on it.
    public init(_ word: String, colour: Color? = nil, fill: Color? = nil, mono: Bool = true) {
        self.word = word
        self.colour = colour
        self.fill = fill
        self.mono = mono
    }

    public var body: some View {
        let shape = RoundedRectangle(cornerRadius: Radius.chip)
        let ink = colour ?? theme.muted
        Text(word)
            .font(.system(size: Flat.chip * theme.scale, design: mono ? .monospaced : .default))
            .foregroundStyle(ink)
            .lineLimit(1)
            .fixedSize()
            .padding(.horizontal, 6)
            .padding(.vertical, 1)
            .background { if let fill { shape.fill(fill) } }
            .overlay { if fill == nil { shape.strokeBorder(colour?.opacity(theme.contrast ? 1 : 0.5) ?? theme.line, lineWidth: 1) } }
    }
}

/// The page's small button: 13, 10 at its sides, 6 round, on the tab's
/// fill; the primary one ink with the pane's words. It takes the place of
/// .bordered and .borderedProminent in a pane.
public struct FlatButtonStyle: ButtonStyle {
    let primary: Bool

    public init(primary: Bool = false) {
        self.primary = primary
    }

    public func makeBody(configuration: Configuration) -> some View {
        FlatButton(primary: primary, configuration: configuration)
    }
}

extension ButtonStyle where Self == FlatButtonStyle {
    public static var flat: FlatButtonStyle { FlatButtonStyle() }
    public static var flatPrimary: FlatButtonStyle { FlatButtonStyle(primary: true) }
}

private struct FlatButton: View {
    @Environment(\.theme) private var theme
    @Environment(\.isEnabled) private var enabled
    @Environment(\.isFocused) private var focused
    let primary: Bool
    let configuration: ButtonStyleConfiguration

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: Radius.row)
        configuration.label
            .font(theme.sized(Flat.label))
            .lineLimit(1)
            .foregroundStyle(!enabled ? theme.muted : primary ? theme.pane : theme.ink)
            .padding(.horizontal, 10)
            .padding(.vertical, 4)
            .background(primary && enabled ? theme.ink : theme.tab, in: shape)
            .overlay {
                // Full Keyboard Access: where the keys are.
                if focused { shape.strokeBorder(theme.focus, lineWidth: 2) }
            }
            .opacity(configuration.isPressed ? 0.8 : 1)
            .contentShape(shape)
    }
}

/// A group's label over its list: 13, muted, a header to VoiceOver.
public struct PaneLabel: View {
    @Environment(\.theme) private var theme
    let title: String

    public init(_ title: String) {
        self.title = title
    }

    public var body: some View {
        Text(title)
            .font(theme.sized(Flat.label))
            .foregroundStyle(theme.muted)
            .accessibilityAddTraits(.isHeader)
    }
}

/// Rows in a 6 round hairline, a soft line between them. The lines are
/// its ground showing in the one point between rows, so a row is any
/// view on the pane's colour (ListRow).
public struct HairlineList<Content: View>: View {
    @Environment(\.theme) private var theme
    let content: Content

    public init(@ViewBuilder content: () -> Content) {
        self.content = content()
    }

    public var body: some View {
        let shape = RoundedRectangle(cornerRadius: Radius.row)
        LazyVStack(alignment: .leading, spacing: 1) {
            content
        }
        .background(theme.lineSoft)
        .clipShape(shape)
        .overlay { shape.strokeBorder(theme.line, lineWidth: theme.contrast ? 1.5 : 1).allowsHitTesting(false) }
    }
}

/// A HairlineList's row: 42 high at least, 13 at its sides, at 14.
public struct ListRow<Content: View>: View {
    @Environment(\.theme) private var theme
    let content: Content

    public init(@ViewBuilder content: () -> Content) {
        self.content = content()
    }

    public var body: some View {
        content
            .font(theme.sized(Flat.text))
            .padding(.horizontal, 13)
            .padding(.vertical, 7)
            .frame(maxWidth: .infinity, minHeight: Flat.row * theme.scale, alignment: .leading)
            .background(theme.pane)
    }
}

/// The slim stat strip: numbers in mono beside their words, a line
/// between them, 560 wide at most.
public struct StatStrip: View {
    @Environment(\.theme) private var theme
    let stats: [(value: String, word: String)]

    public init(_ stats: [(value: String, word: String)]) {
        self.stats = stats
    }

    public var body: some View {
        let shape = RoundedRectangle(cornerRadius: Radius.row)
        HStack(spacing: 1) {
            ForEach(Array(stats.enumerated()), id: \.offset) { _, s in
                HStack(alignment: .firstTextBaseline, spacing: 7) {
                    Text(s.value)
                        .font(.system(size: 15 * theme.scale, weight: .medium, design: .monospaced))
                    Text(s.word)
                        .font(theme.sized(Flat.label))
                        .foregroundStyle(theme.muted)
                        .lineLimit(1)
                }
                .padding(.horizontal, 13)
                .padding(.vertical, 8)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(theme.pane)
                .accessibilityElement(children: .combine)
            }
        }
        .fixedSize(horizontal: false, vertical: true)
        .background(theme.line)
        .clipShape(shape)
        .overlay { shape.strokeBorder(theme.line, lineWidth: theme.contrast ? 1.5 : 1) }
        .frame(maxWidth: 560, alignment: .leading)
    }
}

/// A project's or a program's tile, its name's letters on the action's
/// colour.
public struct NameTile: View {
    @Environment(\.theme) private var theme
    let name: String
    let side: CGFloat
    let radius: CGFloat

    public init(_ name: String, side: CGFloat = 30, radius: CGFloat = Radius.row) {
        self.name = name
        self.side = side
        self.radius = radius
    }

    public var body: some View {
        Text(ProjectTile.letters(name.split(separator: "/").last.map(String.init) ?? name))
            .font(theme.sized(side * 13 / 30, .semibold))
            .foregroundStyle(theme.onAction)
            .frame(width: side, height: side)
            .background(theme.action, in: RoundedRectangle(cornerRadius: radius))
            .accessibilityHidden(true)
    }
}

/// Its children in rows that wrap at the width they have: a question's
/// small buttons.
public struct FlowRow: Layout {
    let spacing: CGFloat

    public init(spacing: CGFloat = 7) {
        self.spacing = spacing
    }

    public func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let (_, size) = place(subviews, width: proposal.width ?? .infinity)
        return size
    }

    public func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        let (spots, _) = place(subviews, width: bounds.width)
        for (v, p) in zip(subviews, spots) {
            v.place(at: CGPoint(x: bounds.minX + p.x, y: bounds.minY + p.y), proposal: ProposedViewSize(v.sizeThatFits(.unspecified)))
        }
    }

    private func place(_ subviews: Subviews, width: CGFloat) -> ([CGPoint], CGSize) {
        var spots: [CGPoint] = []
        var x: CGFloat = 0, y: CGFloat = 0, line: CGFloat = 0, widest: CGFloat = 0
        for v in subviews {
            let s = v.sizeThatFits(.unspecified)
            if x > 0, x + s.width > width {
                x = 0
                y += line + spacing
                line = 0
            }
            spots.append(CGPoint(x: x, y: y))
            x += s.width + spacing
            widest = max(widest, x - spacing)
            line = max(line, s.height)
        }
        return (spots, CGSize(width: widest, height: y + line))
    }
}

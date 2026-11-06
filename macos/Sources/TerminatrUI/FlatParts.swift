import SwiftUI
import TerminatrKit
#if canImport(UIKit)
import UIKit
#endif

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
    /// A conversation's messages and the composer's text, in mono.
    public static let conversation: CGFloat = 13
    /// A chip's size, as the inspector's header's.
    public static let chip: CGFloat = 11.5
    /// A list column's side margin on the phone (`.colL`, `.pin`); what
    /// stands beside it keeps Spacing.gutter (`.colR`).
    public static let listGutter: CGFloat = 14
    /// A row's size in a phone's `.list`.
    public static let listText: CGFloat = 13.5
    /// A `.list` row's least height on the phone.
    public static let listRow: CGFloat = 42
}

extension View {
    /// The design page's line height on the phone (1.45, T1008): points'
    /// text gets its line boxes, between lines and at its top and bottom. The
    /// Mac's text keeps the system's line height.
    /// `mono`: the text is in JetBrains Mono, whose lines are taller.
    public func pageLeading(_ points: CGFloat, mono: Bool = false) -> some View {
        modifier(PageLeading(points: points, mono: mono))
    }
}

private struct PageLeading: ViewModifier {
    @Environment(\.theme) private var theme
    let points: CGFloat
    let mono: Bool

    func body(content: Content) -> some View {
        #if canImport(UIKit)
        let size = points * theme.scale
        let font = mono ? JetBrainsMono.font(size: size) : UIFont.systemFont(ofSize: size)
        let extra = max(size * 1.45 - font.lineHeight, 0)
        content
            .lineSpacing(extra)
            .padding(.vertical, extra / 2)
        #else
        content
        #endif
    }
}

extension EnvironmentValues {
    /// An approval's answers as the design page's small `.opt` buttons, in a
    /// conversation (NeedActs); Needs you's card keeps its large ones.
    @Entry public var compactAnswers = false
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
            .font(mono ? theme.monoSized(Flat.chip) : theme.sized(Flat.chip))
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
    /// How many lines its words may take where the width runs out: a
    /// long option in the inspector's narrow panel, say.
    let lines: Int
    let radius: CGFloat

    public init(primary: Bool = false, lines: Int = 1, radius: CGFloat = Radius.row) {
        self.primary = primary
        self.lines = lines
        self.radius = radius
    }

    public func makeBody(configuration: Configuration) -> some View {
        FlatButton(primary: primary, lines: lines, radius: radius, configuration: configuration)
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
    let lines: Int
    let radius: CGFloat
    let configuration: ButtonStyleConfiguration

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: radius)
        configuration.label
            .font(theme.sized(Flat.label))
            .pageLeading(Flat.label)
            .lineLimit(lines)
            .multilineTextAlignment(.leading)
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
            #if os(iOS)
            // 44 pt to tap, the row keeps its height.
            .padding(.vertical, 9)
            .contentShape(Rectangle())
            .padding(.vertical, -9)
            #endif
    }
}

/// A plain button that's only its words (T1016): muted, ink under the
/// pointer, no fill, for what shouldn't look like a main action.
public struct FlatTextButtonStyle: ButtonStyle {
    let points: CGFloat

    /// At the small button's size and height by default, to sit beside one.
    public init(points: CGFloat = Flat.label) {
        self.points = points
    }

    public func makeBody(configuration: Configuration) -> some View {
        FlatTextButton(points: points, configuration: configuration)
    }
}

extension ButtonStyle where Self == FlatTextButtonStyle {
    public static var flatText: FlatTextButtonStyle { FlatTextButtonStyle() }
}

private struct FlatTextButton: View {
    @Environment(\.theme) private var theme
    @Environment(\.isEnabled) private var enabled
    @Environment(\.isFocused) private var focused
    @State private var hovering = false
    let points: CGFloat
    let configuration: ButtonStyleConfiguration

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: Radius.row)
        configuration.label
            .font(theme.sized(points))
            .lineLimit(1)
            .foregroundStyle(!enabled ? theme.faint : hovering || configuration.isPressed ? theme.ink : theme.muted)
            .padding(.horizontal, 4)
            .padding(.vertical, 4)
            .overlay {
                if focused { shape.strokeBorder(theme.focus, lineWidth: 2) }
            }
            .contentShape(shape)
            .onHover { hovering = $0 }
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
    let radius: CGFloat
    let content: Content

    public init(radius: CGFloat = Radius.row, @ViewBuilder content: () -> Content) {
        self.radius = radius
        self.content = content()
    }

    public var body: some View {
        let shape = RoundedRectangle(cornerRadius: radius)
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
    let selected: Bool
    let content: Content

    public init(selected: Bool = false, @ViewBuilder content: () -> Content) {
        self.selected = selected
        self.content = content()
    }

    public var body: some View {
        content
            .font(theme.sized(Flat.text))
            .padding(.horizontal, 13)
            .padding(.vertical, 7)
            .frame(maxWidth: .infinity, minHeight: Flat.row * theme.scale, alignment: .leading)
            .background(selected ? theme.selection : theme.pane)
    }
}

/// A share as a thin bar: 4 high, on the line's colour.
public struct Meter: View {
    @Environment(\.theme) private var theme
    let fraction: Double
    /// Its fill: the action's colour unless given.
    var colour: Color?

    public init(fraction: Double, colour: Color? = nil) {
        self.fraction = fraction
        self.colour = colour
    }

    public var body: some View {
        GeometryReader { g in
            ZStack(alignment: .leading) {
                Capsule().fill(theme.line)
                Capsule()
                    .fill(colour ?? theme.action)
                    .frame(width: g.size.width * min(max(fraction, 0), 1))
            }
        }
        .accessibilityHidden(true)
    }
}

/// The slim stat strip: numbers in mono beside their words, a line
/// between them, 560 wide at most on the Mac, the whole width on the
/// phone, where it is an 8 round panel (T1001), its cells one over another
/// at accessibility sizes.
public struct StatStrip: View {
    @Environment(\.theme) private var theme
    @Environment(\.dynamicTypeSize) private var typeSize
    let stats: [(value: String, word: String)]
    let widest: CGFloat
    let radius: CGFloat

    public init(_ stats: [(value: String, word: String)], widest: CGFloat = 560, radius: CGFloat = Radius.row) {
        self.stats = stats
        self.widest = widest
        self.radius = radius
    }

    public var body: some View {
        let shape = RoundedRectangle(cornerRadius: radius)
        Group {
            if typeSize.isAccessibilitySize {
                VStack(spacing: 1) { cells(stacked: false) }
            } else {
                // Number and word side by side where every cell has room,
                // else over one another, in all the cells alike.
                ViewThatFits(in: .horizontal) {
                    HStack(spacing: 1) { cells(stacked: false) }
                    HStack(spacing: 1) { cells(stacked: true) }
                }
            }
        }
        .fixedSize(horizontal: false, vertical: true)
        .background(theme.line)
        .clipShape(shape)
        .overlay { shape.strokeBorder(theme.line, lineWidth: theme.contrast ? 1.5 : 1) }
        .frame(maxWidth: widest, alignment: .leading)
    }

    private func cells(stacked: Bool) -> some View {
        ForEach(Array(stats.enumerated()), id: \.offset) { _, s in
            Group {
                if stacked {
                    VStack(alignment: .leading, spacing: 0) { cell(s) }
                } else {
                    HStack(alignment: .firstTextBaseline, spacing: 7) { cell(s) }
                }
            }
            .padding(.horizontal, stacked ? 10 : 13)
            .padding(.vertical, 8)
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
            .background(theme.pane)
            .accessibilityElement(children: .combine)
        }
    }

    @ViewBuilder private func cell(_ s: (value: String, word: String)) -> some View {
        Text(s.value)
            .font(theme.monoSized(15, .medium))
            .lineLimit(1)
            .fixedSize()
        Text(s.word)
            .font(theme.sized(12.5))
            .foregroundStyle(theme.muted)
            .pageLeading(12.5)
            .lineLimit(1)
            .fixedSize()
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
/// small buttons. One wider than the whole width is offered that width,
/// so its words wrap or truncate rather than run out of the row.
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
            v.place(at: CGPoint(x: bounds.minX + p.x, y: bounds.minY + p.y), proposal: ProposedViewSize(size(of: v, width: bounds.width)))
        }
    }

    private func size(of v: LayoutSubview, width: CGFloat) -> CGSize {
        let s = v.sizeThatFits(.unspecified)
        return s.width > width ? v.sizeThatFits(ProposedViewSize(width: width, height: nil)) : s
    }

    private func place(_ subviews: Subviews, width: CGFloat) -> ([CGPoint], CGSize) {
        var spots: [CGPoint] = []
        var x: CGFloat = 0, y: CGFloat = 0, line: CGFloat = 0, widest: CGFloat = 0
        for v in subviews {
            let s = size(of: v, width: width)
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

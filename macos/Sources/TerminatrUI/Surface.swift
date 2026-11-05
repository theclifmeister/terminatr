import SwiftUI
import TerminatrKit

/// Corner radii, by what they round.
public enum Radius {
    /// An option row, a command block, a button, a sidebar row.
    public static let row: CGFloat = 6
    /// A StateGlyph's square at 38 pt.
    public static let glyph: CGFloat = 6
    /// A keycap at 24 pt.
    public static let key: CGFloat = 4
    /// A chip: a state, a priority (T1002).
    public static let chip: CGFloat = 4
    /// A list's group, a pane.
    public static let tile: CGFloat = 8
    /// A card.
    public static let card: CGFloat = 8
}

/// Spacing, from a glyph's gap to a screen's gutter.
public enum Spacing {
    public static let hair: CGFloat = 4
    public static let tight: CGFloat = 6
    public static let item: CGFloat = 8
    public static let row: CGFloat = 10
    public static let card: CGFloat = 14
    public static let gutter: CGFloat = 16
}

/// The least side of anything tapped (Apple's 44 pt).
public let tapTarget: CGFloat = 44

extension View {
    /// The flat look's one surface, in a shape: the pane's solid colour
    /// and a hairline; with Increase Contrast a stronger one. No blur, no
    /// shadow, nothing shows through.
    public func surface<S: InsettableShape>(in shape: S) -> some View {
        modifier(SurfaceModifier(shape: shape))
    }

    /// The pane's solid colour behind the content, with no line.
    public func solid<S: Shape>(_ shape: S) -> some View {
        modifier(Solid(shape: shape))
    }
}

private struct SurfaceModifier<S: InsettableShape>: ViewModifier {
    @Environment(\.theme) private var theme
    let shape: S

    func body(content: Content) -> some View {
        content
            .background(theme.pane, in: shape)
            .overlay { shape.strokeBorder(theme.line, lineWidth: theme.contrast ? 1.5 : 1) }
    }
}

private struct Solid<S: Shape>: ViewModifier {
    @Environment(\.theme) private var theme
    let shape: S

    func body(content: Content) -> some View {
        content.background(theme.pane, in: shape)
    }
}

extension Theme {
    /// A tone's wash: a soft StateGlyph's fill, a tone dot's halo
    /// ("a … wash" in the checks, at ThemeColours.toneWash).
    public func wash(_ tone: Attention.Tone, _ share: Double = ThemeColours.toneWash) -> Color {
        colour(tone).opacity(share)
    }
}

import SwiftUI
import TerminatrKit

/// The redesign's surfaces (T845, direction A "Glass deck"): what sits
/// over the mesh, from the thinnest to the most lifted. Each is the
/// frost (Frosted) under a theme tint and an edge; ThemeColours.checks
/// measures the text on each.
public enum Surface: Sendable {
    /// A header's circle buttons, the pulse chips, a tile: the panel's
    /// glass ("the glass").
    case glass
    /// A deck card: the deeper glass, so the mesh shows through ("a deck
    /// card").
    case card
    /// A card lifted off the deck, the lead need: the deck card with a
    /// deeper shadow.
    case raised
}

/// Corner radii, by what they round.
public enum Radius {
    /// An option row, a command block.
    public static let row: CGFloat = 14
    /// A StateGlyph's squircle at 38 pt.
    public static let glyph: CGFloat = 12
    /// A keycap at 24 pt.
    public static let key: CGFloat = 8
    /// A tile, a list's group.
    public static let tile: CGFloat = 18
    /// A deck card.
    public static let card: CGFloat = 26
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
    /// The surface behind the content, in a shape: the frost, the
    /// surface's tint, a hairline edge, and for a card its shadow. With
    /// Increase Contrast the tint is opaque and the edge the muted ink.
    public func surface<S: InsettableShape>(_ s: Surface, in shape: S) -> some View {
        modifier(SurfaceModifier(surface: s, shape: shape))
    }
}

private struct SurfaceModifier<S: InsettableShape>: ViewModifier {
    @Environment(\.theme) private var theme
    let surface: Surface
    let shape: S

    func body(content: Content) -> some View {
        content
            .background(tint, in: shape)
            .frosted(shape)
            .overlay(shape.strokeBorder(theme.edge, lineWidth: theme.contrast ? 1.5 : 1))
            .shadow(color: .black.opacity(shadow), radius: surface == .raised ? 18 : 10, y: surface == .raised ? 10 : 5)
    }

    private var tint: Color {
        surface == .glass ? theme.glass : theme.deck
    }

    private var shadow: Double {
        switch surface {
        case .glass: 0
        case .card: 0.10
        case .raised: 0.18
        }
    }
}

extension Theme {
    /// A tone's wash: a soft StateGlyph's fill, a tone dot's halo
    /// ("a … wash" in the checks, at ThemeColours.toneWash).
    public func wash(_ tone: Attention.Tone, _ share: Double = ThemeColours.toneWash) -> Color {
        colour(tone).opacity(share)
    }
}

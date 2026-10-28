import SwiftUI

extension View {
    /// A panel's frost behind the content: the system material, or with
    /// Increase Contrast the theme's opaque card.
    public func frosted<S: Shape>(_ shape: S) -> some View {
        modifier(Frosted(shape: shape))
    }
}

private struct Frosted<S: Shape>: ViewModifier {
    @Environment(\.theme) private var theme
    let shape: S

    func body(content: Content) -> some View {
        if theme.contrast {
            content.background(theme.cardTint, in: shape)
        } else {
            content.background(.regularMaterial, in: shape)
        }
    }
}

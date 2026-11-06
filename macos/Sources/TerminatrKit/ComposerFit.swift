import CoreGraphics

/// Whether a composer's buttons fit beside its field (T1033): with less
/// than leastField left for the field, text wraps mid-word and the
/// placeholder cuts, so the buttons take a row of their own under it.
public enum ComposerFit {
    /// The least the field may be beside the buttons at type scale 1.
    public static let leastField: CGFloat = 240

    /// Whether the buttons, buttons wide with their gaps, go under the
    /// field in a row width wide.
    public static func stacks(width: CGFloat, buttons: CGFloat, leastField: CGFloat = leastField) -> Bool {
        width - buttons < leastField
    }
}

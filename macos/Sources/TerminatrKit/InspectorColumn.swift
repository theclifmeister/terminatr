import Foundation

/// The inspector's panel (T1000), in the main window and a session's own:
/// 406 wide as the flat look's reference has it until the user drags it.
public enum InspectorColumn {
    public static let min: Double = 300
    public static let ideal: Double = 406
    public static let max: Double = 560

    /// The width it opens at: the one the user left, kept in range, or
    /// the ideal.
    public static func width(saved: Double?) -> Double {
        guard let saved else { return ideal }
        return Swift.min(max, Swift.max(min, saved))
    }
}

import Foundation

/// The inspector's panel (T1000), in the main window and a session's own:
/// 406 wide as the flat look's reference has it until the user drags it; no greatest, see WindowColumns.limit.
public enum InspectorColumn {
    public static let min: Double = 300
    public static let ideal: Double = 406

    /// The width it opens at: the one the user left, kept at its least or more, or
    /// the ideal.
    public static func width(saved: Double?) -> Double {
        guard let saved else { return ideal }
        return Swift.max(min, saved)
    }
}

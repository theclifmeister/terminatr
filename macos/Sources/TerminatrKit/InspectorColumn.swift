import Foundation

/// The inspector's column, in the main window and a session's own: at
/// least as wide as the toolbar items over it (Search, the Needs you
/// flag with its count, the inspector's toggle and the Conversation or
/// Terminal switch), none dropped. Measured on macOS 27 with a window's
/// toolbar: 380 points with a one-digit count, 388 with two, 396 with
/// three. The app's text size doesn't scale the toolbar.
public enum InspectorColumn {
    public static let min: Double = 396
    public static let max: Double = 520

    /// The width it opens at: the one the user left, kept in range, or
    /// the least.
    public static func ideal(saved: Double?) -> Double {
        guard let saved else { return min }
        return Swift.min(max, Swift.max(min, saved))
    }
}

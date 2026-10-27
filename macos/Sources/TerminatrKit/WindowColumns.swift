import Foundation

/// The windows' split view columns (T777): each column's least width is
/// one of these, never its content's, and together they fit in the
/// window at its least. A column whose least width came from what it
/// shows changed it as rows loaded and scrolled, the split view took it
/// up during AppKit's constraints pass, and the window ran out of passes
/// (an NSException: the app quit). The content takes whatever width the
/// column has (ColumnFit).
public enum WindowColumns {
    /// The main window's least width.
    public static let window: Double = 980
    /// A session's own window's (Open in New Window).
    public static let sessionWindow: Double = 760
    /// The sidebar's least, ideal and greatest width.
    public static let sidebarMin: Double = 210
    public static let sidebarIdeal: Double = 240
    public static let sidebarMax: Double = 340
    /// The detail's least width: a session's conversation or terminal,
    /// or a project's dashboard.
    public static let detailMin: Double = 340
    /// What a divider and its edge take, at most, between two columns.
    public static let divider: Double = 4

    /// The least width the main window's columns need, all shown.
    public static var mainNeeds: Double {
        sidebarMin + detailMin + InspectorColumn.min + 2 * divider
    }

    /// The least width a session window's columns need, its inspector
    /// shown.
    public static var sessionNeeds: Double {
        detailMin + InspectorColumn.min + divider
    }
}

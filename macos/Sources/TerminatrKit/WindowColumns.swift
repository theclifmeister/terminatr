import Foundation

/// The window's columns, flat (T1000): the sidebar, the main pane and the
/// inspector's panel, in the frame the flat look's reference measures.
/// A window at the reference's 1876 wide reads, left to right: the
/// window's edge 1, the sidebar 296 with its line, 9, the pane 1146, 9,
/// the panel 406, 8, the window's edge 1.
public enum WindowColumns {
    /// The main window's least width.
    public static let window: Double = 980
    /// A session's own window's (Open in New Window).
    public static let sessionWindow: Double = 760
    /// The sidebar's least, ideal and greatest width, its line included.
    public static let sidebarMin: Double = 240
    public static let sidebarIdeal: Double = 296
    public static let sidebarMax: Double = 420
    /// The main pane's least width: a session's conversation or
    /// terminal, or a project's dashboard.
    public static let detailMin: Double = 340

    /// The window's edge, on each side.
    public static let edge: Double = 1
    /// From the sidebar's line to the main pane, and between the panes.
    public static let gap: Double = 9
    /// From the last pane to the window's edge.
    public static let trailing: Double = 8

    /// The widths the columns take in a window width wide, the main pane
    /// keeping at least detailMin: the inspector gives way first, then
    /// the sidebar, down to their least. nil is a column that's hidden.
    public static func fit(window width: Double, sidebar: Double?, inspector: Double?) -> (sidebar: Double?, inspector: Double?) {
        var s = sidebar.map { clamp($0, sidebarMin, sidebarMax) }
        var i = inspector.map { InspectorColumn.width(saved: $0) }
        var over = needs(sidebar: s, inspector: i, main: detailMin) - width
        if over > 0, let w = i {
            let keep = max(InspectorColumn.min, w - over)
            over -= w - keep
            i = keep
        }
        if over > 0, let w = s {
            s = max(sidebarMin, w - over)
        }
        return (s, i)
    }

    /// The window's width with these columns and a main pane main wide.
    public static func needs(sidebar: Double?, inspector: Double?, main: Double) -> Double {
        2 * edge + (sidebar ?? 0) + gap + main + (inspector.map { gap + $0 } ?? 0) + trailing
    }

    /// The least width the main window's columns need, all shown.
    public static var mainNeeds: Double {
        needs(sidebar: sidebarMin, inspector: InspectorColumn.min, main: detailMin)
    }

    /// The least width a session window's columns need, its inspector
    /// shown.
    public static var sessionNeeds: Double {
        needs(sidebar: nil, inspector: InspectorColumn.min, main: detailMin)
    }

    /// The sidebar's width as saved, or the ideal.
    public static func sidebar(saved: Double?) -> Double {
        saved.map { clamp($0, sidebarMin, sidebarMax) } ?? sidebarIdeal
    }

    /// The sidebar's lower section (Needs you) in a sidebar height high:
    /// the reference's 476 of its 1076, in whole points, so the line over
    /// it lands where the reference's does.
    public static func needsHeight(sidebar height: Double) -> Double {
        (height * 476 / 1076).rounded()
    }

    private static func clamp(_ v: Double, _ lo: Double, _ hi: Double) -> Double {
        Swift.min(hi, Swift.max(lo, v))
    }
}

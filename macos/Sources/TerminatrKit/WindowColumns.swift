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
    /// The sidebar's least and ideal width, its line included. There is
    /// no greatest: a drag stops where another visible panel would go
    /// under its least (limit).
    public static let sidebarMin: Double = 240
    public static let sidebarIdeal: Double = 296
    /// The main pane's least width: a session's conversation or
    /// terminal, or a project's dashboard.
    public static let detailMin: Double = 340

    /// The window's edge, on each side.
    public static let edge: Double = 1
    /// From the sidebar's line to the main pane, and between the panes.
    public static let gap: Double = 9
    /// From the last pane to the window's edge.
    public static let trailing: Double = 8

    /// A resizable column.
    public enum Column: Sendable { case sidebar, inspector }

    /// The widths the columns take in a window width wide, the main pane
    /// keeping at least detailMin. When they don't all fit, the inspector
    /// gives way first, then the sidebar, down to their least; the column
    /// named in keep (the one the user sized last) gives way last instead.
    /// The widths passed are the saved ones and stay so: a wider window
    /// gives the columns their saved widths back. nil is a hidden column.
    public static func fit(window width: Double, sidebar: Double?, inspector: Double?, keep: Column? = nil) -> (sidebar: Double?, inspector: Double?) {
        var s = sidebar.map { Swift.max(sidebarMin, $0) }
        var i = inspector.map { InspectorColumn.width(saved: $0) }
        var over = needs(sidebar: s, inspector: i, main: detailMin) - width
        let order: [Column] = keep == .sidebar ? [.inspector, .sidebar] : keep == .inspector ? [.sidebar, .inspector] : [.inspector, .sidebar]
        for c in order where over > 0 {
            switch c {
            case .inspector:
                if let w = i {
                    let to = Swift.max(InspectorColumn.min, w - over)
                    over -= w - to
                    i = to
                }
            case .sidebar:
                if let w = s {
                    let to = Swift.max(sidebarMin, w - over)
                    over -= w - to
                    s = to
                }
            }
        }
        return (s, i)
    }

    /// The most a column can be dragged to in a window width wide: the
    /// other visible columns at their least, the main pane at its least.
    /// Never under the column's own least.
    public static func limit(_ column: Column, window width: Double, sidebarShown: Bool, inspectorShown: Bool) -> Double {
        switch column {
        case .sidebar:
            let rest = needs(sidebar: nil, inspector: inspectorShown ? InspectorColumn.min : nil, main: detailMin)
            return Swift.max(sidebarMin, width - rest)
        case .inspector:
            let rest = needs(sidebar: sidebarShown ? sidebarMin : nil, inspector: nil, main: detailMin) + gap
            return Swift.max(InspectorColumn.min, width - rest)
        }
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
        saved.map { Swift.max(sidebarMin, $0) } ?? sidebarIdeal
    }

    /// The sidebar's lower section (Needs you) in a sidebar height high:
    /// the reference's 476 of its 1076, in whole points, so the line over
    /// it lands where the reference's does.
    public static func needsHeight(sidebar height: Double) -> Double {
        (height * 476 / 1076).rounded()
    }
}

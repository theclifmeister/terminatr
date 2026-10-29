import CoreGraphics

/// The font the phone's terminal draws its pane in (T840): the phone
/// never claims the pane's size, so the grid is fitted to the screen by
/// its font instead, the pane's width filling the room, between the
/// smallest size that reads and a comfortable one; a pinch scales that,
/// up to the largest, and the grid pans past the screen.
public enum PaneZoom {
    /// The smallest font size the grid is drawn in: a wide pane on a
    /// narrow screen pans from here.
    public static let smallest: CGFloat = 7
    /// The fitted size's top: a narrow pane on a wide screen isn't drawn
    /// huge.
    public static let fitted: CGFloat = 16
    /// The pinch's top.
    public static let largest: CGFloat = 28

    /// The font size for cols in room points, a cell advance points wide
    /// per point of font size, scaled by zoom: on half points.
    public static func size(fitting room: CGFloat, cols: Int, advance: CGFloat, zoom: CGFloat) -> CGFloat {
        let fit = fit(room: room, cols: cols, advance: advance)
        let s = min(largest, max(smallest, fit * zoom))
        return (s * 2 + 1e-6).rounded(.down) / 2
    }

    /// The size whose cols fill room, within smallest and fitted.
    public static func fit(room: CGFloat, cols: Int, advance: CGFloat) -> CGFloat {
        guard cols > 0, advance > 0, room > 0 else { return smallest }
        return min(fitted, max(smallest, room / (CGFloat(cols) * advance)))
    }

    /// A pinch's zoom, kept where the size it gives the fitted size fit
    /// stays within smallest and largest: further pinching does nothing.
    public static func clamp(zoom: CGFloat, fitted fit: CGFloat) -> CGFloat {
        guard fit > 0 else { return 1 }
        return min(largest / fit, max(smallest / fit, zoom))
    }
}

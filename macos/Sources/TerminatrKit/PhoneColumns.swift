import CoreGraphics

/// The phone's two columns side by side (T849): how wide the list is.
public enum PhoneColumns {
    /// The list's width when nothing else decides it.
    public static let listIdeal: CGFloat = 360
    /// The least either column may be beside a hinge: a SessionTile's
    /// and a conversation card's.
    public static let least: CGFloat = 240
    /// The least width that keeps a session beside the list (T855): the
    /// list's 360 and 440 for the conversation, its cards and composer.
    public static let sessionBeside: CGFloat = 800

    /// The list column's width in a space width wide, and the gap after
    /// it, with hinge the fold's region there (an open iPhone Duo's,
    /// while flat), nil without one. A hinge running top to bottom with
    /// room for a column each side ends the list where it begins and is
    /// the gap, so each column has a half of the display; else the list
    /// is listIdeal wide, half the width at most (an open Duo's 585 pt
    /// leaves the detail as much room as the list), and the gap a
    /// hairline.
    public static func split(_ width: CGFloat, hinge: CGRect?) -> (list: CGFloat, gap: CGFloat) {
        if let h = hinge, h.height > h.width, h.minX >= least, width - h.maxX >= least {
            return (h.minX, max(1, h.width))
        }
        return (min(listIdeal, width / 2), 1)
    }

    /// Whether a coordinator or thread open beside the list takes the
    /// whole width instead, the list folded away (T855), in a flat space
    /// this size: taller than wide (an open iPhone Duo or an iPad held
    /// in portrait), or narrower than sessionBeside (a narrow iPad
    /// window). Wide enough and wider than tall (a Duo or iPad in
    /// landscape) the list stays beside it. A partly folded Duo (book,
    /// laptop) isn't flat: there the fold decides.
    public static func foldsList(_ size: CGSize) -> Bool {
        size.width < size.height || size.width < sessionBeside
    }
}

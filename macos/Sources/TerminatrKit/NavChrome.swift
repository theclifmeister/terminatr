import CoreGraphics

/// Where the app's own way between Needs you, Projects and Search goes
/// (T1005), which replaces the system's tab bar, rail and sidebar.
public enum NavChrome {
    public enum Place: Equatable, Sendable {
        /// Along the bottom: a phone upright, a narrow iPad window.
        case bar
        /// Down the trailing edge: a phone on its side, and an open iPhone
        /// Duo either way up, where a bar would run across the hinge.
        case rail
        /// The iPad's sidebar, or the rail it folds to.
        case sidebar
    }

    /// The rail's width, as the design page's.
    public static let railWidth: CGFloat = 64
    /// The sidebar's width, as the design page's.
    public static let sidebarWidth: CGFloat = 296

    /// The sidebar's width at a text scale: its rows grow with their text.
    public static func sidebarWidth(scale: CGFloat) -> CGFloat {
        sidebarWidth * max(scale, 1)
    }

    public static func place(pad: Bool, regular: Bool, size: CGSize) -> Place {
        if pad { return regular ? .sidebar : .bar }
        return regular || size.width > size.height ? .rail : .bar
    }

    /// Whether the open sidebar lies over the content in a window this
    /// size rather than beside it: upright, or when beside it would leave
    /// the columns too little for a session beside its list.
    public static func sidebarOverlays(_ size: CGSize, sidebar: CGFloat = sidebarWidth) -> Bool {
        size.width < size.height || size.width - sidebar < PhoneColumns.sessionBeside
    }
}

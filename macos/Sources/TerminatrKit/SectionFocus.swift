/// The four parts of a window that can hold the keyboard. A session window
/// has no sidebar.
public enum WindowSection: Hashable, Sendable {
    case sidebar, main, files, inspector
}

/// Which section has the keyboard, and so which shows the focus border
/// (T1031): at most one, and only the main pane, the Files panel or the
/// Inspector. The sidebar holds the keyboard but never shows a border (the
/// user's choice), and with it there none of the others does. When nothing
/// holds the keyboard (a click on the title row, a tab that took its field
/// away) the last one that did counts until the keyboard comes back to it,
/// or the main pane if that section has gone.
public struct SectionFocus: Equatable, Sendable {
    /// The section with the keyboard, nil when none has.
    public var held: WindowSection?
    /// The last section that had it.
    public private(set) var last: WindowSection = .main
    /// The sections the window shows now.
    public var present: Set<WindowSection> = [.main]

    public init() {}

    public mutating func hold(_ section: WindowSection?) {
        held = section
        if let section { last = section }
    }

    /// Where the keyboard belongs when nothing holds it.
    public var home: WindowSection {
        present.contains(last) ? last : .main
    }

    /// The section to draw the border on: none in an inactive window, nor
    /// while the sidebar has the keyboard.
    public func border(windowHasKeys: Bool) -> WindowSection? {
        guard windowHasKeys else { return nil }
        let holder = held.flatMap { present.contains($0) ? $0 : nil } ?? home
        return holder == .sidebar ? nil : holder
    }
}

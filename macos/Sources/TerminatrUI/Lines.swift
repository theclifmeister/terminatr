import SwiftUI

extension View {
    /// At most n lines; at an accessibility text size (the phone's
    /// Dynamic Type) as many as the text needs, so a title or a line
    /// under it isn't cut off where a few words fill a row.
    public func lines(_ n: Int) -> some View {
        modifier(Lines(n: n))
    }
}

private struct Lines: ViewModifier {
    @Environment(\.dynamicTypeSize) private var typeSize
    let n: Int

    func body(content: Content) -> some View {
        if typeSize.isAccessibilitySize {
            content.lineLimit(nil).fixedSize(horizontal: false, vertical: true)
        } else {
            content.lineLimit(n)
        }
    }
}

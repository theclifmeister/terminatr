#if os(macOS)
import AppKit

/// What holds a window's keyboard, kept while something over the window has
/// it, to give back: the view itself, and where the text was selected when
/// that is a field (its field editor goes with the focus).
@MainActor
public struct KeyboardHolder {
    private weak var view: NSView?
    private let selection: NSRange?

    /// Nil when nothing holds the keyboard: the window or its root view has it.
    public init?(of window: NSWindow) {
        if let editor = window.firstResponder as? NSTextView, editor.isFieldEditor, let field = editor.delegate as? NSView {
            view = field
            selection = editor.selectedRange()
        } else if let v = window.firstResponder as? NSView, v !== window.contentView {
            view = v
            selection = nil
        } else {
            return nil
        }
    }

    /// False when the view has left the window or refuses it.
    @discardableResult
    public func giveBack(in window: NSWindow) -> Bool {
        guard let view, view.window === window, window.makeFirstResponder(view) else { return false }
        if let selection, let editor = (view as? NSControl)?.currentEditor() {
            let length = (editor.string as NSString).length
            let at = min(selection.location, length)
            editor.selectedRange = NSRange(location: at, length: min(selection.length, length - at))
        }
        return true
    }
}
#endif

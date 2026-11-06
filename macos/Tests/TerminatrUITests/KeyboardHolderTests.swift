#if os(macOS)
import AppKit
import Testing
@testable import TerminatrUI

// T1076: what the command palette took the keyboard from gets it back, in a
// real window. A field's editor goes with its focus, so the field is what's
// kept, and its selection with it.
@MainActor
struct KeyboardHolderTests {
    private func window(_ views: NSView...) -> NSWindow {
        let w = NSWindow(contentRect: NSRect(x: 0, y: 0, width: 300, height: 200), styleMask: [.titled], backing: .buffered, defer: false)
        w.isReleasedWhenClosed = false
        for v in views {
            v.frame = NSRect(x: 0, y: 0, width: 200, height: 40)
            w.contentView?.addSubview(v)
        }
        return w
    }

    @Test func aTextViewGetsItBack() {
        let text = NSTextView()
        let other = NSTextField()
        let w = window(text, other)
        #expect(w.makeFirstResponder(text))
        let held = KeyboardHolder(of: w)
        #expect(w.makeFirstResponder(other))
        let back = held?.giveBack(in: w)
        #expect(back == true)
        #expect(w.firstResponder === text)
    }

    @Test func aFieldGetsItBackWithItsSelection() {
        let field = NSTextField(string: "hello world")
        let other = NSTextField()
        let w = window(field, other)
        #expect(w.makeFirstResponder(field))
        field.currentEditor()?.selectedRange = NSRange(location: 2, length: 3)
        let held = KeyboardHolder(of: w)
        #expect(w.makeFirstResponder(other))
        #expect(held?.giveBack(in: w) == true)
        #expect(field.currentEditor() != nil)
        #expect(field.currentEditor()?.selectedRange == NSRange(location: 2, length: 3))
    }

    @Test func aViewThatLeftTheWindowDoesNot() {
        let text = NSTextView()
        let other = NSTextField()
        let w = window(text, other)
        #expect(w.makeFirstResponder(text))
        let held = KeyboardHolder(of: w)
        #expect(w.makeFirstResponder(other))
        text.removeFromSuperview()
        #expect(held?.giveBack(in: w) == false)
        #expect(w.firstResponder !== text)
    }

    @Test func nothingHoldingItKeepsNothing() {
        let w = window()
        w.makeFirstResponder(nil)
        #expect(KeyboardHolder(of: w) == nil)
        w.makeFirstResponder(w.contentView)
        #expect(KeyboardHolder(of: w) == nil)
    }
}
#endif

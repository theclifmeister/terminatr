import AppKit
import SwiftUI
import TerminatrKit
import TerminatrUI

/// A file's text, read only: selectable, unwrapped, with its line numbers.
/// AppKit's, for a file of a megabyte. New text for the same file keeps the
/// scroll: a file that changes while it's read doesn't lose the reader's
/// place.
struct CodeView: NSViewRepresentable {
    @Environment(\.theme) private var theme
    let id: String
    let text: String
    let points: CGFloat

    final class Coordinator {
        var id = ""
        var text: String?
        var look = ""
    }

    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeNSView(context: Context) -> NSScrollView {
        let storage = NSTextStorage()
        let layout = NSLayoutManager()
        layout.allowsNonContiguousLayout = true
        storage.addLayoutManager(layout)
        let box = NSTextContainer(size: NSSize(width: CGFloat.greatestFiniteMagnitude, height: CGFloat.greatestFiniteMagnitude))
        box.widthTracksTextView = false
        layout.addTextContainer(box)
        let v = NSTextView(frame: .zero, textContainer: box)
        v.isEditable = false
        v.isSelectable = true
        v.isRichText = false
        v.drawsBackground = false
        v.isHorizontallyResizable = true
        v.isVerticallyResizable = true
        v.maxSize = NSSize(width: CGFloat.greatestFiniteMagnitude, height: CGFloat.greatestFiniteMagnitude)
        v.autoresizingMask = []
        v.textContainerInset = NSSize(width: 8, height: 9)
        let s = NSScrollView()
        s.documentView = v
        s.drawsBackground = false
        s.hasVerticalScroller = true
        s.hasHorizontalScroller = true
        s.autohidesScrollers = true
        s.borderType = .noBorder
        s.verticalRulerView = LineNumbers(scrollView: s, text: v)
        s.hasVerticalRuler = true
        s.rulersVisible = true
        return s
    }

    func updateNSView(_ s: NSScrollView, context: Context) {
        guard let v = s.documentView as? NSTextView, let ruler = s.verticalRulerView as? LineNumbers else { return }
        let c = context.coordinator
        let size = points * theme.scale
        let look = "\(theme.name.rawValue) \(theme.contrast) \(size)"
        let sameFile = c.id == id
        guard c.text != text || !sameFile || c.look != look else { return }
        let keep = s.contentView.bounds.origin
        let font = NSFont.monospacedSystemFont(ofSize: size, weight: .regular)
        let lines = NSMutableParagraphStyle()
        lines.minimumLineHeight = ceil(size * 1.5)
        lines.maximumLineHeight = ceil(size * 1.5)
        lines.defaultTabInterval = 4 * ("0" as NSString).size(withAttributes: [.font: font]).width
        lines.tabStops = []
        v.textStorage?.setAttributedString(NSAttributedString(string: text, attributes: [.font: font, .foregroundColor: NSColor(theme.ink), .paragraphStyle: lines]))
        v.insertionPointColor = NSColor(theme.ink)
        v.selectedTextAttributes = [.backgroundColor: NSColor(theme.selection)]
        ruler.update(text: text, font: NSFont.monospacedDigitSystemFont(ofSize: size - 1, weight: .regular), ink: NSColor(theme.faint))
        v.sizeToFit()
        if sameFile {
            s.contentView.scroll(to: keep)
        } else {
            s.contentView.scroll(to: .zero)
        }
        s.reflectScrolledClipView(s.contentView)
        (c.id, c.text, c.look) = (id, text, look)
    }
}

/// The line numbers beside a CodeView.
private final class LineNumbers: NSRulerView {
    private weak var text: NSTextView?
    private var starts = [0]
    private var numberFont = NSFont.monospacedDigitSystemFont(ofSize: 11, weight: .regular)
    private var ink = NSColor.secondaryLabelColor

    init(scrollView: NSScrollView, text: NSTextView) {
        self.text = text
        super.init(scrollView: scrollView, orientation: .verticalRuler)
        clientView = text
        scrollView.contentView.postsBoundsChangedNotifications = true
        NotificationCenter.default.addObserver(self, selector: #selector(moved), name: NSView.boundsDidChangeNotification, object: scrollView.contentView)
    }

    @available(*, unavailable)
    required init(coder: NSCoder) { fatalError("not from a coder") }

    @objc private func moved() { needsDisplay = true }

    func update(text s: String, font: NSFont, ink: NSColor) {
        starts = FileText.lineStarts(s)
        (numberFont, self.ink) = (font, ink)
        let digits = max(String(starts.count).count, 2)
        ruleThickness = ceil(CGFloat(digits) * ("0" as NSString).size(withAttributes: [.font: font]).width) + 16
        needsDisplay = true
    }

    /// Only its numbers, on the pane's own ground.
    override var isOpaque: Bool { false }

    override func draw(_ dirtyRect: NSRect) {
        drawHashMarksAndLabels(in: dirtyRect)
    }

    override func drawHashMarksAndLabels(in rect: NSRect) {
        guard let text, let layout = text.layoutManager, let box = text.textContainer else { return }
        let visible = text.visibleRect
        let origin = text.textContainerOrigin
        let glyphs = layout.glyphRange(forBoundingRect: visible.offsetBy(dx: -origin.x, dy: -origin.y), in: box)
        let attrs: [NSAttributedString.Key: Any] = [.font: numberFont, .foregroundColor: ink]
        // On the line's own baseline.
        let draw = { [ruleThickness, numberFont] (n: Int, baseline: CGFloat) in
            let word = String(n) as NSString
            let size = word.size(withAttributes: attrs)
            let y = baseline + origin.y - visible.minY - numberFont.ascender
            word.draw(at: NSPoint(x: ruleThickness - size.width - 8, y: y), withAttributes: attrs)
        }
        var i = glyphs.location
        var last = -1
        var offset: CGFloat?
        while i < NSMaxRange(glyphs) {
            var range = NSRange()
            let fragment = layout.lineFragmentRect(forGlyphAt: i, effectiveRange: &range, withoutAdditionalLayout: false)
            let n = FileText.line(at: layout.characterIndexForGlyph(at: i), in: starts)
            // An empty line's newline has no baseline of its own: the
            // last line of text's offset gives it one.
            if range.length > 1 { offset = layout.location(forGlyphAt: i).y }
            if n != last { draw(n, fragment.minY + (offset ?? fragment.height + numberFont.descender)) }
            last = n
            i = NSMaxRange(range)
        }
    }
}

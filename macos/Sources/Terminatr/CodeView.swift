import AppKit
import SwiftUI
import TerminatrKit
import TerminatrUI

/// A file's text, read only: selectable, unwrapped, with its line numbers.
/// AppKit's, for a file of a megabyte. New text for the same file keeps the
/// scroll: a file that changes while it's read doesn't lose the reader's
/// place. With marks, each added and removed line has its colour across the
/// pane and its sign beside its number, and a file opens at its first change.
struct CodeView: NSViewRepresentable {
    @Environment(\.theme) private var theme
    let id: String
    let text: String
    let marks: FileMarks?
    let points: CGFloat

    final class Coordinator {
        var id = ""
        var text: String?
        var marks: FileMarks?
        var look = ""
    }

    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeNSView(context: Context) -> NSScrollView {
        let storage = NSTextStorage()
        let layout = TintedLayout()
        layout.allowsNonContiguousLayout = true
        storage.addLayoutManager(layout)
        let box = NSTextContainer(size: NSSize(width: CGFloat.greatestFiniteMagnitude, height: CGFloat.greatestFiniteMagnitude))
        box.widthTracksTextView = false
        layout.addTextContainer(box)
        let v = CodeText(frame: .zero, textContainer: box)
        v.isEditable = false
        v.isSelectable = true
        v.isRichText = false
        v.drawsBackground = false
        v.isHorizontallyResizable = true
        v.isVerticallyResizable = true
        v.maxSize = NSSize(width: CGFloat.greatestFiniteMagnitude, height: CGFloat.greatestFiniteMagnitude)
        v.autoresizingMask = []
        // The side's space is the container's own, not an inset: nothing
        // draws in an inset, and a changed line's colour has to reach the ruler.
        v.textContainerInset = NSSize(width: 0, height: 9)
        box.lineFragmentPadding = 13
        let s = CodeScroll()
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
        guard c.text != text || c.marks != marks || !sameFile || c.look != look else { return }
        let keep = s.contentView.bounds.origin
        let font = JetBrainsMono.font(size: size)
        let lines = NSMutableParagraphStyle()
        lines.minimumLineHeight = ceil(size * 1.5)
        lines.maximumLineHeight = ceil(size * 1.5)
        lines.defaultTabInterval = 4 * ("0" as NSString).size(withAttributes: [.font: font]).width
        lines.tabStops = []
        v.textStorage?.setAttributedString(NSAttributedString(string: text, attributes: [.font: font, .foregroundColor: NSColor(theme.ink), .paragraphStyle: lines]))
        v.insertionPointColor = NSColor(theme.ink)
        v.selectedTextAttributes = [.backgroundColor: NSColor(theme.selection)]
        let added = NSColor(theme.good), removed = NSColor(theme.attention)
        (v.layoutManager as? TintedLayout)?.update(text: text, marks: marks, added: added, removed: removed)
        ruler.update(text: text, marks: marks, added: added, removed: removed, font: JetBrainsMono.font(size: size - 1), ink: NSColor(theme.faint), ground: NSColor(theme.pane))
        let left = -ruler.requiredThickness
        s.contentView.scroll(to: NSPoint(x: sameFile && keep.x > 0 ? keep.x : left, y: keep.y))
        // Far down a file, the layout has to know how tall it is first.
        let seek = sameFile ? 0 : max((marks?.firstChange ?? 0) - 3, 0)
        if seek > 0, let box = v.textContainer { v.layoutManager?.ensureLayout(for: box) }
        v.sizeToFit()
        if sameFile {
            s.contentView.scroll(to: NSPoint(x: keep.x > 0 ? keep.x : left, y: keep.y))
        } else if seek > 0 {
            let top = CGFloat(seek) * ceil(size * 1.5) + v.textContainerInset.height
            s.contentView.scroll(to: NSPoint(x: left, y: min(top, max(v.frame.height - s.contentView.bounds.height, 0))))
        } else {
            s.contentView.scroll(to: NSPoint(x: left, y: 0))
        }
        s.reflectScrolledClipView(s.contentView)
        (c.id, c.text, c.marks, c.look) = (id, text, marks, look)
    }
}

/// A CodeView's scroll view. Its clip view runs under the ruler, so at rest its
/// left edge is the ruler's width left of the text, or the text starts under
/// the numbers. A first layout loses a scroll made before it, so each layout
/// puts it back, and the text fits the pane's new width.
private final class CodeScroll: NSScrollView {
    private var tiling = false

    override func tile() {
        super.tile()
        guard !tiling, let ruler = verticalRulerView else { return }
        tiling = true
        defer { tiling = false }
        let at = contentView.bounds.origin
        if at.x <= 0 { contentView.scroll(to: NSPoint(x: -ruler.requiredThickness, y: at.y)) }
        (documentView as? NSTextView)?.sizeToFit()
        reflectScrolledClipView(contentView)
    }
}

/// A CodeView's text: never narrower than what shows of it, so a changed
/// line's colour crosses the pane.
private final class CodeText: NSTextView {
    /// Stays at the top of its clip view: AppKit moves a text view by what its
    /// layout changed, and a file's first change then opens off its place.
    override func setFrameOrigin(_ newOrigin: NSPoint) {
        super.setFrameOrigin(.zero)
    }

    override func setFrameSize(_ newSize: NSSize) {
        // A clip view under the ruler starts left of the text, by the ruler.
        let shows = enclosingScrollView?.contentView.bounds ?? .zero
        super.setFrameSize(NSSize(width: max(newSize.width, shows.width + min(shows.minX, 0)), height: newSize.height))
        (layoutManager as? TintedLayout)?.width = frame.width
    }
}

/// How much of a changed line's colour shows, as the design's mock.
private let tint: CGFloat = 0.13

/// Draws the added and removed lines' colour under their text and selection.
private final class TintedLayout: NSLayoutManager {
    /// The text view's width: each changed line's colour crosses it.
    var width: CGFloat = 0
    private var starts = [0]
    private var kinds: [FileMarks.Kind] = []
    private var added = NSColor.clear
    private var removed = NSColor.clear

    func update(text: String, marks: FileMarks?, added: NSColor, removed: NSColor) {
        starts = marks == nil ? [0] : FileText.lineStarts(text)
        kinds = marks?.kinds ?? []
        self.added = added.withAlphaComponent(tint)
        self.removed = removed.withAlphaComponent(tint)
    }

    override func drawBackground(forGlyphRange glyphs: NSRange, at origin: NSPoint) {
        if !kinds.isEmpty {
            enumerateLineFragments(forGlyphRange: glyphs) { rect, _, _, range, _ in
                let row = FileText.line(at: self.characterIndexForGlyph(at: range.location), in: self.starts) - 1
                guard row < self.kinds.count, self.kinds[row] != .same else { return }
                (self.kinds[row] == .added ? self.added : self.removed).setFill()
                NSRect(x: 0, y: rect.minY + origin.y, width: self.width, height: rect.height).fill(using: .sourceOver)
            }
        }
        super.drawBackground(forGlyphRange: glyphs, at: origin)
    }
}

/// The line numbers beside a CodeView, with a changed line's sign and colour.
private final class LineNumbers: NSRulerView {
    private weak var text: NSTextView?
    private var starts = [0]
    private var numberFont = JetBrainsMono.font(size: 11)
    private var ink = NSColor.secondaryLabelColor
    private var marks: FileMarks?
    private var added = NSColor.clear
    private var removed = NSColor.clear
    private var ground = NSColor.clear

    init(scrollView: NSScrollView, text: NSTextView) {
        self.text = text
        super.init(scrollView: scrollView, orientation: .verticalRuler)
        clientView = text
        // Part of a line's number at its top or bottom stays inside.
        clipsToBounds = true
        scrollView.contentView.postsBoundsChangedNotifications = true
        NotificationCenter.default.addObserver(self, selector: #selector(moved), name: NSView.boundsDidChangeNotification, object: scrollView.contentView)
    }

    @available(*, unavailable)
    required init(coder: NSCoder) { fatalError("not from a coder") }

    @objc private func moved() { needsDisplay = true }

    func update(text s: String, marks: FileMarks?, added: NSColor, removed: NSColor, font: NSFont, ink: NSColor, ground: NSColor) {
        starts = FileText.lineStarts(s)
        (self.marks, self.added, self.removed) = (marks, added, removed)
        (numberFont, self.ink, self.ground) = (font, ink, ground)
        let digits = max(String(marks?.numbers.max() ?? starts.count).count, 2)
        let digit = ("0" as NSString).size(withAttributes: [.font: font]).width
        ruleThickness = ceil(CGFloat(digits) * digit) + 16 + (marks == nil ? 0 : ceil(digit) + 4)
        needsDisplay = true
    }

    /// On the pane's own ground: text scrolled sideways passes under it.
    override func draw(_ dirtyRect: NSRect) {
        ground.setFill()
        // A dirty rect can reach past the view since macOS 14.
        dirtyRect.intersection(bounds).fill()
        drawHashMarksAndLabels(in: dirtyRect)
    }

    override func drawHashMarksAndLabels(in rect: NSRect) {
        guard let text, let layout = text.layoutManager, let box = text.textContainer else { return }
        let visible = text.visibleRect
        let origin = text.textContainerOrigin
        let glyphs = layout.glyphRange(forBoundingRect: visible.offsetBy(dx: -origin.x, dy: -origin.y), in: box)
        let attrs: [NSAttributedString.Key: Any] = [.font: numberFont, .foregroundColor: ink]
        // On the line's own baseline.
        let draw = { [ruleThickness, numberFont, marks, added, removed] (n: Int, baseline: CGFloat, fragment: NSRect) in
            let y = baseline + origin.y - visible.minY - numberFont.ascender
            let kind = marks.flatMap { n <= $0.kinds.count ? $0.kinds[n - 1] : nil } ?? .same
            if kind != .same {
                let colour = kind == .added ? added : removed
                colour.withAlphaComponent(tint).setFill()
                NSRect(x: 0, y: fragment.minY + origin.y - visible.minY, width: ruleThickness, height: fragment.height).fill(using: .sourceOver)
                let sign = (kind == .added ? "+" : "−") as NSString
                sign.draw(at: NSPoint(x: 5, y: y), withAttributes: [.font: numberFont, .foregroundColor: colour])
            }
            let word = String(marks.flatMap { n <= $0.numbers.count ? $0.numbers[n - 1] : nil } ?? n) as NSString
            let size = word.size(withAttributes: attrs)
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
            if n != last { draw(n, fragment.minY + (offset ?? fragment.height + numberFont.descender), fragment) }
            last = n
            i = NSMaxRange(range)
        }
    }
}

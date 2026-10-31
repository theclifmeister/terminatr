import SwiftUI
import TerminatrKit
import TerminatrUI
import UniformTypeIdentifiers

/// The composer's text (T893): a UITextView, so an image pasted goes in
/// as a file whatever pastes it: the keyboard's suggestion ("Photo ·
/// Paste from Screenshots"), the edit menu's Paste, ⌘V, or a drop. A
/// SwiftUI TextField pastes text alone and lets those fall (its
/// pasteDestination misses a pasteboard of images only). Text pastes as
/// in any field, and a pasteboard with text and an image pastes its
/// text. It grows to six lines, then scrolls; the placeholder shows
/// while it is empty. Return makes a new line; Command-Return is the
/// send button's. No Writing Tools: a prompt goes as typed.
struct ComposerField: UIViewRepresentable {
    @Environment(\.theme) private var theme
    @Binding var text: String
    @Binding var focused: Bool
    let placeholder: String
    /// Where files pasted or dropped go; nil where the composer takes
    /// none (a paired server's session).
    let files: ((_ providers: [NSItemProvider]) -> Void)?

    static let lines = 6

    func makeUIView(context: Context) -> FieldView {
        let v = FieldView()
        v.delegate = context.coordinator
        v.backgroundColor = .clear
        v.textContainerInset = UIEdgeInsets(top: Spacing.row, left: 0, bottom: Spacing.row, right: 0)
        v.textContainer.lineFragmentPadding = 0
        v.isScrollEnabled = false
        v.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        if #available(iOS 18, *) { v.writingToolsBehavior = .none }
        v.text = text
        return v
    }

    func updateUIView(_ v: FieldView, context: Context) {
        context.coordinator.parent = self
        let font = UIFont.systemFont(ofSize: theme.size(.body))
        if v.font != font { v.font = font }
        v.textColor = UIColor(theme.ink)
        v.tintColor = UIColor(theme.action)
        v.placeholder.text = placeholder
        v.placeholder.font = font
        v.placeholder.textColor = UIColor(theme.muted)
        v.accessibilityLabel = placeholder
        v.files = files
        if v.text != text {
            v.text = text
            v.placeholder.isHidden = !text.isEmpty
        }
        v.placeholder.isHidden = !v.text.isEmpty
        v.wantsFocus = focused
        if focused, !v.isFirstResponder {
            // Not in a window yet: didMoveToWindow takes it.
            DispatchQueue.main.async { if v.window != nil, v.wantsFocus { v.becomeFirstResponder() } }
        } else if !focused, v.isFirstResponder {
            DispatchQueue.main.async { v.resignFirstResponder() }
        }
    }

    /// As tall as its text, from one line to lines, at the width given.
    func sizeThatFits(_ proposal: ProposedViewSize, uiView v: FieldView, context: Context) -> CGSize? {
        let width = proposal.width ?? 240
        let fit = v.sizeThatFits(CGSize(width: width, height: .greatestFiniteMagnitude))
        let line = (v.font ?? .preferredFont(forTextStyle: .body)).lineHeight
        let most = ceil(line * CGFloat(Self.lines) + v.textContainerInset.top + v.textContainerInset.bottom)
        let scrolls = fit.height > most
        if v.isScrollEnabled != scrolls { v.isScrollEnabled = scrolls }
        return CGSize(width: width, height: min(fit.height, most))
    }

    func makeCoordinator() -> Coordinator { Coordinator(parent: self) }

    final class Coordinator: NSObject, UITextViewDelegate {
        var parent: ComposerField

        init(parent: ComposerField) { self.parent = parent }

        func textViewDidChange(_ v: UITextView) {
            parent.text = v.text
            (v as? FieldView)?.placeholder.isHidden = !v.text.isEmpty
        }

        func textViewDidBeginEditing(_ v: UITextView) {
            if !parent.focused { parent.focused = true }
        }

        func textViewDidEndEditing(_ v: UITextView) {
            if parent.focused { parent.focused = false }
        }
    }

    /// The text view: pastes an image or a file to files.
    final class FieldView: UITextView {
        let placeholder = UILabel()
        /// The composer asks for the keyboard: taken once in a window.
        var wantsFocus = false
        var files: (([NSItemProvider]) -> Void)? {
            didSet { pasteConfiguration = files == nil ? nil : Self.accepted }
        }

        /// Text and the files the composer takes, so the keyboard and a
        /// drop offer an image here.
        private static let accepted = UIPasteConfiguration(acceptableTypeIdentifiers:
            [UTType.plainText, .text, .url].map(\.identifier) + Incoming.types.map(\.identifier))

        override init(frame: CGRect, textContainer: NSTextContainer?) {
            super.init(frame: frame, textContainer: textContainer)
            placeholder.numberOfLines = 1
            placeholder.isAccessibilityElement = false
            placeholder.translatesAutoresizingMaskIntoConstraints = false
            addSubview(placeholder)
            NSLayoutConstraint.activate([
                placeholder.leadingAnchor.constraint(equalTo: leadingAnchor),
                placeholder.widthAnchor.constraint(lessThanOrEqualTo: widthAnchor),
                placeholder.topAnchor.constraint(equalTo: topAnchor, constant: Spacing.row),
            ])
        }

        required init?(coder: NSCoder) { fatalError("not from a coder") }

        override func didMoveToWindow() {
            super.didMoveToWindow()
            if wantsFocus, window != nil, !isFirstResponder {
                DispatchQueue.main.async { if self.wantsFocus, self.window != nil { self.becomeFirstResponder() } }
            }
        }

        /// The pasteboard has a file to take and no text to paste.
        private var pastesFiles: Bool {
            guard files != nil else { return false }
            let pb = UIPasteboard.general
            return !pb.hasStrings && !pb.hasURLs && PasteFiles.check()
        }

        override func canPerformAction(_ action: Selector, withSender sender: Any?) -> Bool {
            if action == #selector(paste(_:)), pastesFiles { return true }
            return super.canPerformAction(action, withSender: sender)
        }

        override func paste(_ sender: Any?) {
            if pastesFiles, let files {
                files(UIPasteboard.general.itemProviders)
                return
            }
            super.paste(sender)
        }

        /// A paste or a drop through the paste configuration: files go to
        /// the tray, text into the field.
        override func paste(itemProviders: [NSItemProvider]) {
            guard let files else { return super.paste(itemProviders: itemProviders) }
            let text = itemProviders.contains { p in
                p.hasItemConformingToTypeIdentifier(UTType.plainText.identifier) || p.hasItemConformingToTypeIdentifier(UTType.url.identifier)
            }
            let taken = itemProviders.filter { p in
                p.registeredTypeIdentifiers.contains { id in
                    UTType(id).map { t in Incoming.types.contains { t.conforms(to: $0) } } ?? false
                }
            }
            if text || taken.isEmpty { return super.paste(itemProviders: itemProviders) }
            files(taken)
        }
    }
}

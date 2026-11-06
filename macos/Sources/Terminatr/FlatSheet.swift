import AppKit
import SwiftUI
import TerminatrKit
import TerminatrUI

/// Every sheet of the window (T1035): the flat look's pane, 8 round in the
/// theme's line, a header, a hairline, what the sheet says, a hairline and
/// a footer of its buttons, which are the panes' small ones.
///
/// It stays SwiftUI's .sheet, whose window holds the keys, Esc, the default
/// button, the focus and VoiceOver's AXSheet. What it took away is what
/// that window paints: an NSVisualEffectView (material .sheet, behind the
/// window) that the desktop's colours tint, and corners the system cuts at
/// a far larger radius. presentationBackground covers the first but not the
/// second; a style mask without .titled makes the window unable to take the
/// keys. So the window goes clear (SheetWindowClearer) and the pane stands
/// margin inside it, where the system's corners never reach; the window's
/// shadow follows the pane.
struct FlatSheet<Header: View, Content: View, Footer: View>: View {
    @Environment(\.theme) private var theme
    /// Its fixed width; nil when its content's frame sets it.
    var width: CGFloat?
    @ViewBuilder var header: Header
    @ViewBuilder var content: Content
    @ViewBuilder var footer: Footer

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: Radius.tile)
        VStack(alignment: .leading, spacing: 0) {
            header
                .padding(.horizontal, 16)
                .padding(.vertical, 12)
                .frame(maxWidth: .infinity, alignment: .leading)
            theme.line.frame(height: 1)
            content
            theme.line.frame(height: 1)
            HStack(spacing: 8) { footer }
                .padding(.horizontal, 16)
                .padding(.vertical, 10)
                .frame(maxWidth: .infinity, alignment: .trailing)
        }
        .frame(width: width)
        .font(theme.sized(Flat.text))
        .foregroundStyle(theme.ink)
        .tint(theme.action)
        .buttonStyle(.flat)
        .menuStyle(.button)
        .background(theme.pane)
        .clipShape(shape)
        .overlay { shape.strokeBorder(theme.line, lineWidth: theme.contrast ? 1.5 : 1).allowsHitTesting(false) }
        .padding(FlatSheetMargin.points)
        .presentationBackground(.clear)
        .background { SheetWindowClearer().accessibilityHidden(true) }
    }
}

/// How far the pane stands inside the sheet's clear window: past where the
/// system's corners (about 22 round on macOS 27) would cut its own 8.
enum FlatSheetMargin {
    static let points: CGFloat = 10
}

/// A sheet's window with nothing of its own showing: not opaque, no colour
/// and its effect view hidden. The frame view shows that view again as the
/// window comes up and changes key, so this looks again each time.
private struct SheetWindowClearer: NSViewRepresentable {
    func makeNSView(context: Context) -> NSView { Clearer() }
    func updateNSView(_ nsView: NSView, context: Context) {}

    private final class Clearer: NSView {
        private var watching: [NSObjectProtocol] = []

        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            watching.forEach(NotificationCenter.default.removeObserver)
            watching = []
            guard let window else { return }
            clear()
            let again: @Sendable (Notification) -> Void = { [weak self] _ in
                DispatchQueue.main.async { MainActor.assumeIsolated { self?.clear() } }
            }
            for name in [NSWindow.didBecomeKeyNotification, NSWindow.didResignKeyNotification, NSWindow.didBecomeMainNotification,
                         NSWindow.didResizeNotification, NSWindow.didChangeOcclusionStateNotification] {
                watching.append(NotificationCenter.default.addObserver(forName: name, object: window, queue: .main, using: again))
            }
            DispatchQueue.main.async { [weak self] in self?.clear() }
        }

        private func clear() {
            guard let window else { return }
            window.isOpaque = false
            window.backgroundColor = .clear
            for v in window.contentView?.superview?.subviews ?? [] where v is NSVisualEffectView {
                v.isHidden = true
            }
        }
    }
}

/// A sheet's header: its title, at the panes' heading size, and what it
/// says under it.
struct SheetTitle: View {
    @Environment(\.theme) private var theme
    let title: String
    var detail: String?

    init(_ title: String, detail: String? = nil) {
        self.title = title
        self.detail = detail
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            Text(title)
                .font(theme.title(.headline, .semibold))
                .fixedSize(horizontal: false, vertical: true)
                .accessibilityAddTraits(.isHeader)
            if let detail {
                Text(detail)
                    .font(theme.sized(Flat.label))
                    .foregroundStyle(theme.muted)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }
}

/// What a sheet says between its hairlines: 16 inside, 12 between.
struct SheetBody<Content: View>: View {
    @ViewBuilder var content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            content
        }
        .padding(16)
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// A form's field: its label over the control. VoiceOver hears the
/// control's own label, not this one again.
struct SheetField<Control: View>: View {
    @Environment(\.theme) private var theme
    let label: String
    @ViewBuilder var control: Control

    init(_ label: String, @ViewBuilder control: () -> Control) {
        self.label = label
        self.control = control()
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            Text(label)
                .font(theme.sized(Flat.label))
                .foregroundStyle(theme.muted)
                .accessibilityHidden(true)
            control
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// A picker as the panes' small button and its menu, instead of the
/// system's pop-up button. A control of its own to VoiceOver: its name and
/// what's picked.
struct SheetPicker<Value: Hashable, Options: View>: View {
    let label: String
    /// The picked option's words.
    let shown: String
    @Binding var selection: Value
    @ViewBuilder var options: Options

    var body: some View {
        Menu {
            Picker(label, selection: $selection) { options }
                .pickerStyle(.inline)
                .labelsHidden()
        } label: {
            HStack(spacing: 6) {
                Text(shown).lineLimit(1)
                Image(systemName: "chevron.up.chevron.down")
                    .font(.system(size: 9, weight: .medium))
                    .accessibilityHidden(true)
            }
        }
        .fixedSize()
        .accessibilityLabel(label)
        .accessibilityValue(shown)
    }
}

/// Esc for a sheet with no Cancel, Close or Done on it to say so: nothing
/// to see or hear, the footer holds it.
struct SheetEscape: View {
    let close: () -> Void

    var body: some View {
        Button("", action: close)
            .keyboardShortcut(.cancelAction)
            .opacity(0)
            .frame(width: 0, height: 0)
            .accessibilityHidden(true)
    }
}

extension View {
    /// A text field or editor in a sheet: plain, 6 round in the line, the
    /// focus colour round it while it has the keys.
    func sheetField() -> some View {
        modifier(SheetFieldFrame())
    }
}

private struct SheetFieldFrame: ViewModifier {
    @Environment(\.theme) private var theme
    @FocusState private var focused: Bool

    func body(content: Content) -> some View {
        let shape = RoundedRectangle(cornerRadius: Radius.row)
        content
            .textFieldStyle(.plain)
            .font(theme.sized(Flat.text))
            .focused($focused)
            .focusEffectDisabled()
            .padding(.horizontal, 8)
            .padding(.vertical, 6)
            .background(theme.pane, in: shape)
            .overlay { shape.strokeBorder(focused ? theme.focus : theme.line, lineWidth: 1).allowsHitTesting(false) }
    }
}

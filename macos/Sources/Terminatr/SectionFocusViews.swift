import AppKit
import SwiftUI
import TerminatrKit
import TerminatrUI

private struct WindowSectionKey: FocusedValueKey {
    typealias Value = WindowSection
}

extension FocusedValues {
    /// The section around the focused view: SwiftUI's own focus chain.
    fileprivate var windowSection: WindowSection? {
        get { self[WindowSectionKey.self] }
        set { self[WindowSectionKey.self] = newValue }
    }
}

/// A window's one owner of which section has the focus: fed by SwiftUI's
/// focus chain and the window's clicks, read by every section's border.
@MainActor @Observable
final class SectionFocusOwner {
    private(set) var focus = SectionFocus()
    var windowHasKeys = false
    /// Something over the sections has the keyboard (the command palette):
    /// clicks there aren't a section's, and nothing is taken from it.
    var paused = false
    /// A section asked to take the keyboard; its id changes with each ask.
    private(set) var ask: (section: WindowSection, id: Int)?
    /// What had the keyboard when the palette took it.
    @ObservationIgnored private var covered: KeyboardHolder?
    /// The palette is closing on a field that takes the keyboard (handOff).
    @ObservationIgnored private var handedOff = false
    @ObservationIgnored fileprivate var marks: [WindowSection: WeakView] = [:]
    @ObservationIgnored fileprivate weak var window: NSWindow?

    var border: WindowSection? { focus.border(windowHasKeys: windowHasKeys) }

    func hold(_ section: WindowSection?) { focus.hold(section) }

    private func want(_ section: WindowSection) {
        ask = (section, (ask?.id ?? 0) + 1)
    }

    /// The palette is taking the keyboard: what has it gets it back when the
    /// palette closes (T1076). The section alone wouldn't do: the terminal,
    /// the composer and the transcript sit in the main pane beside its anchor.
    func cover() {
        covered = window.flatMap { KeyboardHolder(of: $0) }
    }

    /// The palette closes on something that takes the keyboard itself.
    func forget() { covered = nil }

    /// It may not have it yet as the palette goes: the rehome of that close
    /// would send the keyboard home ahead of it.
    func handOff() {
        forget()
        handedOff = true
    }

    /// After the pass that takes the palette's field away. What has gone too
    /// is left to rehome.
    fileprivate func uncover() {
        guard let held = covered else { return }
        covered = nil
        DispatchQueue.main.async { [weak self] in
            MainActor.assumeIsolated {
                if let window = self?.window { held.giveBack(in: window) }
            }
        }
    }

    /// When what had the keyboard has gone and the window or its root view
    /// has it, where it belongs takes it back, so the border never says a
    /// section holds keys that reach nothing.
    func rehome() {
        if handedOff {
            handedOff = false
            return
        }
        DispatchQueue.main.async { [weak self] in
            MainActor.assumeIsolated {
                guard let self, let window = self.window, !self.paused, self.windowHasKeys,
                      self.focus.held == nil, window.firstResponder === window || window.firstResponder === window.contentView else { return }
                self.want(self.focus.home)
            }
        }
    }

    /// A click in a section gives it the keyboard unless what it landed on
    /// took it: the terminal, a text field.
    fileprivate func clicked(_ section: WindowSection) {
        guard !paused else { return }
        DispatchQueue.main.async { [weak self] in
            MainActor.assumeIsolated {
                // The click may have opened the palette by now.
                guard let self, !self.paused, self.focus.held != section, !self.responderIsIn(section) else { return }
                self.want(section)
            }
        }
    }

    /// AppKit's own views hold the keyboard at their place; SwiftUI's
    /// proxy for its focusable views is elsewhere, and doesn't match.
    private func responderIsIn(_ section: WindowSection) -> Bool {
        guard let v = window?.firstResponder as? NSView, v.window != nil, let mark = marks[section]?.view else { return false }
        let at = v.convert(NSPoint(x: v.bounds.midX, y: v.bounds.midY), to: nil)
        return v !== window?.contentView && mark.bounds.contains(mark.convert(at, from: nil))
    }

    fileprivate func mark(_ section: WindowSection, _ view: NSView, shown: Bool) {
        if shown {
            marks[section] = WeakView(view)
        } else if marks[section]?.view === view {
            // Not a newer view's: SwiftUI adds that before removing this.
            marks[section] = nil
        }
        let present = Set(marks.filter { $0.value.view != nil }.keys)
        if focus.present != present { focus.present = present }
    }

    /// The section under a point in window coordinates.
    fileprivate func section(at point: NSPoint) -> WindowSection? {
        marks.first { _, m in
            guard let v = m.view else { return false }
            return v.bounds.contains(v.convert(point, from: nil))
        }?.key
    }
}

fileprivate struct WeakView {
    weak var view: NSView?
    init(_ view: NSView) { self.view = view }
}

extension View {
    /// Makes this view a window's root: gives it the owner of its sections'
    /// focus.
    func ownsSectionFocus(paused: Bool = false) -> some View { modifier(SectionFocusRoot(paused: paused)) }

    /// Marks this view as a section. With `anchor` it can take the keyboard
    /// itself, for a section with nothing else that does; else it has its
    /// own focusable view, which answers the owner's ask.
    func windowSection(_ section: WindowSection, anchor: Bool = false) -> some View {
        modifier(SectionMark(section: section, anchor: anchor))
    }
}

private struct SectionFocusRoot: ViewModifier {
    @Environment(\.controlActiveState) private var active
    @FocusedValue(\.windowSection) private var focused
    @State private var owner = SectionFocusOwner()
    let paused: Bool

    func body(content: Content) -> some View {
        content
            .environment(owner)
            .background { ClickWatcher(owner: owner) }
            .onChange(of: focused, initial: true) { _, s in owner.hold(s) }
            .onChange(of: active, initial: true) { _, a in
                owner.windowHasKeys = a == .key
                owner.rehome()
            }
            .onChange(of: paused, initial: true) { _, p in
                owner.paused = p
                if !p { owner.uncover() }
                owner.rehome()
            }
    }
}

private struct SectionMark: ViewModifier {
    @Environment(SectionFocusOwner.self) private var owner
    @FocusState private var held: Bool
    let section: WindowSection
    let anchor: Bool

    func body(content: Content) -> some View {
        content
            .background {
                if anchor {
                    // Behind the content, not around it: views inside a
                    // focused one read as focused and draw their rings.
                    Color.clear
                        .focusable()
                        .focusEffectDisabled()
                        .focused($held)
                        .onChange(of: owner.ask?.id) {
                            if owner.ask?.section == section { held = true }
                        }
                        .accessibilityHidden(true)
                }
            }
            .focusedValue(\.windowSection, section)
            .background { MarkView(owner: owner, section: section).allowsHitTesting(false) }
    }
}

/// Where a section is, for the clicks, and that the window shows it.
private struct MarkView: NSViewRepresentable {
    let owner: SectionFocusOwner
    let section: WindowSection

    func makeNSView(context: Context) -> NSView { Mark(owner: owner, section: section) }
    func updateNSView(_ nsView: NSView, context: Context) {}

    private final class Mark: NSView {
        let owner: SectionFocusOwner
        let section: WindowSection

        init(owner: SectionFocusOwner, section: WindowSection) {
            self.owner = owner
            self.section = section
            super.init(frame: .zero)
        }

        @available(*, unavailable)
        required init?(coder: NSCoder) { fatalError() }

        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            owner.mark(section, self, shown: window != nil)
        }
    }
}

/// A click in a section gives it the keyboard, whatever it lands on: a
/// button, a label or empty ground take none themselves. Also watches the
/// window's first responder, for when nothing holds the keyboard.
private struct ClickWatcher: NSViewRepresentable {
    let owner: SectionFocusOwner

    func makeNSView(context: Context) -> NSView { Watcher(owner: owner) }
    func updateNSView(_ nsView: NSView, context: Context) {}

    private final class Watcher: NSView {
        let owner: SectionFocusOwner
        private var monitor: Any?
        private var responder: NSKeyValueObservation?

        init(owner: SectionFocusOwner) {
            self.owner = owner
            super.init(frame: .zero)
        }

        @available(*, unavailable)
        required init?(coder: NSCoder) { fatalError() }

        override func viewDidMoveToWindow() {
            super.viewDidMoveToWindow()
            if let monitor { NSEvent.removeMonitor(monitor) }
            monitor = nil
            owner.window = window
            responder = nil
            guard let window else { return }
            responder = window.observe(\.firstResponder) { [owner] _, _ in
                DispatchQueue.main.async { MainActor.assumeIsolated { owner.rehome() } }
            }
            monitor = NSEvent.addLocalMonitorForEvents(matching: .leftMouseDown) { [weak self] e in
                if let self, e.window === self.window, let s = self.owner.section(at: e.locationInWindow) {
                    self.owner.clicked(s)
                }
                return e
            }
        }
    }
}

extension View {
    /// The focus line and its soft glow, while this section has the keyboard.
    func focusBorder(_ section: WindowSection) -> some View {
        modifier(FocusBorder(section: section))
    }
}

private struct FocusBorder: ViewModifier {
    @Environment(SectionFocusOwner.self) private var owner
    @Environment(\.theme) private var theme
    let section: WindowSection

    func body(content: Content) -> some View {
        let shape = RoundedRectangle(cornerRadius: 8)
        content.overlay {
            if owner.border == section {
                ZStack {
                    // The glow inside the line, kept to the section.
                    shape.strokeBorder(theme.focus.opacity(0.1), lineWidth: 2)
                        .blur(radius: 0.5)
                        .clipShape(shape)
                    // And outside it.
                    shape.stroke(theme.focus.opacity(0.1), lineWidth: 1)
                        .blur(radius: 0.5)
                    shape.strokeBorder(theme.focus, lineWidth: 1)
                }
                .allowsHitTesting(false)
                .accessibilityHidden(true)
            }
        }
    }
}

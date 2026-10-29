import AppKit
import SwiftTerm
import SwiftUI
import TerminatrKit
import TerminatrPane
import TerminatrUI

/// The Terminal tab: the session's pane through pane.vt, drawn by
/// SwiftTerm (PaneController, shared with the phone's). The server's
/// emulator stays the pane's; this one mirrors it at the pane's size
/// and never answers the program. Keys go back
/// through pane.input as the human's: typed text takes a thread over.
/// The tab claims the pane's size for its room when it opens, when the
/// room changes and settles, and on keys (PaneFit). A paired
/// server's pane ("<alias>/<session>") comes the same way through the
/// server's peer link, under that server's switch: it opens at view or
/// write, and takes keys at write alone (the far server checks each).
struct TerminalTab: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let model: SessionModel
    let session: AppAPI.Session?
    @State private var pane = MacPane()

    /// The terminal box's corners, which the session's notice above it
    /// shares; its edges, and the view-only note's sides, are the pages'
    /// (Page.edge).
    static let corner: CGFloat = 12

    var body: some View {
        let remote = model.ref.isRemote ? app.remotePane(model.ref) : nil
        VStack(spacing: 0) {
            if !app.caps.contains("pane-vt") {
                Text("This tm server doesn't stream panes to the app; tm update and tm server restart bring it.")
                    .foregroundStyle(theme.muted)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if case .closed(let why) = remote {
                Text(why)
                    .foregroundStyle(theme.muted)
                    .multilineTextAlignment(.center)
                    .padding(28)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else if let id = model.sessionID {
                // While the bridge is down the pane keeps its last
                // screen (the window's banner says why) until the next.
                ZStack(alignment: .bottom) {
                    PaneTerminal(host: pane.host, theme: theme)
                        .task(id: PaneKey(session: id, bridge: app.bridge.map(ObjectIdentifier.init))) {
                            if let bridge = app.bridge { await pane.controller.run(bridge, id) }
                        }
                        .onChange(of: remote, initial: true) { _, r in pane.controller.keys = r != .view }
                        .onChange(of: model.repaints) { pane.controller.repaint() }
                    if let closed = pane.controller.closed, app.bridge != nil {
                        Text(closed)
                            .padding(10)
                            .frosted(RoundedRectangle(cornerRadius: 10))
                            .padding(16)
                    }
                }
                .background(theme.terminalBackground)
                .clipShape(RoundedRectangle(cornerRadius: Self.corner))
                // Below the title bar, the conversation's top margin
                // (T800); the room it takes is claimed off the pane.
                .padding(.horizontal, Page.edge)
                .padding(.top, Page.edge)
                .padding(.bottom, remote == .view ? 0 : Page.edge)
                if remote == .view { viewOnly }
            }
        }
        .guardingLinks()
    }

    /// At `view`, the one note the tab has: typing there does nothing.
    /// Repaint is in the terminal's context menu and View › Repaint
    /// Terminal (⌥⌘R).
    private var viewOnly: some View {
        Text("View only: \(model.ref.server) lets this server watch its panes, not type into them.")
            .font(theme.body(.footnote))
            .foregroundStyle(theme.muted)
            .frame(maxWidth: .infinity, alignment: .leading)
            .padding(.horizontal, Page.edge)
            .padding(.vertical, 8)
    }
}

/// What the pane's stream runs for: a session on one bridge.
private struct PaneKey: Equatable {
    let session: String
    let bridge: ObjectIdentifier?
}

/// The tab's pane: the shared stream (PaneController, TerminatrPane)
/// into the mirror PaneHost pins at the top left, claiming the tab's
/// size as PaneFit says.
@MainActor
@Observable
final class MacPane {
    @ObservationIgnored let host = PaneHost()
    let controller: PaneController

    init() {
        let host = host
        controller = PaneController(terminal: host.terminal)
        controller.fitting = { host.fitting }
        controller.mirror = { cols, rows in host.mirror(cols: cols, rows: rows) }
        controller.openLink = { LinkConfirm.open($0) }
        host.terminal.onRepaint = { [weak controller] in controller?.repaint() }
        host.roomChanged = { [weak controller] in controller?.roomChanged() }
    }
}

/// A link clicked in the terminal: shown with its full URL first, and
/// opened only for http, https and mailto.
enum LinkConfirm {
    @MainActor
    static func open(_ link: String) {
        let a = NSAlert()
        a.messageText = "Open this link?"
        guard let url = URL(string: link), SafeMarkdown.allowed(url) else {
            a.informativeText = "\(link)\n\nOnly web and mail links open from the app."
            a.addButton(withTitle: "OK")
            a.runModal()
            return
        }
        a.informativeText = url.absoluteString
        a.addButton(withTitle: "Open")
        a.addButton(withTitle: "Cancel")
        if a.runModal() == .alertFirstButtonReturn {
            NSWorkspace.shared.open(url)
        }
    }
}

/// The mirror, pinned to the pane's size at the top left of whatever
/// room the window gives it.
final class PaneHost: NSView {
    let terminal = MirrorTerminalView(frame: .zero, font: NSFont.monospacedSystemFont(ofSize: 12.5, weight: .regular))

    override init(frame: NSRect) {
        super.init(frame: frame)
        terminal.changeScrollback(PaneScroll.lines)
        addSubview(terminal)
    }

    required init?(coder: NSCoder) { fatalError("not from a nib") }

    override var isFlipped: Bool { true }

    /// Room between the cells and the tab's rounded edge.
    static let inset = CGSize(width: 10, height: 8)

    /// Called when the view's size changed, not on every layout.
    var roomChanged: (() -> Void)?
    private var laidOut: CGSize?

    override func layout() {
        super.layout()
        let room = self.room
        terminal.setFrameSize(room)
        // A fitted pane's cells leave less than a cell of the room: split
        // it evenly around them.
        let cell = terminal.cell
        let dx = PaneFit.centre(room: room.width, used: terminal.frame.width, cell: cell.width)
        let dy = PaneFit.centre(room: room.height, used: terminal.frame.height, cell: cell.height)
        terminal.setFrameOrigin(CGPoint(x: Self.inset.width + dx, y: Self.inset.height + dy))
        if laidOut != bounds.size {
            laidOut = bounds.size
            roomChanged?()
        }
    }

    private var room: CGSize {
        CGSize(width: max(0, bounds.width - 2 * Self.inset.width), height: max(0, bounds.height - 2 * Self.inset.height))
    }

    func mirror(cols: Int, rows: Int) {
        terminal.pane = (cols, rows)
        needsLayout = true
    }

    /// The pane size that fills this view.
    var fitting: AppAPI.PaneSize? {
        let cell = terminal.cell
        let room = self.room
        guard cell.width > 0, cell.height > 0, room.width > 0 else { return nil }
        // The scroller SwiftTerm reserves beside the cells.
        let w = room.width - NSScroller.scrollerWidth(for: .regular, scrollerStyle: terminal.scrollerStyle)
        let cols = min(1000, max(20, Int(w / cell.width)))
        let rows = min(500, max(5, Int(room.height / cell.height)))
        return AppAPI.PaneSize(cols: cols, rows: rows)
    }

    override func becomeFirstResponder() -> Bool {
        window?.makeFirstResponder(terminal) ?? false
    }
}

/// SwiftTerm's view as a mirror: its size is the pane's, not its
/// frame's, and the replies its emulator would send the program are
/// dropped (the server's emulator answers them); its mouse reports, the
/// wheel and clicks of a program that tracks the mouse, go to the
/// program as keys do. Shift+PgUp/PgDn scroll its scrollback on the main
/// screen (PaneScroll).
final class MirrorTerminalView: TerminalView {
    var pane: (cols: Int, rows: Int)? {
        didSet {
            guard let pane else { return }
            let t = getTerminal()
            if t.cols != pane.cols || t.rows != pane.rows {
                t.resize(cols: pane.cols, rows: pane.rows)
            }
            setFrameSize(frame.size)
        }
    }

    /// One cell, as SwiftTerm measures it for the font.
    var cell: CGSize {
        let f = font
        let scale = window?.backingScaleFactor ?? NSScreen.main?.backingScaleFactor ?? 1
        let w = f.advancement(forGlyph: f.glyph(withName: "W")).width
        let h = ceil(CTFontGetAscent(f) + CTFontGetDescent(f) + CTFontGetLeading(f))
        return CGSize(width: (w * scale).rounded() / scale, height: ceil(h * scale) / scale)
    }

    override func setFrameSize(_ newSize: NSSize) {
        guard pane != nil else {
            super.setFrameSize(newSize)
            return
        }
        // The frame that holds the pane's cells exactly, and a hair
        // more, so SwiftTerm's own sizing keeps the pane's cols and rows.
        let r = getOptimalFrameSize()
        super.setFrameSize(NSSize(width: r.width + 0.5, height: r.height + 0.5))
    }

    override func send(source: Terminal, data: ArraySlice<UInt8>) {
        if PaneScroll.isMouseReport(data) {
            terminalDelegate?.send(source: self, data: data)
        }
    }

    private var pageKeys: Any?

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        if let m = pageKeys {
            NSEvent.removeMonitor(m)
            pageKeys = nil
        }
        guard window != nil else { return }
        // SwiftTerm sends Shift+PgUp/PgDn to the program, and its keyDown
        // can't be overridden from here: take them before it.
        pageKeys = NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak self] e in
            guard let self, e.window === self.window, self.window?.firstResponder === self,
                  e.keyCode == 116 || e.keyCode == 121,
                  e.modifierFlags.intersection([.shift, .control, .option, .command]) == .shift else { return e }
            let t = getTerminal()
            guard let n = PaneScroll.page(rows: t.rows, alternate: t.isCurrentBufferAlternate) else { return e }
            if e.keyCode == 116 { scrollUp(lines: n) } else { scrollDown(lines: n) }
            return nil
        }
    }

    /// The context menu's Repaint, as View › Repaint Terminal.
    var onRepaint: (() -> Void)?

    override func menu(for event: NSEvent) -> NSMenu? {
        let m = NSMenu()
        m.addItem(withTitle: "Repaint", action: #selector(repaintFromMenu), keyEquivalent: "").target = self
        return m
    }

    @objc private func repaintFromMenu() { onRepaint?() }
}

/// The pane's terminal in the theme's colours (T770): its ANSI palette,
/// cursor and selection, set again when the theme or the appearance
/// changes.
struct PaneTerminal: NSViewRepresentable {
    let host: PaneHost
    let theme: Theme
    @Environment(\.colorScheme) private var scheme

    func makeNSView(context: Context) -> PaneHost {
        host.terminal.optionAsMetaKey = false
        paint(host, context.coordinator)
        return host
    }

    func updateNSView(_ nsView: PaneHost, context: Context) {
        paint(nsView, context.coordinator)
    }

    func makeCoordinator() -> Painted { Painted() }

    /// What the terminal was last painted in.
    final class Painted {
        var with: (TerminalColours, Bool)?
    }

    private func paint(_ h: PaneHost, _ painted: Painted) {
        let c = theme.colours.terminal, dark = scheme == .dark
        if let w = painted.with, w.0 == c, w.1 == dark { return }
        painted.with = (c, dark)
        h.terminal.paint(c, dark: dark)
    }
}

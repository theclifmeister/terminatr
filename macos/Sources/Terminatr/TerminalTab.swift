import AppKit
import SwiftTerm
import SwiftUI
import TerminatrKit

/// The Terminal tab: the session's pane through pane.vt, drawn by
/// SwiftTerm. The server's emulator stays the pane's; this one mirrors
/// it at the pane's size and never answers the program. Keys go back
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
    @State private var pane = PaneController()

    /// The terminal box's corners, which the session's notice above it
    /// shares; its side edges, and the view-only note's, are the pages'
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
                    PaneTerminal(controller: pane, theme: theme)
                        .task(id: PaneKey(session: id, bridge: app.bridge.map(ObjectIdentifier.init))) {
                            if let bridge = app.bridge { await pane.run(bridge, id) }
                        }
                        .onChange(of: remote, initial: true) { _, r in pane.keys = r != .view }
                        .onChange(of: model.repaints) { pane.repaint() }
                    if let closed = pane.closed, app.bridge != nil {
                        Text(closed)
                            .padding(10)
                            .frosted(RoundedRectangle(cornerRadius: 10))
                            .padding(16)
                    }
                }
                .background(theme.terminalBackground)
                .clipShape(RoundedRectangle(cornerRadius: Self.corner))
                .padding(.horizontal, Page.edge)
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

/// The pane stream and the mirror it feeds.
@MainActor
@Observable
final class PaneController: NSObject, TerminalViewDelegate {
    private(set) var size: AppAPI.PaneSize?
    private(set) var closed: String?
    /// Keys are sent: false while a paired server's switch is view,
    /// where its server would drop them.
    @ObservationIgnored var keys = true

    @ObservationIgnored let host = PaneHost()
    @ObservationIgnored private var bridge: Bridge?
    @ObservationIgnored private var stream: Int?
    @ObservationIgnored private var pending = Data()
    @ObservationIgnored private var claim: AppAPI.PaneSize?
    @ObservationIgnored private var fits = PaneFit()
    @ObservationIgnored private var sending = false

    override init() {
        super.init()
        host.terminal.terminalDelegate = self
        host.terminal.onRepaint = { [weak self] in self?.repaint() }
        host.roomChanged = { [weak self] in self?.roomChanged() }
    }

    /// Streams session id's pane into the mirror until cancelled or
    /// the pane closes.
    func run(_ bridge: Bridge, _ id: String) async {
        self.bridge = bridge
        closed = nil
        do {
            let sub = try await bridge.subscribe(AppAPI.Methods.paneVt, AppAPI.PaneParams(session: id))
            stream = sub.id
            fits.opened()
            show(cols: sub.result.session.cols, rows: sub.result.session.rows)
            fit()
            for try await e in sub.events {
                switch e.name {
                case "pane.repaint":
                    let t = host.terminal.getTerminal()
                    t.resetToInitialState()
                    show(cols: e.data.cols, rows: e.data.rows)
                    host.terminal.feed(byteArray: ArraySlice(e.data.data))
                case "pane.output":
                    host.terminal.feed(byteArray: ArraySlice(e.data.data))
                case "pane.resize":
                    show(cols: e.data.cols, rows: e.data.rows)
                case "pane.closed":
                    // A paired server's pane says why in the server's
                    // words, this server's in a fixed one.
                    let why = e.data.reason
                    closed = why.contains(" ") ? "The pane closed: \(why)." : "The pane closed (\(why))."
                default:
                    break
                }
            }
        } catch {
            if !Task.isCancelled {
                closed = error.localizedDescription
            }
        }
        stream = nil
    }

    private func show(cols: Int, rows: Int) {
        guard cols > 0, rows > 0 else { return }
        size = AppAPI.PaneSize(cols: cols, rows: rows)
        host.mirror(cols: cols, rows: rows)
    }

    /// Claims the size that fills the tab, when PaneFit says to.
    private func fit() {
        guard let c = fits.claim(fitting: host.fitting, pane: size, keys: keys, open: stream != nil) else { return }
        claim = c
        flush()
    }

    /// The tab's room changed (the window resized, the inspector or the
    /// sidebar shown or hidden): claims once it settles.
    private func roomChanged() {
        let move = fits.moved()
        Task { [weak self] in
            try? await Task.sleep(for: PaneFit.settle)
            guard let self, self.fits.settled(move) else { return }
            self.fit()
        }
    }

    func repaint() {
        guard let bridge, let stream else { return }
        Task { _ = try? await bridge.call(AppAPI.Methods.paneInput, AppAPI.PaneInputParams(id: stream, repaint: true)) }
    }

    /// Sends what was typed, in order, a call at a time. Keys claim the
    /// window's size when the pane has another, as typing in a console
    /// does: once per size.
    private func type(_ data: Data) {
        guard keys else { return }
        if let c = fits.claim(fitting: host.fitting, pane: size, keys: keys, open: stream != nil) {
            claim = c
        }
        pending.append(data)
        flush()
    }

    private func flush() {
        guard !sending, let bridge, let stream else { return }
        sending = true
        Task {
            while !pending.isEmpty || claim != nil {
                let input = pending, size = claim
                pending = Data()
                claim = nil
                _ = try? await bridge.call(AppAPI.Methods.paneInput, AppAPI.PaneInputParams(id: stream, input: input, size: size))
            }
            sending = false
        }
    }

    // MARK: TerminalViewDelegate

    nonisolated func send(source: TerminalView, data: ArraySlice<UInt8>) {
        let d = Data(data)
        MainActor.assumeIsolated { type(d) }
    }

    nonisolated func requestOpenLink(source: TerminalView, link: String, params: [String: String]) {
        MainActor.assumeIsolated { LinkConfirm.open(link) }
    }

    /// A program setting the clipboard (OSC 52) is ignored: an agent's
    /// output never writes the user's pasteboard.
    nonisolated func clipboardCopy(source: TerminalView, content: Data) {}
    nonisolated func sizeChanged(source: TerminalView, newCols: Int, newRows: Int) {}
    nonisolated func setTerminalTitle(source: TerminalView, title: String) {}
    nonisolated func hostCurrentDirectoryUpdate(source: TerminalView, directory: String?) {}
    nonisolated func scrolled(source: TerminalView, position: Double) {}
    nonisolated func rangeChanged(source: TerminalView, startY: Int, endY: Int) {}
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
/// dropped (the server's emulator answers them).
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

    override func send(source: Terminal, data: ArraySlice<UInt8>) {}

    /// The context menu's Repaint, as View › Repaint Terminal.
    var onRepaint: (() -> Void)?

    override func menu(for event: NSEvent) -> NSMenu? {
        let m = NSMenu()
        m.addItem(withTitle: "Repaint", action: #selector(repaintFromMenu), keyEquivalent: "").target = self
        return m
    }

    @objc private func repaintFromMenu() { onRepaint?() }
}

struct PaneTerminal: NSViewRepresentable {
    let controller: PaneController
    let theme: Theme

    func makeNSView(context: Context) -> PaneHost {
        let h = controller.host
        h.terminal.nativeBackgroundColor = NSColor(theme.terminalBackground)
        h.terminal.nativeForegroundColor = NSColor(theme.terminalForeground)
        h.terminal.optionAsMetaKey = false
        return h
    }

    func updateNSView(_ nsView: PaneHost, context: Context) {}
}

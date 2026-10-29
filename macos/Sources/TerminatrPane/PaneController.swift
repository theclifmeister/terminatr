import Foundation
import SwiftTerm
import TerminatrKit
import TerminatrUI

/// A session's pane through pane.vt, mirrored in a SwiftTerm view: the
/// Mac's Terminal tab and the phone's (T840) alike. The server's
/// emulator stays the pane's; the mirror shows it at the pane's size and
/// never answers the program. Keys go back through pane.input as the
/// human's: typed text takes a thread over. A client that claims the
/// pane's size for its room (the Mac, PaneFit) sets fitting; one that
/// leaves it unset (the phone) never resizes the pane for the others.
@MainActor
@Observable
public final class PaneController: NSObject, TerminalViewDelegate {
    public private(set) var size: AppAPI.PaneSize?
    public private(set) var closed: String?
    /// Keys are sent: false while a paired server's switch is view,
    /// where its server would drop them, below a device's write level,
    /// and while an escape's prompt may be up on a device; the mouse
    /// stays the view's then.
    @ObservationIgnored public var keys = true {
        didSet { terminal.allowMouseReporting = keys && mouse }
    }
    /// Whether a program that tracks the mouse gets the view's clicks
    /// and wheel: the Mac's; the phone's taps stay its own.
    @ObservationIgnored public var mouse = true {
        didSet { terminal.allowMouseReporting = keys && mouse }
    }
    /// The pane size that fills the view, for a client that claims it.
    @ObservationIgnored public var fitting: (() -> AppAPI.PaneSize?)?
    /// Told the pane's size when it is known or changes: the view pins
    /// its mirror to it.
    @ObservationIgnored public var mirror: ((Int, Int) -> Void)?
    /// A link tapped or clicked in the terminal.
    @ObservationIgnored public var openLink: ((String) -> Void)?

    @ObservationIgnored public let terminal: TerminalView
    @ObservationIgnored private var bridge: Bridge?
    @ObservationIgnored private var stream: Int?
    @ObservationIgnored private var pending = Data()
    @ObservationIgnored private var claim: AppAPI.PaneSize?
    @ObservationIgnored private var fits = PaneFit()
    @ObservationIgnored private var sending = false

    public init(terminal: TerminalView) {
        self.terminal = terminal
        super.init()
        terminal.terminalDelegate = self
    }

    /// Streams session id's pane into the mirror until cancelled or
    /// the pane closes.
    public func run(_ bridge: Bridge, _ id: String) async {
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
                    let t = terminal.getTerminal()
                    t.resetToInitialState()
                    show(cols: e.data.cols, rows: e.data.rows)
                    terminal.feed(byteArray: ArraySlice(e.data.data))
                case "pane.output":
                    terminal.feed(byteArray: ArraySlice(e.data.data))
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
        mirror?(cols, rows)
    }

    /// Claims the size that fills the view, when PaneFit says to.
    private func fit() {
        guard let fitting, let c = fits.claim(fitting: fitting(), pane: size, keys: keys, open: stream != nil) else { return }
        claim = c
        flush()
    }

    /// The view's room changed (the window resized, the inspector or
    /// the sidebar shown or hidden): claims once it settles.
    public func roomChanged() {
        guard fitting != nil else { return }
        let move = fits.moved()
        Task { [weak self] in
            try? await Task.sleep(for: PaneFit.settle)
            guard let self, self.fits.settled(move) else { return }
            self.fit()
        }
    }

    /// Asks the server for a fresh repaint (pane.input, a write).
    public func repaint() {
        guard let bridge, let stream else { return }
        Task { _ = try? await bridge.call(AppAPI.Methods.paneInput, AppAPI.PaneInputParams(id: stream, repaint: true)) }
    }

    /// Sends what was typed, in order, a call at a time. Keys claim the
    /// view's size when the pane has another, as typing in a console
    /// does: once per size.
    public func type(_ data: Data) {
        guard keys else { return }
        if let fitting, let c = fits.claim(fitting: fitting(), pane: size, keys: keys, open: stream != nil) {
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

    public nonisolated func send(source: TerminalView, data: ArraySlice<UInt8>) {
        let d = Data(data)
        MainActor.assumeIsolated { type(d) }
    }

    public nonisolated func requestOpenLink(source: TerminalView, link: String, params: [String: String]) {
        MainActor.assumeIsolated { openLink?(link) }
    }

    /// A program setting the clipboard (OSC 52) is ignored: an agent's
    /// output never writes the user's pasteboard.
    public nonisolated func clipboardCopy(source: TerminalView, content: Data) {}
    public nonisolated func sizeChanged(source: TerminalView, newCols: Int, newRows: Int) {}
    public nonisolated func setTerminalTitle(source: TerminalView, title: String) {}
    public nonisolated func hostCurrentDirectoryUpdate(source: TerminalView, directory: String?) {}
    public nonisolated func scrolled(source: TerminalView, position: Double) {}
    public nonisolated func rangeChanged(source: TerminalView, startY: Int, endY: Int) {}
}

extension PlatformColor {
    /// A theme's swatch as drawn in light or dark (the terminal's, set
    /// once per appearance).
    public convenience init(_ s: Swatch, dark: Bool) {
        self.init(hex: s.hex(dark: dark), alpha: s.alpha(dark: dark))
    }
}

extension TerminalView {
    /// Paints the terminal in the theme's colours (T770): its ANSI
    /// palette, cursor and selection.
    @MainActor
    public func paint(_ c: TerminalColours, dark: Bool) {
        nativeBackgroundColor = PlatformColor(c.background, dark: dark)
        nativeForegroundColor = PlatformColor(c.foreground, dark: dark)
        caretColor = PlatformColor(c.cursor, dark: dark)
        selectedTextBackgroundColor = PlatformColor(c.selection, dark: dark)
        installColors(c.ansi.map { s in
            let x = s.hex(dark: dark)
            return SwiftTerm.Color(red8: UInt16((x >> 16) & 0xff), green8: UInt16((x >> 8) & 0xff), blue8: UInt16(x & 0xff))
        })
    }
}

import AppKit
import SwiftTerm
import SwiftUI
import TerminatrKit

/// The Terminal tab: the session's pane through pane.vt, drawn by
/// SwiftTerm. The server's emulator stays the pane's; this one mirrors
/// it at the pane's size and never answers the program. Keys go back
/// through pane.input as the human's: typed text takes a thread over,
/// and the first keys claim the pane's size for this window. A paired
/// server's pane ("<alias>/<session>") comes the same way through the
/// server's peer link, under that server's switch: it opens at view or
/// write, and takes keys at write alone (the far server checks each).
struct TerminalTab: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let model: SessionModel
    let session: AppAPI.Session?
    @State private var pane = PaneController()

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
            } else if let bridge = app.bridge, let id = model.sessionID {
                ZStack(alignment: .bottom) {
                    PaneTerminal(controller: pane, theme: theme)
                        .task(id: id) { await pane.run(bridge, id) }
                        .onChange(of: remote, initial: true) { _, r in pane.keys = r != .view }
                    if let closed = pane.closed {
                        Text(closed)
                            .padding(10)
                            .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 10))
                            .padding(16)
                    }
                }
                .background(theme.terminalBackground)
                .clipShape(RoundedRectangle(cornerRadius: 12))
                .padding(.horizontal, 14)
                footer(remote)
            }
        }
        .guardingLinks()
    }

    private func footer(_ remote: RemotePane?) -> some View {
        HStack(spacing: 12) {
            if let s = pane.size {
                Text("\(s.cols)×\(s.rows)" + (remote == nil ? "" : " on \(model.ref.server)"))
                    .font(theme.mono(11))
                    .foregroundStyle(theme.muted)
            }
            Text(note(remote))
                .font(theme.body(11.5))
                .foregroundStyle(theme.muted)
            Spacer()
            Button("Fit to Window") { pane.fit() }
                .disabled(remote == .view)
                .help("Claims the pane's size for this window, as typing does")
            Button("Repaint") { pane.repaint() }
        }
        .controlSize(.small)
        .padding(.horizontal, 18)
        .padding(.vertical, 8)
    }
}

extension TerminalTab {
    private func note(_ remote: RemotePane?) -> String {
        let server = model.ref.server
        switch remote {
        case .view: return "View only: \(server) lets this server watch its panes, not type into them."
        case .write: return model.ref.isCoordinator ? "Keys here go to the coordinator on \(server)." : "Typing here takes the thread over on \(server)."
        default: return model.ref.isCoordinator ? "Keys here go to the coordinator." : "Typing here takes the thread over."
        }
    }
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
    /// The size these keys last claimed.
    @ObservationIgnored private var claimed: AppAPI.PaneSize?
    @ObservationIgnored private var sending = false

    override init() {
        super.init()
        host.terminal.terminalDelegate = self
    }

    /// Streams session id's pane into the mirror until cancelled or
    /// the pane closes.
    func run(_ bridge: Bridge, _ id: String) async {
        self.bridge = bridge
        closed = nil
        do {
            let sub = try await bridge.subscribe(AppAPI.Methods.paneVt, AppAPI.PaneParams(session: id))
            stream = sub.id
            show(cols: sub.result.session.cols, rows: sub.result.session.rows)
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

    /// Claims the size that fills the window.
    func fit() {
        claim = host.fitting
        flush()
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
        if let fit = host.fitting, fit != size, fit != claimed {
            claim = fit
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
                if let size { claimed = size }
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

    override func layout() {
        super.layout()
        terminal.setFrameOrigin(CGPoint(x: Self.inset.width, y: Self.inset.height))
        terminal.setFrameSize(room)
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
        let w = room.width - NSScroller.scrollerWidth(for: .regular, scrollerStyle: NSScroller.preferredScrollerStyle)
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

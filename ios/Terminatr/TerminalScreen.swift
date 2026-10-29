import SwiftTerm
import SwiftUI
import TerminatrKit
import TerminatrPane
import TerminatrUI
import UIKit

/// A session's terminal (T840), the Mac's Terminal tab on the phone:
/// its pane through pane.vt (PaneController, shared with the Mac),
/// drawn by SwiftTerm. The phone never resizes the pane for the other
/// viewers: the mirror keeps the pane's size, at a font that fits its
/// width (PhonePaneHost), pinched to zoom and panned past the screen.
/// It shows the pane's screen, not its scrollback: the conversation is
/// the history. At the write level a tap brings the keyboard with
/// SwiftTerm's key bar (Esc, Ctrl, Tab, the arrows, function keys),
/// and keys go in through pane.input as the human's, signed; not while
/// an escape's prompt may be up (the server drops a device's keys then
/// too), nor at a paired server's view switch. A paired server's pane
/// ("<alias>/<session>") comes the same way, under that server's switch.
struct TerminalScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    let ref: SessionRef
    /// Goes up for each repaint the user asks: the stream opens again,
    /// which repaints at every level, unsigned.
    let repaints: Int
    /// Made once the screen is up, never while SwiftUI updates it:
    /// SwiftTerm's key bar, a keyboard input view, sets up the scene's
    /// keyboard window as it is made, which lays the window out, and
    /// SwiftUI refuses that from inside its update (iOS 27.1).
    @State private var pane: PhonePane?

    var body: some View {
        let id = model.sessionID(ref)
        let session = id.flatMap { id in model.sessions.first { $0.id == id } }
        let remote = ref.isRemote ? model.remotePane(ref) : nil
        let held = session.map { $0.escape || $0.question?.escape == true } ?? false
        let note = note(remote: remote, held: held)
        // A ZStack, not a Group: a Group's modifiers go to its children,
        // and before the pane is made the branch below had none, so the
        // .task that makes it never ran and the screen stayed blank
        // (T853).
        ZStack {
            if id == nil {
                ContentUnavailableView("No session runs", systemImage: "moon.zzz",
                                       description: Text("Its terminal shows while a session runs it."))
            } else if !model.caps.contains("pane-vt") {
                message("This tm server doesn't stream panes to the app; tm update and tm server restart bring it.")
            } else if case .closed(let why) = remote {
                message(why)
            } else if let id {
                ZStack(alignment: .bottom) {
                    // The box in the terminal's colours from the first
                    // frame, the pane's mirror once it is made.
                    if let pane {
                        PhoneTerminal(host: pane.host, theme: theme)
                            // A redial is a new bridge: the stream opens again on it.
                            .task(id: "\(id)/\(model.generation)/\(repaints)") {
                                if let b = model.bridge { await pane.controller.run(b, id) }
                            }
                    } else {
                        Color.clear
                    }
                    if let closed = pane?.controller.closed {
                        Text(closed)
                            .font(theme.body(.subheadline))
                            .padding(10)
                            .frosted(RoundedRectangle(cornerRadius: 10))
                            .padding(16)
                    }
                }
                .background(theme.terminalBackground)
                // A box on the screen's backdrop, as the Mac's tab draws
                // it: the bars keep the theme's colours above it.
                .clipShape(RoundedRectangle(cornerRadius: Self.corner))
                .padding(.horizontal, 10)
                .padding(.vertical, 8)
                .safeAreaInset(edge: .bottom, spacing: 0) {
                    if let note {
                        Text(note)
                            .font(theme.body(.footnote))
                            .foregroundStyle(theme.muted)
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(.horizontal, 16)
                            .padding(.vertical, 8)
                            .background(.bar, ignoresSafeAreaEdges: .bottom)
                    }
                }
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .backdrop()
        .task {
            if pane == nil {
                let p = PhonePane()
                p.typing = note == nil
                pane = p
            }
        }
        .onChange(of: note == nil) { _, typing in pane?.typing = typing }
    }

    /// The terminal box's corners.
    static let corner: CGFloat = 12

    /// Why keys don't go in, when they don't: nil when they do.
    private func note(remote: RemotePane?, held: Bool) -> String? {
        if remote == .view { return "View only: \(ref.server) lets this server watch its panes, not type into them." }
        if !model.can(AppAPI.Methods.paneInput.name) { return "View only: this iPhone's level doesn't type into terminals." }
        if held { return "A command asks to run outside the sandbox: answer it on your computer. Keys are off until then." }
        return nil
    }

    private func message(_ text: String) -> some View {
        Text(text)
            .foregroundStyle(theme.muted)
            .multilineTextAlignment(.center)
            .padding(28)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

/// The screen's pane: the shared stream into the phone's mirror, which
/// claims no size.
@MainActor
@Observable
final class PhonePane {
    @ObservationIgnored let host = PhonePaneHost()
    let controller: PaneController

    /// Keys go in: the terminal takes the keyboard.
    @ObservationIgnored var typing = false {
        didSet {
            controller.keys = typing
            host.terminal.typing = typing
        }
    }

    init() {
        let host = host
        controller = PaneController(terminal: host.terminal)
        controller.mouse = false
        controller.keys = false
        controller.mirror = { cols, rows in host.mirror(cols: cols, rows: rows) }
        controller.openLink = { LinkConfirm.open($0) }
    }
}

/// A link tapped in the terminal: shown with its full URL first, and
/// opened only for http, https and mailto, as the Mac's.
enum LinkConfirm {
    @MainActor
    static func open(_ link: String) {
        let url = URL(string: link).flatMap { SafeMarkdown.allowed($0) ? $0 : nil }
        let a = UIAlertController(title: "Open this link?",
                                  message: url == nil ? "\(link)\n\nOnly web and mail links open from the app." : url?.absoluteString,
                                  preferredStyle: .alert)
        if let url {
            a.addAction(UIAlertAction(title: "Open", style: .default) { _ in UIApplication.shared.open(url) })
            a.addAction(UIAlertAction(title: "Cancel", style: .cancel))
        } else {
            a.addAction(UIAlertAction(title: "OK", style: .cancel))
        }
        let scene = UIApplication.shared.connectedScenes.compactMap { $0 as? UIWindowScene }.first { $0.activationState == .foregroundActive }
        var top = scene?.keyWindow?.rootViewController
        while let p = top?.presentedViewController { top = p }
        top?.present(a, animated: true)
    }
}

/// The mirror in a scroll view of its own: at the pane's size, in the
/// font that fits the pane's width in the room (PaneZoom), pinched to
/// zoom between the smallest readable and the largest; past the room it
/// pans, and it stays at the bottom, where the prompt is, until the
/// user pans away.
final class PhonePaneHost: UIScrollView, UIScrollViewDelegate {
    let terminal = PhoneMirrorView(frame: .zero, font: UIFont.monospacedSystemFont(ofSize: PaneZoom.largest, weight: .regular))
    private var pane: (cols: Int, rows: Int)?
    /// The pinch's scale on the fitted font.
    private var zoom: CGFloat = 1
    private var fontSize: CGFloat = 0
    /// The size that fits the pane's width, unzoomed.
    private var fitted: CGFloat = PaneZoom.smallest
    private var laidOut: CGSize = .zero
    /// The user hasn't panned up from the bottom.
    private var atBottom = true

    /// Room between the cells and the screen's edges.
    static let inset = CGSize(width: 6, height: 6)

    override init(frame: CGRect) {
        super.init(frame: frame)
        delegate = self
        alwaysBounceVertical = false
        contentInsetAdjustmentBehavior = .never
        keyboardDismissMode = .interactive
        terminal.changeScrollback(nil)
        terminal.isScrollEnabled = false
        terminal.optionAsMetaKey = false
        addSubview(terminal)
        addGestureRecognizer(UIPinchGestureRecognizer(target: self, action: #selector(pinched(_:))))
    }

    required init?(coder: NSCoder) { fatalError("not from a nib") }

    func mirror(cols: Int, rows: Int) {
        if let p = pane, p.cols == cols, p.rows == rows { return }
        pane = (cols, rows)
        refit()
    }

    override func layoutSubviews() {
        super.layoutSubviews()
        if bounds.size != laidOut {
            laidOut = bounds.size
            refit()
        }
    }

    /// Sets the font for the pane and the room, pins the mirror to the
    /// pane's size in it and centres it while it is smaller.
    private func refit() {
        guard let pane, bounds.width > 0 else { return }
        let room = bounds.width - 2 * Self.inset.width
        let advance = Self.advance
        fitted = PaneZoom.fit(room: room, cols: pane.cols, advance: advance)
        let size = PaneZoom.size(fitting: room, cols: pane.cols, advance: advance, zoom: zoom)
        terminal.pin(cols: pane.cols, rows: pane.rows, fontSize: size == fontSize ? nil : size)
        fontSize = size
        let grid = terminal.frame.size
        contentSize = CGSize(width: grid.width + 2 * Self.inset.width, height: grid.height + 2 * Self.inset.height)
        terminal.frame.origin = CGPoint(x: max(Self.inset.width, (bounds.width - grid.width) / 2), y: Self.inset.height)
        if atBottom { toBottom() }
    }

    /// A cell's width per point of font size: the system's monospaced
    /// font's, as SwiftTerm measures a cell.
    static let advance: CGFloat = {
        let f = UIFont.monospacedSystemFont(ofSize: 100, weight: .regular)
        return ("W" as NSString).size(withAttributes: [.font: f]).width / 100
    }()

    private func toBottom() {
        let y = max(0, contentSize.height - bounds.height + adjustedContentInset.bottom)
        contentOffset = CGPoint(x: contentOffset.x, y: y)
    }

    @objc private func pinched(_ g: UIPinchGestureRecognizer) {
        switch g.state {
        case .changed:
            terminal.transform = CGAffineTransform(scaleX: g.scale, y: g.scale)
        case .ended, .cancelled:
            terminal.transform = .identity
            let at = g.location(in: terminal)
            let before = terminal.frame.size
            zoom = PaneZoom.clamp(zoom: zoom * g.scale, fitted: fitted)
            refit()
            // Keep the point under the fingers where it was.
            let k = terminal.frame.width / max(1, before.width)
            let local = g.location(in: self)
            let x = terminal.frame.minX + at.x * k - (local.x - contentOffset.x)
            let y = terminal.frame.minY + at.y * k - (local.y - contentOffset.y)
            contentOffset = CGPoint(x: min(max(0, x), max(0, contentSize.width - bounds.width)),
                                    y: min(max(0, y), max(0, contentSize.height - bounds.height)))
            atBottom = contentOffset.y >= contentSize.height - bounds.height - 4
        default:
            break
        }
    }

    // MARK: UIScrollViewDelegate

    func scrollViewDidEndDragging(_ scrollView: UIScrollView, willDecelerate decelerate: Bool) {
        if !decelerate { rest() }
    }

    func scrollViewDidEndDecelerating(_ scrollView: UIScrollView) { rest() }

    private func rest() {
        atBottom = contentOffset.y >= contentSize.height - bounds.height + adjustedContentInset.bottom - 4
    }
}

/// SwiftTerm's view as a mirror, as the Mac's: its size is the pane's,
/// not its frame's, and the replies its emulator would send the program
/// are dropped (the server's emulator answers them). It takes the
/// keyboard only while keys go in.
final class PhoneMirrorView: TerminalView {
    var typing = false {
        didSet { if !typing, isFirstResponder { _ = resignFirstResponder() } }
    }

    override var canBecomeFirstResponder: Bool { typing }

    /// Pins the emulator to cols × rows, at fontSize when given, and the
    /// frame to the cells that hold them, and a hair more, so SwiftTerm's
    /// own sizing from the frame keeps the pane's cols and rows.
    func pin(cols: Int, rows: Int, fontSize: CGFloat?) {
        if let fontSize {
            // A font change resizes the emulator from the frame unless
            // the frame is empty: empty it first, and the pane's size
            // stays.
            frame.size = .zero
            font = UIFont.monospacedSystemFont(ofSize: fontSize, weight: .regular)
        }
        let t = getTerminal()
        if t.cols != cols || t.rows != rows {
            t.resize(cols: cols, rows: rows)
        }
        let r = getOptimalFrameSize()
        frame.size = CGSize(width: r.width + 0.5, height: r.height + 0.5)
    }

    override func send(source: Terminal, data: ArraySlice<UInt8>) {
        if PaneScroll.isMouseReport(data) {
            terminalDelegate?.send(source: self, data: data)
        }
    }
}

/// The phone's terminal in the theme's colours, as the Mac's.
struct PhoneTerminal: UIViewRepresentable {
    let host: PhonePaneHost
    let theme: Theme
    @Environment(\.colorScheme) private var scheme

    func makeUIView(context: Context) -> PhonePaneHost {
        paint(host, context.coordinator)
        return host
    }

    func updateUIView(_ uiView: PhonePaneHost, context: Context) {
        paint(uiView, context.coordinator)
    }

    func makeCoordinator() -> Painted { Painted() }

    /// What the terminal was last painted in.
    final class Painted {
        var with: (TerminalColours, Bool)?
    }

    private func paint(_ h: PhonePaneHost, _ painted: Painted) {
        let c = theme.colours.terminal, dark = scheme == .dark
        if let w = painted.with, w.0 == c, w.1 == dark { return }
        painted.with = (c, dark)
        h.terminal.paint(c, dark: dark)
        // SwiftTerm on iOS keeps the layer's colour it set up with
        // (black) under the cells it draws: the theme's, before the
        // first frame too.
        let bg = PlatformColor(c.background, dark: dark)
        h.terminal.layer.backgroundColor = bg.cgColor
        h.backgroundColor = bg
    }
}

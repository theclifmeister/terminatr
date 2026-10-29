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
/// Above the screen a swipe scrolls through the pane's scrollback, kept
/// in the mirror from the first repaint (T861), with a button back to
/// the bottom; a program that tracks the mouse or runs full screen
/// gets the swipe as the Mac's wheel, at the write level alone. At the
/// write level a tap brings the keyboard with SwiftTerm's key bar
/// (Esc, Ctrl, Tab, the arrows, function keys),
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
                    if let pane, pane.back {
                        Button { pane.host.jumpToBottom() } label: {
                            Label("Jump to bottom", systemImage: "arrow.down")
                                .font(theme.body(.subheadline, .semibold))
                                .padding(.horizontal, 14)
                                .padding(.vertical, 8)
                                .frosted(Capsule())
                                .overlay(Capsule().strokeBorder(theme.edge, lineWidth: 1))
                                .shadow(color: .black.opacity(0.18), radius: 8, y: 3)
                        }
                        .buttonStyle(.plain)
                        .padding(.bottom, 10)
                    }
                    // A drop's error says nothing: the redial streams it again.
                    if let closed = pane?.controller.closed, !model.relinking {
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
            host.keysChanged()
        }
    }
    /// The terminal reads its scrollback, off the bottom.
    private(set) var back = false

    init() {
        let host = host
        controller = PaneController(terminal: host.terminal)
        controller.mouse = false
        controller.keys = false
        controller.mirror = { cols, rows in host.mirror(cols: cols, rows: rows) }
        controller.openLink = { LinkConfirm.open($0) }
        host.onBack = { [weak self] in self?.back = $0 }
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
/// user scrolls away. Above the screen the scroll goes on into the
/// mirror's scrollback (T861), one row of it a cell: the mirror stays
/// a screen tall and moves down the rows it shows as the scroll goes
/// up, and output doesn't move what the user reads off the bottom. A
/// program that takes the swipe (PaneScroll.swipe) gets it instead
/// past the screen's edges, as wheel ticks or arrow keys.
final class PhonePaneHost: UIScrollView, UIScrollViewDelegate {
    let terminal = PhoneMirrorView(frame: .zero, font: UIFont.monospacedSystemFont(ofSize: PaneZoom.largest, weight: .regular))
    private var pane: (cols: Int, rows: Int)?
    /// The pinch's scale on the fitted font.
    private var zoom: CGFloat = 1
    private var fontSize: CGFloat = 0
    /// The size that fits the pane's width, unzoomed.
    private var fitted: CGFloat = PaneZoom.smallest
    private var laidOut: CGSize = .zero
    /// The user hasn't scrolled up from the bottom.
    private var atBottom = true
    /// A cell's height, and the rows of scrollback above the screen in
    /// the scroll: none while a program takes the swipe.
    private var cell: CGFloat = 0
    private var history = 0
    /// The scrollback row the mirror was last placed at.
    private var shown = 0
    private var swipe = PaneScroll.Swipe.history
    /// Moving the offset from here, not the user's scroll.
    private var placing = false
    /// The view reads the scrollback: the jump-to-bottom button shows.
    private(set) var back = false {
        didSet { if back != oldValue { onBack?(back) } }
    }
    var onBack: ((Bool) -> Void)?
    /// A program's swipe, beside the scroll's own pan.
    private let wheel = UIPanGestureRecognizer()
    private let wheelRules = Simultaneous()
    private var wheelLast: CGFloat = 0
    private var wheelCarry: CGFloat = 0

    /// Room between the cells and the screen's edges.
    static let inset = CGSize(width: 6, height: 6)

    override init(frame: CGRect) {
        super.init(frame: frame)
        delegate = self
        alwaysBounceVertical = false
        isDirectionalLockEnabled = true
        contentInsetAdjustmentBehavior = .never
        keyboardDismissMode = .interactive
        terminal.changeScrollback(PaneScroll.lines)
        terminal.isScrollEnabled = false
        terminal.optionAsMetaKey = false
        terminal.onChange = { [weak self] reset in
            if reset { self?.atBottom = true }
            self?.layOut()
        }
        terminal.onAccessibilityScroll = { [weak self] in self?.page($0) ?? false }
        addSubview(terminal)
        addGestureRecognizer(UIPinchGestureRecognizer(target: self, action: #selector(pinched(_:))))
        wheel.addTarget(self, action: #selector(swiped(_:)))
        wheelRules.begins = { [weak self] g in self?.wheelBegins(g) ?? false }
        wheel.delegate = wheelRules
        addGestureRecognizer(wheel)
    }

    required init?(coder: NSCoder) { fatalError("not from a nib") }

    func mirror(cols: Int, rows: Int) {
        if let p = pane, p.cols == cols, p.rows == rows { return }
        pane = (cols, rows)
        refit()
    }

    /// Keys went on or off: whether a program takes the swipe.
    func keysChanged() { layOut() }

    override func layoutSubviews() {
        super.layoutSubviews()
        if bounds.size != laidOut {
            laidOut = bounds.size
            refit()
        }
    }

    /// Sets the font for the pane and the room and pins the mirror to
    /// the pane's size in it.
    private func refit() {
        guard let pane, bounds.width > 0 else { return }
        let room = bounds.width - 2 * Self.inset.width
        let advance = Self.advance
        fitted = PaneZoom.fit(room: room, cols: pane.cols, advance: advance)
        let size = PaneZoom.size(fitting: room, cols: pane.cols, advance: advance, zoom: zoom)
        terminal.pin(cols: pane.cols, rows: pane.rows, fontSize: size == fontSize ? nil : size)
        fontSize = size
        layOut()
    }

    /// Sizes the scroll for the scrollback and the screen, centres the
    /// grid while it is narrower than the room and keeps what the user
    /// reads where it is: the bottom while they are at it.
    private func layOut() {
        guard let pane, pane.rows > 0, bounds.width > 0 else { return }
        let t = terminal.getTerminal()
        let was = cell, wasTop = CGFloat(history) * was
        let grid = terminal.frame.size
        cell = max(1, (grid.height - 0.5) / CGFloat(pane.rows))
        swipe = PaneScroll.swipe(alternate: t.isCurrentBufferAlternate, mouse: t.mouseMode != .off,
                                 alternateScroll: t.alternateScrollMode, keys: terminal.typing)
        // A program's swipe at the grid's edges doesn't rubber-band it.
        bounces = swipe == .history || swipe == .none
        // SwiftTerm sizes its own scroll for every line it keeps.
        let lines = Int((terminal.contentSize.height / cell).rounded())
        let latest = max(0, lines - pane.rows)
        history = swipe == .history ? latest : 0
        if swipe != .history, t.buffer.yDisp != latest {
            terminal.scrollTo(row: latest, notifyAccessibility: false)
        }
        let top = CGFloat(history) * cell
        contentSize = CGSize(width: grid.width + 2 * Self.inset.width,
                             height: top + max(grid.height + 2 * Self.inset.height, bounds.height))
        terminal.frame.origin.x = max(Self.inset.width, (bounds.width - grid.width) / 2)
        let y = contentOffset.y
        let target: CGFloat
        if atBottom {
            target = bottom
        } else if was > 0, y < wasTop {
            // In the scrollback: the row the mirror shows, which a full
            // scrollback moves up as old lines go.
            target = CGFloat(min(t.buffer.yDisp, history)) * cell + (y - CGFloat(shown) * was) * cell / was
        } else {
            target = top + (y - wasTop)
        }
        placing = true
        contentOffset.y = min(max(0, target), bottom)
        placing = false
        place()
    }

    /// The largest offset: the screen's last row at the bottom.
    private var bottom: CGFloat {
        max(0, contentSize.height - bounds.height + adjustedContentInset.bottom)
    }

    /// Puts the mirror on the scrollback row the offset reads.
    private func place() {
        guard cell > 0 else { return }
        let row = PaneScroll.row(offset: contentOffset.y, cell: cell, history: history)
        shown = row
        terminal.frame.origin.y = Self.inset.height + CGFloat(row) * cell
        if swipe == .history, terminal.getTerminal().buffer.yDisp != row {
            terminal.scrollTo(row: row, notifyAccessibility: false)
        }
        back = PaneScroll.back(offset: contentOffset.y, cell: cell, history: history)
        terminal.accessibilityCustomActions = back ? [UIAccessibilityCustomAction(name: "Jump to bottom") { [weak self] _ in
            self?.jumpToBottom()
            return true
        }] : nil
    }

    /// The jump-to-bottom button's: back to the screen, following output.
    func jumpToBottom() {
        atBottom = true
        // A plain set stops a running deceleration.
        setContentOffset(CGPoint(x: contentOffset.x, y: bottom), animated: false)
        place()
    }

    /// A cell's width per point of font size: the system's monospaced
    /// font's, as SwiftTerm measures a cell.
    static let advance: CGFloat = {
        let f = UIFont.monospacedSystemFont(ofSize: 100, weight: .regular)
        return ("W" as NSString).size(withAttributes: [.font: f]).width / 100
    }()

    @objc private func pinched(_ g: UIPinchGestureRecognizer) {
        switch g.state {
        case .changed:
            terminal.transform = CGAffineTransform(scaleX: g.scale, y: g.scale)
        case .ended, .cancelled:
            terminal.transform = .identity
            // The line and the column under the fingers, in the scroll.
            let local = g.location(in: self)
            let screen = CGPoint(x: local.x - contentOffset.x, y: local.y - contentOffset.y)
            let across = (local.x - terminal.frame.minX) / max(1, terminal.frame.width)
            let line = (local.y - Self.inset.height) / max(1, cell)
            let followed = atBottom
            atBottom = false
            zoom = PaneZoom.clamp(zoom: zoom * g.scale, fitted: fitted)
            refit()
            // Keep the point under the fingers where it was.
            let x = terminal.frame.minX + across * terminal.frame.width - screen.x
            let y = Self.inset.height + line * cell - screen.y
            contentOffset = CGPoint(x: min(max(0, x), max(0, contentSize.width - bounds.width)),
                                    y: min(max(0, y), bottom))
            atBottom = followed && contentOffset.y >= bottom - max(4, cell / 2)
            rest()
        default:
            break
        }
    }

    // MARK: A program's swipe

    private func wheelBegins(_ g: UIGestureRecognizer) -> Bool {
        guard swipe == .wheel || swipe == .arrows, let g = g as? UIPanGestureRecognizer else { return false }
        let v = g.velocity(in: self)
        return abs(v.y) > abs(v.x)
    }

    /// Past the grid's top or bottom edge (all of it while the grid fits
    /// the room), a cell's drag is a line for the program.
    @objc private func swiped(_ g: UIPanGestureRecognizer) {
        let ty = g.translation(in: self).y
        switch g.state {
        case .began:
            wheelLast = ty
            wheelCarry = 0
        case .changed:
            let d = ty - wheelLast
            wheelLast = ty
            let y = contentOffset.y
            guard (d > 0 && y <= 0.5) || (d < 0 && y >= bottom - 0.5) else {
                wheelCarry = 0
                return
            }
            wheelCarry += d
            let (n, rest) = PaneScroll.lines(wheelCarry, step: cell)
            wheelCarry = rest
            if n != 0 { scrollProgram(n, at: g.location(in: self)) }
        default:
            break
        }
    }

    /// n lines up (positive) or down for the program, as the Mac's wheel
    /// sends them: wheel ticks at the point, or arrow keys. They go in
    /// through the mirror, which drops them while keys don't.
    private func scrollProgram(_ n: Int, at p: CGPoint) {
        let t = terminal.getTerminal()
        let up = n > 0
        switch swipe {
        case .wheel:
            let width = terminal.frame.width / CGFloat(max(1, t.cols))
            let col = min(t.cols - 1, max(0, Int((p.x - terminal.frame.minX) / max(1, width))))
            let row = min(t.rows - 1, max(0, Int((p.y - terminal.frame.minY) / cell)))
            let flags = t.encodeButton(button: up ? 4 : 5, release: false, shift: false, meta: false, control: false)
            for _ in 0..<abs(n) { t.sendEvent(buttonFlags: flags, x: col, y: row) }
        case .arrows:
            let key = up ? (t.applicationCursor ? EscapeSequences.moveUpApp : EscapeSequences.moveUpNormal)
                : (t.applicationCursor ? EscapeSequences.moveDownApp : EscapeSequences.moveDownNormal)
            for _ in 0..<abs(n) { terminal.send(key) }
        case .history, .none:
            break
        }
    }

    // MARK: VoiceOver

    /// A three-finger swipe: half the pane through the scrollback, or
    /// for the program.
    private func page(_ direction: UIAccessibilityScrollDirection) -> Bool {
        guard let pane, cell > 0 else { return false }
        let up: Bool
        switch direction {
        case .up, .previous: up = true
        case .down, .next: up = false
        default: return false
        }
        let n = PaneScroll.page(rows: pane.rows, alternate: false) ?? 1
        switch swipe {
        case .history:
            let y = contentOffset.y + CGFloat(up ? -n : n) * cell
            let to = min(max(0, y), bottom)
            guard to != contentOffset.y else { return false }
            contentOffset.y = to
            atBottom = to >= bottom - max(4, cell / 2)
            let behind = history - shown
            UIAccessibility.post(notification: .pageScrolled,
                                 argument: back ? "\(behind) line\(behind == 1 ? "" : "s") back" : "At the bottom")
        case .wheel, .arrows:
            scrollProgram(up ? n : -n, at: CGPoint(x: terminal.frame.midX, y: terminal.frame.midY))
            UIAccessibility.post(notification: .pageScrolled, argument: nil)
        case .none:
            return false
        }
        return true
    }

    // MARK: UIScrollViewDelegate

    func scrollViewDidScroll(_ scrollView: UIScrollView) {
        if !placing { place() }
    }

    func scrollViewWillBeginDragging(_ scrollView: UIScrollView) {
        // While the finger is down output doesn't pull the view back.
        atBottom = false
    }

    func scrollViewDidEndDragging(_ scrollView: UIScrollView, willDecelerate decelerate: Bool) {
        if !decelerate { rest() }
    }

    func scrollViewDidEndDecelerating(_ scrollView: UIScrollView) { rest() }

    private func rest() {
        atBottom = contentOffset.y >= bottom - max(4, cell / 2)
    }
}

/// A gesture that goes along with the others, once begins says it may.
final class Simultaneous: NSObject, UIGestureRecognizerDelegate {
    var begins: ((UIGestureRecognizer) -> Bool)?

    func gestureRecognizerShouldBegin(_ g: UIGestureRecognizer) -> Bool { begins?(g) ?? true }

    func gestureRecognizer(_ g: UIGestureRecognizer, shouldRecognizeSimultaneouslyWith other: UIGestureRecognizer) -> Bool { true }
}

/// SwiftTerm's view as a mirror, as the Mac's: its size is the pane's,
/// not its frame's, and the replies its emulator would send the program
/// are dropped (the server's emulator answers them). It takes the
/// keyboard only while keys go in, never pans the program's mouse (the
/// host turns a swipe into its wheel) and tells the host when its
/// scrollback or screen changed.
final class PhoneMirrorView: TerminalView {
    var typing = false {
        didSet { if !typing, isFirstResponder { _ = resignFirstResponder() } }
    }

    /// Lines came, the screen switched or the mouse mode changed, once
    /// a run loop; reset after a full reset or a screen switch, which
    /// put the view at the bottom.
    var onChange: ((_ reset: Bool) -> Void)?
    var onAccessibilityScroll: ((UIAccessibilityScrollDirection) -> Bool)?
    private var change: Bool?

    override var canBecomeFirstResponder: Bool { typing }

    /// Pins the emulator to cols × rows, at fontSize when given, and the
    /// frame to the cells that hold them, and a hair more, so SwiftTerm's
    /// own sizing from the frame keeps the pane's cols and rows.
    func pin(cols: Int, rows: Int, fontSize: CGFloat?) {
        if let fontSize {
            // A font change resizes the emulator from the frame unless
            // the frame is empty: empty it first, and the pane's size
            // stays.
            let origin = frame.origin
            frame.size = .zero
            font = UIFont.monospacedSystemFont(ofSize: fontSize, weight: .regular)
            frame.origin = origin
        }
        let t = getTerminal()
        if t.cols != cols || t.rows != rows {
            t.resize(cols: cols, rows: rows)
        }
        let r = getOptimalFrameSize()
        frame.size = CGSize(width: r.width + 0.5, height: r.height + 0.5)
        // Its own scroll sized for the font.
        scrollTo(row: t.buffer.yDisp, notifyAccessibility: false)
    }

    override func send(source: Terminal, data: ArraySlice<UInt8>) {
        if PaneScroll.isMouseReport(data) {
            terminalDelegate?.send(source: self, data: data)
        }
    }

    override func scrolled(source terminal: Terminal, yDisp: Int) {
        super.scrolled(source: terminal, yDisp: yDisp)
        changed(reset: false)
    }

    override func bufferActivated(source: Terminal) {
        super.bufferActivated(source: source)
        changed(reset: true)
    }

    override func mouseModeChanged(source: Terminal) {
        // Not SwiftTerm's mouse pan: it would take the scroll's swipes.
        changed(reset: false)
    }

    private func changed(reset: Bool) {
        if let c = change {
            change = c || reset
            return
        }
        change = reset
        DispatchQueue.main.async { [weak self] in
            guard let self, let reset = change else { return }
            change = nil
            onChange?(reset)
        }
    }

    override func accessibilityScroll(_ direction: UIAccessibilityScrollDirection) -> Bool {
        onAccessibilityScroll?(direction) ?? false
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

import Foundation

/// The Terminal tab's scrollback, wheel and Shift+PgUp/PgDn, as the
/// TUI's (docs/SPEC.md §3.3): on the main screen of a program that
/// doesn't track the mouse (a shell, Claude's inline mode) the wheel and
/// Shift+PgUp/PgDn scroll the tab's own scrollback, which the first
/// pane.repaint fills; a program that tracks the mouse (Claude's
/// full-screen mode) gets the wheel and the clicks, and a full-screen
/// one the keys. A pane that takes no keys (a paired server's at view)
/// keeps the mouse the tab's.
public enum PaneScroll {
    /// Lines the tab's scrollback keeps: the server emulator's
    /// (emu.DefaultScrollback), as many as the first repaint sends.
    public static let lines = 10_000

    /// Rows Shift+PgUp/PgDn move the scrollback: half the pane, as the
    /// TUI's; nil on the alternate screen, where the program gets the key.
    public static func page(rows: Int, alternate: Bool) -> Int? {
        alternate ? nil : max(1, rows / 2)
    }

    /// What a vertical swipe on the phone's terminal moves (T861), as the
    /// Mac's wheel does: the mirror's own scrollback on the main screen;
    /// the program's wheel when it tracks the mouse; the arrow keys on the
    /// alternate screen (alternate scroll, DECSET 1007, as SwiftTerm's
    /// wheel), nothing with it reset. Only while keys go in: below the
    /// write level a swipe sends nothing and moves the scrollback alone.
    public enum Swipe: Equatable {
        case history, wheel, arrows, none
    }

    public static func swipe(alternate: Bool, mouse: Bool, alternateScroll: Bool, keys: Bool) -> Swipe {
        if keys && mouse { return .wheel }
        if alternate { return keys && alternateScroll ? .arrows : .none }
        return .history
    }

    /// Whole lines in a drag of distance points at step points a line,
    /// and what is left for the next: 1:1, as the Mac's trackpad.
    public static func lines(_ distance: CGFloat, step: CGFloat) -> (lines: Int, rest: CGFloat) {
        guard step > 0 else { return (0, 0) }
        let n = Int(distance / step)
        return (n, distance - CGFloat(n) * step)
    }

    /// The phone's scrollback as one scroll with the screen below it:
    /// the first row the mirror shows at a scroll offset, history rows of
    /// cell points above the screen (the screen's own row is history).
    public static func row(offset: CGFloat, cell: CGFloat, history: Int) -> Int {
        guard cell > 0 else { return history }
        return min(history, max(0, Int((offset / cell).rounded(.down))))
    }

    /// Whether that offset reads the scrollback, more than half a row up
    /// from the screen: the jump-to-bottom button shows.
    public static func back(offset: CGFloat, cell: CGFloat, history: Int) -> Bool {
        history > 0 && offset < CGFloat(history) * cell - cell / 2
    }

    /// Whether bytes the mirror's emulator sends are a mouse report, the
    /// human's, which go to the program, rather than a reply to it, which
    /// the server's emulator answers: CSI M (X10, UTF-8), CSI < (SGR) or
    /// CSI b;x;y M (urxvt), CSI as ESC [ or 8-bit.
    public static func isMouseReport(_ data: some Collection<UInt8>) -> Bool {
        let b = Array(data)
        let body: ArraySlice<UInt8>
        if b.starts(with: [0x1b, 0x5b]) {
            body = b.dropFirst(2)
        } else if b.first == 0x9b {
            body = b.dropFirst()
        } else {
            return false
        }
        guard let first = body.first else { return false }
        if first == UInt8(ascii: "M") || first == UInt8(ascii: "<") {
            return body.count > 1
        }
        // urxvt: three numbers, then M.
        guard body.last == UInt8(ascii: "M") else { return false }
        let params = body.dropLast().split(separator: UInt8(ascii: ";"), omittingEmptySubsequences: false)
        return params.count == 3 && params.allSatisfy { p in !p.isEmpty && p.allSatisfy { (0x30...0x39).contains($0) } }
    }
}

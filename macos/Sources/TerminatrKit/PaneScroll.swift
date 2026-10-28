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

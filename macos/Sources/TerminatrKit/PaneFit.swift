import Foundation

/// When the Terminal tab claims its pane's size (docs/SPEC.md §3.8):
/// the window's size once the stream's first size is known, again when
/// the tab's room changes and the change settles (a live drag claims
/// once, at its end), and on keys, as typing in a console does. Never
/// while keys are off (a paired server's pane at view) or no stream is
/// open, and once per size, so a pane.resize from another client isn't
/// fought: the last user wins, as in tmux.
public struct PaneFit: Sendable {
    /// How long the room stays put before a change of it claims.
    public static let settle: Duration = .milliseconds(200)

    /// The size this tab last claimed on its stream.
    public private(set) var claimed: AppAPI.PaneSize?
    private var moves = 0

    public init() {}

    /// A new stream: its first fit claims again.
    public mutating func opened() {
        claimed = nil
    }

    /// The room changed: the change's number, which settled(_:) takes
    /// once the settle time passed.
    public mutating func moved() -> Int {
        moves += 1
        return moves
    }

    /// Whether no change of the room came after move.
    public func settled(_ move: Int) -> Bool {
        move == moves
    }

    /// The size to claim for the room's fitting while the pane has size,
    /// nil for none: not with keys off, no stream open, either size
    /// unknown, the pane at it already, or once claimed.
    public mutating func claim(fitting: AppAPI.PaneSize?, pane size: AppAPI.PaneSize?, keys: Bool, open: Bool) -> AppAPI.PaneSize? {
        guard keys, open, let fitting, let size, fitting != size, fitting != claimed else { return nil }
        claimed = fitting
        return fitting
    }
}

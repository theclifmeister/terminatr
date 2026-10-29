import SwiftUI
import TerminatrKit
import TerminatrUI

extension Attention {
    /// A paired server's session's, from what that server lists of it.
    static func of(peer s: AppAPI.PeerSession?) -> Attention {
        guard let s else { return Attention(word: "not running", tone: .idle, why: "", waits: false) }
        if s.asking { return Attention(word: "asks", tone: .question, why: "asks a question", waits: true) }
        if s.state == "blocked" && s.reason == "permission" {
            return Attention(word: "allow?", tone: .approval, why: "needs approval", waits: true)
        }
        switch s.state {
        case "working", "": return Attention(word: s.state.isEmpty ? "running" : "working", tone: .working, why: s.current, waits: false)
        case "blocked": return Attention(word: s.reason.isEmpty ? "blocked" : s.reason, tone: .attention, why: s.current, waits: false)
        default: return Attention(word: s.state, tone: .idle, why: s.current, waits: false)
        }
    }
}

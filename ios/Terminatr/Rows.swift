import SwiftUI
import TerminatrKit
import TerminatrUI

/// A project's row, as the Mac sidebar's: its coordinator's state as
/// the dot, its name, the state in words and its needs as the badge.
struct ProjectLine: View {
    @Environment(\.theme) private var theme
    let name: String
    let state: Attention
    let needs: Int
    var dim = false

    var body: some View {
        HStack(spacing: 10) {
            Circle()
                .fill(theme.colour(state.tone))
                .frame(width: 8, height: 8)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 1) {
                Text(name)
                    .font(theme.body(.body, .semibold))
                Text(state.word)
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.muted)
            }
            Spacer(minLength: 4)
            if needs > 0 {
                Text("\(needs)")
                    .font(theme.mono(.caption, .medium))
                    .padding(.horizontal, 7)
                    .padding(.vertical, 2)
                    .background(theme.attention, in: Capsule())
                    .foregroundStyle(theme.onAttention)
            }
        }
        .opacity(dim ? 0.55 : 1)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(name)
        .accessibilityValue(needs > 0 ? "\(state.spoken), \(needs) need you" : state.spoken)
    }
}

/// A session's row: its dot, task ref and title, and what waits on the
/// user as a filled pill (allow?, asks, review…), else its PR in plain
/// muted text, as the Mac sidebar's rows. A working one (busy) has the
/// activity mark for its dot and how long its turn has run. At an
/// accessibility text size the pills go under the title, which keeps
/// the row's width.
struct SessionLine: View {
    @Environment(\.theme) private var theme
    @Environment(\.dynamicTypeSize) private var typeSize
    let label: String
    let ref: String?
    let state: Attention
    var busy: Busy?
    var pr = 0
    var dormant = false

    var body: some View {
        let busy = dormant ? nil : busy
        let stacked = typeSize.isAccessibilitySize
        HStack(spacing: 8) {
            StateDot(tone: dormant ? .idle : state.tone, busy: busy != nil, size: 7)
            if let ref {
                Text(ref)
                    .font(theme.mono(.subheadline, .medium))
                    .foregroundStyle(theme.action)
            }
            VStack(alignment: .leading, spacing: 1) {
                Text(label)
                    .font(theme.body(.body))
                    .lines(1)
                if !state.why.isEmpty, !dormant {
                    Text(state.why)
                        .font(theme.body(.footnote))
                        .foregroundStyle(theme.muted)
                        .lines(1)
                }
                if stacked {
                    HStack(spacing: 8) { pills(busy) }
                }
            }
            Spacer(minLength: 4)
            if !stacked {
                pills(busy)
            }
        }
        .foregroundStyle(dormant ? theme.muted : theme.ink)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel([ref, label].compactMap { $0 }.joined(separator: " "))
        .accessibilityValue(dormant ? "inactive" : [busy.map { $0.spoken(now: Date()) } ?? state.spoken, pr > 0 ? "PR \(pr)" : nil]
            .compactMap { $0 }.joined(separator: ", "))
    }

    /// How long its turn has run, and what waits on the user or its PR.
    @ViewBuilder private func pills(_ busy: Busy?) -> some View {
        if let busy {
            BusyLabel(busy: busy).fixedSize()
        }
        if !dormant, state.waits {
            Text(state.word)
                .font(theme.mono(.caption2, .medium))
                .padding(.horizontal, 6)
                .padding(.vertical, 2)
                .background(theme.colour(state.tone), in: Capsule())
                .foregroundStyle(theme.on(state.tone))
        } else if !dormant, pr > 0 {
            Text("PR")
                .font(theme.mono(.caption2, .medium))
                .foregroundStyle(theme.muted)
        }
    }
}

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

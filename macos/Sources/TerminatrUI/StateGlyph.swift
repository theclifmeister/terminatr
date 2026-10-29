import SwiftUI
import TerminatrKit

/// A state's glyph (T845): a squircle with the state's symbol, so shape
/// and symbol carry the state and colour is never the only sign. Filled
/// in the tone, its symbol in the tone's ink, when the state waits on
/// the user; soft otherwise, a wash of the tone with a ring and the
/// symbol in ink. Working turns an arc (BusyMark's clock), still with
/// Reduce Motion. A state change morphs the symbol.
public struct StateGlyph: View {
    @Environment(\.theme) private var theme
    @Environment(\.accessibilityReduceMotion) private var still
    let tone: Attention.Tone
    let filled: Bool
    /// The side at 100%; the theme's scale grows it with Dynamic Type.
    let side: CGFloat
    /// The symbol in place of the tone's: a need's kind's.
    let symbolName: String?

    /// filled nil: filled when the tone waits on the user.
    public init(_ tone: Attention.Tone, filled: Bool? = nil, side: CGFloat = 38, symbol: String? = nil) {
        self.tone = tone
        self.symbolName = symbol
        self.filled = filled ?? tone.waits
        self.side = side
    }

    public var body: some View {
        let s = side * theme.scale
        let shape = RoundedRectangle(cornerRadius: Radius.glyph * s / 38, style: .continuous)
        ZStack {
            if filled {
                shape.fill(theme.colour(tone))
            } else {
                shape.fill(theme.wash(tone))
                shape.strokeBorder(theme.colour(tone).opacity(theme.contrast ? 1 : 0.45), lineWidth: 1)
            }
            symbol(s)
        }
        .frame(width: s, height: s)
        .animation(still ? nil : .easeInOut(duration: 0.25), value: tone)
        .accessibilityElement()
        .accessibilityLabel(tone.spoken)
    }

    @ViewBuilder private func symbol(_ s: CGFloat) -> some View {
        let ink = filled ? theme.on(tone) : theme.ink
        if tone == .working && symbolName == nil && !still {
            TimelineView(.animation(minimumInterval: 1 / 30)) { tl in
                Circle()
                    .trim(from: 0, to: 0.75)
                    .stroke(ink, style: StrokeStyle(lineWidth: s / 16, lineCap: .round))
                    .rotationEffect(.degrees(BusyMark.angle(at: tl.date)))
                    .frame(width: s * 0.44, height: s * 0.44)
            }
        } else {
            Image(systemName: symbolName ?? tone.symbol)
                .font(.system(size: s * 0.46, weight: .semibold))
                .foregroundStyle(ink)
                .contentTransition(.symbolEffect(.replace))
        }
    }
}

extension Attention.Tone {
    /// Its glyph's SF Symbol (working's is an arc; this is it still).
    public var symbol: String {
        switch self {
        case .approval: "hand.raised.fill"
        case .question: "questionmark.bubble.fill"
        case .attention: "exclamationmark.triangle.fill"
        case .review: "checkmark.seal.fill"
        case .working: "circle.dotted"
        case .idle: "moon.fill"
        case .done: "checkmark"
        }
    }
}

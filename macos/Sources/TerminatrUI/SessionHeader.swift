import SwiftUI
import TerminatrKit

/// A session's title in its header (T848, direction A "Glass deck"): the
/// thread's title in the display face, and under it where it is ("T14 ·
/// acme-web") in mono with its state's pill (ToneBadge), on a glass
/// capsule, so no line of it sits on the raw mesh. The bar is short, so
/// its text grows with Dynamic Type only to a point; held, the large
/// content viewer shows it whole. VoiceOver reads it as one header.
public struct SessionTitle: View {
    @Environment(\.theme) private var theme
    let title: String
    let place: String
    let state: Attention?

    /// The most the bar's text grows.
    static let most: CGFloat = 1.3

    public init(_ title: String, place: String, state: Attention? = nil) {
        self.title = title
        self.place = place
        self.state = state
    }

    public var body: some View {
        var t = theme
        t.scale = min(theme.scale, Self.most)
        return VStack(spacing: 1) {
            Text(title)
                .font(t.title(.headline, .bold))
                .foregroundStyle(t.ink)
                .lineLimit(1)
            HStack(spacing: Spacing.tight) {
                if !place.isEmpty {
                    Text(place)
                        .font(t.mono(.caption, .medium))
                        .foregroundStyle(t.muted)
                        .lineLimit(1)
                }
                if let state {
                    ToneBadge(state.word, tone: state.tone, compact: true)
                }
            }
        }
        .environment(\.theme, t)
        .padding(.horizontal, Spacing.card)
        .padding(.vertical, 3)
        .surface(.glass, in: Capsule())
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(spoken)
        .accessibilityAddTraits(.isHeader)
        .accessibilityShowsLargeContentViewer {
            Text(title)
        }
    }

    private var spoken: String {
        [title, place.replacingOccurrences(of: " · ", with: ", "), state?.word ?? ""]
            .filter { !$0.isEmpty }.joined(separator: ", ")
    }
}

/// The Conversation / Terminal switch (T840) as the header's glass
/// capsule (T848): two symbols, each a 44 pt target, the shown one
/// filled in ink with the base's symbol (a picked chip's pairing). A
/// switch is a selection tap. framed false leaves the capsule to what
/// holds it: iOS 26's bar draws its items' glass itself.
public struct SessionToggle: View {
    @Environment(\.theme) private var theme
    @Environment(\.accessibilityReduceMotion) private var still
    @Binding var selection: SessionTab
    let framed: Bool

    public init(selection: Binding<SessionTab>, framed: Bool = true) {
        _selection = selection
        self.framed = framed
    }

    public var body: some View {
        HStack(spacing: 2) {
            ForEach(SessionTab.allCases, id: \.self) { t in
                let on = t == selection
                Button {
                    withAnimation(still ? nil : .snappy(duration: 0.2)) { selection = t }
                } label: {
                    Image(systemName: Self.symbol(t))
                        .font(.system(size: 16 * min(theme.scale, SessionTitle.most), weight: .semibold))
                        .foregroundStyle(on ? theme.base : theme.ink)
                        .frame(width: 36, height: 36)
                        .background { if on { Circle().fill(theme.ink) } }
                        .frame(width: tapTarget, height: tapTarget)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel(t.rawValue)
                .accessibilityAddTraits(on ? .isSelected : [])
            }
        }
        .padding(.horizontal, framed ? 2 : 0)
        .background { if framed { Color.clear.surface(.glass, in: Capsule()) } }
        .feedback(.selection, trigger: selection)
        .accessibilityElement(children: .contain)
        .accessibilityLabel("View")
    }

    public static func symbol(_ t: SessionTab) -> String {
        t == .conversation ? "text.bubble" : "terminal"
    }
}

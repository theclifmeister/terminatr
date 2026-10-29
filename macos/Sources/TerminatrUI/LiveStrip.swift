import SwiftUI
import TerminatrKit

/// The work running on the server (T849), in a strip above the phone's
/// tab bar: the turning mark, "2 working · T16 reading cart.spec.ts"
/// and a three-bar meter; a tap opens the session that has worked
/// longest (LiveWork). On the glass ("the live strip" in
/// ThemeColours.checks), which also covers the tab bar accessory's own.
/// Compact (the accessory beside a minimised tab bar) it says only how
/// many work. With Reduce Motion the mark and the meter stand still.
public struct LiveStrip: View {
    @Environment(\.theme) private var theme
    @Environment(\.accessibilityReduceMotion) private var still
    let work: LiveWork
    let compact: Bool
    let open: () -> Void

    public init(_ work: LiveWork, compact: Bool = false, open: @escaping () -> Void) {
        self.work = work
        self.compact = compact
        self.open = open
    }

    public var body: some View {
        Button(action: open) {
            HStack(spacing: Spacing.item) {
                BusyMark(size: 8)
                // One line at any text size: the accessory's height is
                // the tab bar's, and VoiceOver reads it whole.
                Text(work.word)
                    .font(theme.body(.subheadline, .semibold))
                    .foregroundStyle(theme.ink)
                    .lineLimit(1)
                    .layoutPriority(1)
                if !compact {
                    Text("· " + work.what)
                        .font(theme.mono(.caption))
                        .foregroundStyle(theme.muted)
                        .lineLimit(1)
                }
                Spacer(minLength: 0)
                if !compact { meter }
            }
            .padding(.leading, Spacing.card)
            .padding(.trailing, Spacing.card - 2)
            .frame(maxWidth: .infinity, minHeight: tapTarget)
            .surface(.glass, in: Capsule())
            .contentShape(Capsule())
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(work.word)
        .accessibilityValue(work.what)
        .accessibilityHint("Opens \(work.who)'s conversation")
    }

    /// Three bars in the action colour, rising and falling while the
    /// work runs.
    private var meter: some View {
        TimelineView(.animation(minimumInterval: 1 / 15, paused: still)) { tl in
            HStack(alignment: .bottom, spacing: 3) {
                ForEach(0..<3) { i in
                    Capsule()
                        .fill(theme.action.opacity([0.4, 0.7, 1][i]))
                        .frame(width: 5, height: Self.height(i, at: still ? nil : tl.date))
                }
            }
            .frame(height: 22, alignment: .bottom)
        }
        .accessibilityHidden(true)
    }

    /// A bar's height, 8 to 22 pt, each on its own beat; still, the
    /// design's 18, 14 and 22.
    static func height(_ i: Int, at d: Date?) -> CGFloat {
        guard let d else { return [18, 14, 22][i] }
        let t = d.timeIntervalSinceReferenceDate * [2.1, 2.9, 1.7][i]
        return 15 + 7 * CGFloat(sin(t))
    }
}

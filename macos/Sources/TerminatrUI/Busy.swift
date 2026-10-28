import SwiftUI
import TerminatrKit

/// A working session's mark in place of its dot (T803): an arc that
/// turns around the dot, in the working tone. With Reduce Motion it
/// stands still, a ring around the dot, still unlike an idle dot.
public struct BusyMark: View {
    @Environment(\.theme) private var theme
    @Environment(\.accessibilityReduceMotion) private var still
    /// The dot's diameter; the ring goes around it.
    let size: CGFloat
    var colour: Color?

    public init(size: CGFloat, colour: Color? = nil) {
        self.size = size
        self.colour = colour
    }

    public var body: some View {
        let c = colour ?? theme.colour(.working)
        let ring = size + 5
        ZStack {
            Circle()
                .fill(c)
                .frame(width: size * 0.6, height: size * 0.6)
            if still {
                Circle()
                    .strokeBorder(c, lineWidth: 1.2)
            } else {
                // The clock drives the turn: an animation started on
                // appear stops in a List's reused rows.
                TimelineView(.animation(minimumInterval: 1 / 30)) { tl in
                    Circle()
                        .trim(from: 0, to: 0.7)
                        .stroke(c, style: StrokeStyle(lineWidth: 1.5, lineCap: .round))
                        .rotationEffect(.degrees(Self.angle(at: tl.date)))
                }
            }
        }
        .frame(width: ring, height: ring)
        .accessibilityHidden(true)
    }

    /// One turn a second.
    static func angle(at d: Date) -> Double {
        d.timeIntervalSinceReferenceDate.truncatingRemainder(dividingBy: 1) * 360
    }
}

/// A card's state dot in its tone, or while its session works the
/// activity mark (BusyMark) in the dot's place, at the dot's size.
public struct StateDot: View {
    @Environment(\.theme) private var theme
    let tone: Attention.Tone
    let busy: Bool
    var size: CGFloat = 8

    public init(tone: Attention.Tone, busy: Bool, size: CGFloat = 8) {
        self.tone = tone
        self.busy = busy
        self.size = size
    }

    public var body: some View {
        if busy {
            // The ring stands out of the dot's frame, so text after it
            // stays where it was.
            BusyMark(size: size)
                .frame(width: size, height: size)
        } else {
            Circle()
                .fill(theme.colour(tone))
                .frame(width: size, height: size)
                .accessibilityHidden(true)
        }
    }
}

/// "working · 3m", kept current: a row's and a card's words for a
/// working session.
public struct BusyLabel: View {
    @Environment(\.theme) private var theme
    let busy: Busy
    var colour: Color?

    public init(busy: Busy, colour: Color? = nil) {
        self.busy = busy
        self.colour = colour
    }

    public var body: some View {
        TimelineView(.periodic(from: .now, by: 1)) { tl in
            Text(busy.word(now: tl.date))
                .font(theme.mono(.caption2, .medium))
                .foregroundStyle(colour ?? theme.muted)
                .monospacedDigit()
                .lineLimit(1)
                .accessibilityLabel(busy.spoken(now: tl.date))
        }
    }
}

/// The conversation's last line while the agent has its turn: the mark,
/// Working…, how long the turn has run and what the agent does now
/// (Activity). The conversation shows it only while the session is
/// working.
public struct WorkingBar: View {
    @Environment(\.theme) private var theme
    let busy: Busy
    let activity: String

    public init(busy: Busy, activity: String) {
        self.busy = busy
        self.activity = activity
    }

    public var body: some View {
        TimelineView(.periodic(from: .now, by: 1)) { tl in
            HStack(alignment: .center, spacing: 8) {
                BusyMark(size: 8)
                Text("Working…")
                    .font(theme.body(.subheadline, .semibold))
                    .foregroundStyle(theme.colour(.working))
                    .fixedSize()
                if let t = busy.clock(now: tl.date) {
                    Text(t)
                        .font(theme.mono(.footnote, .medium))
                        .foregroundStyle(theme.muted)
                        .monospacedDigit()
                        .fixedSize()
                }
                Text(activity)
                    .font(theme.mono(.footnote))
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
                    .truncationMode(.middle)
                Spacer(minLength: 0)
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 7)
            .background(theme.command, in: Capsule())
            .overlay(Capsule().strokeBorder(theme.edge, lineWidth: 1))
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(busy.spoken(now: tl.date))
            .accessibilityValue(activity)
        }
    }

}

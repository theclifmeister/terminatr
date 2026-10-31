import SwiftUI
import TerminatrKit
import TerminatrUI

/// The conversation's end after its last entry (T893), as the Claude
/// app shows it: the prompts this phone queued while the agent works,
/// each as the user's own bubble marked queued until it goes in
/// (QueuedPrompt.waiting), a line for prompts queued elsewhere, and
/// while the agent has its turn the working line under them.
struct ConversationTail: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    let ref: SessionRef
    let session: AppAPI.Session
    let transcript: Transcript

    var body: some View {
        let waiting = QueuedPrompt.waiting(model.queuedSent[ref] ?? [],
                                           prompts: transcript.entries.filter { $0.kind == .prompt }.map { ($0.text, $0.at) },
                                           queued: session.queuedPrompts)
        let elsewhere = session.queuedPrompts - waiting.count
        // A ZStack: its .onChange runs with none of these showing.
        ZStack {
            VStack(alignment: .leading, spacing: 12) {
                ForEach(waiting) { QueuedBubble(prompt: $0) }
                if elsewhere > 0 {
                    Text(elsewhere == 1 ? "1 more prompt queued" : "\(elsewhere) more prompts queued")
                        .font(theme.body(.footnote, .medium))
                        .foregroundStyle(theme.muted)
                        .onGlass()
                        .frame(maxWidth: .infinity, alignment: .trailing)
                }
                if let busy = Busy.of(session) {
                    WorkingLine(busy: busy, activity: Activity.of(transcript, current: session.current))
                }
            }
        }
        // Those that went in are forgotten.
        .onAppear { model.keepQueued(ref, waiting) }
        .onChange(of: waiting.map(\.id)) { model.keepQueued(ref, waiting) }
    }
}

/// A prompt waiting in the queue (T893): the user's bubble, faded, with
/// "queued" over it, its files counted under it.
struct QueuedBubble: View {
    @Environment(\.theme) private var theme
    let prompt: QueuedPrompt

    var body: some View {
        HStack {
            Spacer(minLength: 40)
            VStack(alignment: .trailing, spacing: 4) {
                Label("queued", systemImage: "clock")
                    .font(theme.mono(.caption, .medium))
                    .foregroundStyle(theme.muted)
                    .onGlass()
                Text(prompt.text.isEmpty ? "\(prompt.files) files" : prompt.text)
                    .foregroundStyle(theme.onPrompt)
                    .padding(.horizontal, 13)
                    .padding(.vertical, 9)
                    .background(LinearGradient(colors: theme.prompt, startPoint: .topLeading, endPoint: .bottomTrailing)
                        .opacity(0.55), in: RoundedRectangle(cornerRadius: 14))
                    .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(theme.edge, style: StrokeStyle(lineWidth: 1, dash: [4, 3])))
                if prompt.files > 0, !prompt.text.isEmpty {
                    Text(prompt.files == 1 ? "1 file" : "\(prompt.files) files")
                        .font(theme.body(.caption))
                        .foregroundStyle(theme.muted)
                        .onGlass()
                }
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Queued: \(prompt.text)")
    }
}

/// The agent's turn under the conversation's last message (T893, the
/// Claude app's "• • Working…"): dots that pulse in turn, Working… in
/// the working colour, the turn's time and what it does now, muted.
/// With Reduce Motion the dots hold still.
struct WorkingLine: View {
    @Environment(\.theme) private var theme
    @Environment(\.accessibilityReduceMotion) private var still
    let busy: Busy
    let activity: String

    var body: some View {
        TimelineView(.periodic(from: .now, by: 1)) { tl in
            HStack(alignment: .firstTextBaseline, spacing: Spacing.item) {
                dots
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
            }
            .onGlass()
            .padding(.leading, 12)
            .padding(.vertical, Spacing.hair)
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(busy.spoken(now: tl.date))
            .accessibilityValue(activity)
        }
    }

    private var dots: some View {
        let c = theme.colour(.working)
        return TimelineView(.animation(minimumInterval: 1 / 20, paused: still)) { tl in
            let t = tl.date.timeIntervalSinceReferenceDate
            HStack(spacing: 4) {
                ForEach(0..<3, id: \.self) { i in
                    let phase = still ? 1 : (sin((t * 2 * .pi / 1.2) - Double(i) * 0.9) + 1) / 2
                    Circle()
                        .fill(c)
                        .frame(width: 6, height: 6)
                        .opacity(0.3 + 0.7 * phase)
                        .scaleEffect(0.8 + 0.25 * phase)
                }
            }
        }
        // The dots hold no text, so their baseline is their bottom edge and
        // they would sit low: put their middle at the middle of the capitals
        // of the font "Working…" is set in (theme.body, SF at the theme's size).
        .alignmentGuide(.firstTextBaseline) { _ in
            3 + UIFont.systemFont(ofSize: theme.size(.subheadline), weight: .semibold).capHeight / 2
        }
        .accessibilityHidden(true)
    }
}

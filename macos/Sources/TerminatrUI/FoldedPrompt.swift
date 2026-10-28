import SwiftUI
import TerminatrKit

/// A prompt tm sent (Entry.folds: the thread's kickoff, the server's
/// [tm] notes) as one muted line the user taps to open, instead of the
/// user's own bubble. The Mac's conversation and the phone's both draw it.
public struct FoldedPrompt: View {
    @Environment(\.theme) private var theme
    let entry: Transcript.Entry
    let when: String
    let open: () -> Void

    public init(entry: Transcript.Entry, when: String, open: @escaping () -> Void) {
        self.entry = entry
        self.when = when
        self.open = open
    }

    public var body: some View {
        Button(action: open) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Image(systemName: "chevron.right")
                    .font(theme.body(.glyph, .bold))
                Text("tm")
                    .font(theme.mono(.footnote, .medium))
                Text([entry.headline, when].filter { !$0.isEmpty }.joined(separator: " · "))
                    .font(theme.body(.subheadline))
                    .lines(1)
                Spacer(minLength: 0)
            }
            .foregroundStyle(theme.muted)
            .padding(.horizontal, 12)
            .padding(.vertical, 6)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help("Show what tm sent")
        .accessibilityHint("Shows the prompt tm sent")
    }
}

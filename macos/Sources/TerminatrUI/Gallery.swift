import SwiftUI
import TerminatrKit

/// The redesign's shared pieces (T845) on one page, over the backdrop:
/// the previews', and the phone's demo (-open gallery) for screenshots
/// in each theme. No screen uses it.
public struct ComponentGallery: View {
    @Environment(\.theme) private var theme
    @State private var filter = "all"
    @State private var picked = 0
    /// Opened at the states and buttons, not the top: the Simulator
    /// can't scroll for a screenshot.
    let lower: Bool

    public init(lower: Bool = false) {
        self.lower = lower
    }

    public var body: some View {
        ScrollViewReader { scroller in
            page.onAppear { if lower { scroller.scrollTo("states", anchor: .top) } }
        }
    }

    private var page: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Spacing.card) {
                ScreenHeader("Needs you", count: 5, subtitle: "clifford's Mac · linked, answers here", symbol: "wifi") {
                    Button {} label: { Label("Search", systemImage: "magnifyingglass") }
                        .buttonStyle(.circle)
                    Button {} label: { Label("Settings", systemImage: "gearshape") }
                        .buttonStyle(.circle)
                }
                PulseChips([
                    PulseChip(id: "all", label: "All 5"),
                    PulseChip(id: "approval", label: "1 approval", tone: .approval),
                    PulseChip(id: "question", label: "2 asks", tone: .question),
                    PulseChip(id: "blocked", label: "1 blocked", tone: .attention),
                ], selection: $filter)
                VStack(spacing: Spacing.card) {
                    approval
                    question
                    glyphs.id("states")
                    buttons
                }
                .padding(.horizontal, Spacing.card)
            }
            .padding(.bottom, 40)
        }
        .background(Backdrop())
    }

    private func head(_ tone: Attention.Tone, _ what: String, _ ref: String, _ age: String) -> some View {
        HStack(spacing: 11) {
            StateGlyph(tone)
            VStack(alignment: .leading, spacing: 2) {
                Text(what).font(theme.body(.headline, .bold)).foregroundStyle(theme.ink)
                Text(ref).font(theme.mono(.subheadline, .medium)).foregroundStyle(theme.muted)
            }
            Spacer(minLength: 0)
            Text(age).font(theme.mono(.subheadline, .medium)).foregroundStyle(theme.muted)
        }
    }

    private var approval: some View {
        VStack(alignment: .leading, spacing: 11) {
            head(.approval, "Approval", "acme-web · T12", "2m")
            Text("Checkout: Apple Pay button")
                .font(theme.body(.title2, .semibold)).foregroundStyle(theme.ink)
            Text("Run the checkout tests against the Stripe sandbox?")
                .font(theme.body(.body)).foregroundStyle(theme.muted)
            Text("$ npm run test:checkout -- --sandbox")
                .font(theme.mono(.callout)).foregroundStyle(theme.ink)
                .padding(.horizontal, 10).padding(.vertical, 8)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(theme.command, in: RoundedRectangle(cornerRadius: 10))
            HStack(spacing: Spacing.item) {
                Button {} label: { HStack(spacing: 7) { Keycap("Y", tone: .approval, inverted: true); Text("Allow") } }
                    .buttonStyle(.primary(.approval))
                Button {} label: { HStack(spacing: 7) { Keycap("N", tone: .approval); Text("Deny") } }
                    .buttonStyle(.secondary)
            }
        }
        .padding(Spacing.card)
        .surface(.raised, in: RoundedRectangle(cornerRadius: Radius.card, style: .continuous))
    }

    private var question: some View {
        VStack(alignment: .leading, spacing: 11) {
            head(.question, "Asks", "acme-web · T14", "6m")
            Text("Which ranking should search use for products with no sales yet?")
                .font(theme.body(.title2, .semibold)).foregroundStyle(theme.ink)
                .fixedSize(horizontal: false, vertical: true)
            ForEach(Array(Self.options.enumerated()), id: \.offset) { i, o in
                AnswerOption(key: i + 1, label: o.0, detail: o.1, picked: picked == i + 1) { picked = i + 1 }
            }
        }
        .padding(Spacing.card)
        .surface(.card, in: RoundedRectangle(cornerRadius: Radius.card, style: .continuous))
    }

    private static let options = [
        ("Newest first", "New products surface until they have sales data."),
        ("By rating", "Uses review scores; unrated ones go last."),
        ("Keep current", "Alphabetical, as today."),
    ]

    private static let tones: [(Attention.Tone, String)] = [
        (.approval, "allow?"), (.question, "asks"), (.attention, "blocked"), (.review, "review"),
        (.working, "working"), (.idle, "idle"), (.done, "done"),
    ]

    private var glyphs: some View {
        VStack(alignment: .leading, spacing: Spacing.row) {
            Text("States").font(theme.title(.title, .bold)).foregroundStyle(theme.ink)
            Flow(spacing: Spacing.row) {
                ForEach(Self.tones, id: \.1) { t, word in
                    VStack(spacing: Spacing.tight) {
                        StateGlyph(t)
                        ToneBadge(word, tone: t)
                    }
                }
            }
            HStack(spacing: Spacing.card) {
                CountBadge(5)
                NeedsOrb(3)
                StateGlyph(.question, filled: false, side: 28)
                StateGlyph(.review, side: 28)
            }
        }
        .padding(Spacing.card)
        .frame(maxWidth: .infinity, alignment: .leading)
        .surface(.card, in: RoundedRectangle(cornerRadius: Radius.card, style: .continuous))
    }

    private var buttons: some View {
        VStack(alignment: .leading, spacing: Spacing.row) {
            Text("Buttons").font(theme.title(.title, .bold)).foregroundStyle(theme.ink)
            HStack(spacing: Spacing.item) {
                Button("Answer") {}.buttonStyle(.primary())
                Button("Later") {}.buttonStyle(.secondary)
            }
            HStack(spacing: Spacing.item) {
                Button("Accept") {}.buttonStyle(.primary(.review))
                Button("Send back") {}.buttonStyle(.secondary)
            }
            HStack(spacing: Spacing.item) {
                Button("Open the thread") {}.buttonStyle(.ghost)
                Spacer()
                Button {} label: { Label("Add", systemImage: "plus") }.buttonStyle(.circle)
                Button("Off") {}.buttonStyle(.primary()).disabled(true).frame(width: 90)
            }
        }
        .padding(Spacing.card)
        .surface(.card, in: RoundedRectangle(cornerRadius: Radius.card, style: .continuous))
    }
}

#Preview("Bloom light") {
    ComponentGallery().environment(\.theme, .named(.bloom)).preferredColorScheme(.light)
}

#Preview("Bloom dark") {
    ComponentGallery().environment(\.theme, .named(.bloom)).preferredColorScheme(.dark)
}

#Preview("Graphite, Increase Contrast") {
    ComponentGallery().environment(\.theme, Theme.named(.graphite).increasedContrast())
}

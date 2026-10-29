import SwiftUI
import TerminatrKit

/// The redesign's shared pieces (T845) on one page, over the backdrop:
/// the previews', and the phone's demo (-open gallery) for screenshots
/// in each theme. No screen uses it.
public struct ComponentGallery: View {
    @Environment(\.theme) private var theme
    @State private var filter = "all"
    @State private var picked = 0
    @State private var tab = SessionTab.conversation
    @State private var steer = false
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
                    paired
                    glyphs.id("states")
                    buttons
                    session
                }
                .padding(.horizontal, Spacing.card)
            }
            .padding(.bottom, 40)
        }
        .background(Backdrop())
    }

    private static let now = Date()

    private var approval: some View {
        let need = Need(id: "approval:s-12", kind: .approval, project: "acme-web", what: "T12 needs approval",
                        detail: "Checkout: Apple Pay button", text: "Run the checkout tests against the Stripe sandbox?",
                        session: "s-12", task: "T12", since: Self.now.addingTimeInterval(-120))
        return NeedCard(need: need, command: "$ npm run test:checkout -- --sandbox", lead: true, now: Self.now) {
            HStack(spacing: Spacing.item) {
                Button {} label: { HStack(spacing: 7) { Keycap("Y", tone: .approval, inverted: true); Text("Allow") } }
                    .buttonStyle(.primary(.approval))
                    .answerKey("y")
                Button {} label: { HStack(spacing: 7) { Keycap("N", tone: .approval); Text("Deny") } }
                    .buttonStyle(.secondary)
                    .answerKey("n")
            }
        }
    }

    /// A paired server's need: answered there, so no buttons and a soft glyph.
    private var paired: some View {
        let need = Need(id: "paired:mac/billing-api:n1", kind: .network, project: "mac/billing-api",
                        what: "studio-mac › billing-api: T3 asks a question", detail: "Invoice PDFs · answer on studio-mac",
                        since: Self.now.addingTimeInterval(-840))
        return NeedCard(need: need, now: Self.now) { EmptyView() }
    }

    private var question: some View {
        let need = Need(id: "question:s-14", kind: .question, project: "acme-web", what: "T14 asks a question",
                        detail: "Product search ranking",
                        text: "Which ranking should search use for products with no sales yet?",
                        session: "s-14", task: "T14", since: Self.now.addingTimeInterval(-360))
        return NeedCard(need: need, now: Self.now) {
            VStack(spacing: Spacing.item) {
                ForEach(Array(Self.options.enumerated()), id: \.offset) { i, o in
                    AnswerOption(key: i + 1, label: o.0, detail: o.1, picked: picked == i + 1) { picked = i + 1 }
                }
            }
        }
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

extension ComponentGallery {
    /// A session's pieces (T848): its header's title and switch, the
    /// question as its hero, and the composer dock with its key strip.
    private var session: some View {
        VStack(alignment: .leading, spacing: Spacing.row) {
            Text("Session").font(theme.title(.title, .bold)).foregroundStyle(theme.ink)
            HStack {
                SessionTitle("Product search ranking", place: "T14 · acme-web", state: Attention(word: "asks", tone: .question, why: "", waits: true))
                Spacer(minLength: 0)
                SessionToggle(selection: $tab)
            }
            HeroCard(tone: .question, symbol: Need.Kind.question.symbol, word: "Asks · Ranking", line: "AskUserQuestion · waiting 6m",
                     title: "Which ranking should search use for products with no sales yet?") {
                VStack(spacing: Spacing.item) {
                    ForEach(Array(Self.options.enumerated()), id: \.offset) { i, o in
                        AnswerOption(key: i + 1, label: o.0, detail: o.1, action: nil)
                    }
                }
            }
            DockNote("1 prompt queued")
            KeyStrip { _ in }
            HStack(alignment: .bottom, spacing: Spacing.item) {
                Button {} label: { Label("Keys", systemImage: "keyboard") }.buttonStyle(.circle)
                HStack {
                    Text("Message T14").foregroundStyle(theme.muted).padding(.vertical, 11)
                    Spacer(minLength: 0)
                    DockModes([(false, "Queue"), (true, "Steer")], selection: $steer)
                }
                .dockField()
                Button {} label: { Label("Send", systemImage: "arrow.up") }.buttonStyle(.send)
            }
        }
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

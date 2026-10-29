import SwiftUI
import TerminatrKit

/// The card's own coordinates, where its head is measured.
private let cardSpace = "need card"

/// A need's answer as a VoiceOver action on its card.
public struct NeedAct: Identifiable {
    public let name: String
    public let action: () -> Void
    public var id: String { name }

    public init(_ name: String, action: @escaping () -> Void) {
        self.name = name
        self.action = action
    }
}

/// A need as a card of the Needs you deck (T847, direction A "Glass
/// deck"): its head (NeedCardHead), in the caller's link to what opens
/// it; an approval's dialog as a command block; then its answers, in
/// reach (the caller's: big tone buttons, or numbered option rows), and
/// a line of what the server said. The lead card is lifted, ringed in
/// its colour, and its answers take the keyboard's keys (answerKeys); a
/// card shown beside the list is ringed too.
///
/// With VoiceOver it is one element, "T12 needs approval, acme-web, 2
/// minutes ago", whose actions are its answers and whose activation
/// lands on its head, the link; without, its buttons are its own (Voice
/// Control, Full Keyboard Access).
public struct NeedCard<Link: View, Answers: View>: View {
    @Environment(\.theme) private var theme
    @Environment(\.accessibilityVoiceOverEnabled) private var voiceOver
    let need: Need
    let command: String
    let note: (text: String, failed: Bool)?
    let lead: Bool
    let open: Bool
    let acts: [NeedAct]
    let now: Date
    let link: (NeedCardHead) -> Link
    let answers: Answers
    /// The head's middle and the card's height: where VoiceOver activates it.
    @State private var headY: CGFloat = 0
    @State private var cardHeight: CGFloat = 0

    /// command: an approval's dialog, "" for none; note: what the server
    /// said of an answer, or why the card can't answer; link: the head in
    /// what opens the need's session or project.
    public init(need: Need, command: String = "", note: (text: String, failed: Bool)? = nil, lead: Bool = false,
                open: Bool = false, acts: [NeedAct] = [], now: Date = Date(),
                @ViewBuilder link: @escaping (NeedCardHead) -> Link, @ViewBuilder answers: () -> Answers) {
        self.need = need
        self.command = command
        self.note = note
        self.lead = lead
        self.open = open
        self.acts = acts
        self.now = now
        self.link = link
        self.answers = answers()
    }

    public var body: some View {
        let shape = RoundedRectangle(cornerRadius: Radius.card, style: .continuous)
        let ring = lead || open
        VStack(alignment: .leading, spacing: 11) {
            link(NeedCardHead(need: need, now: now))
                .onGeometryChange(for: CGFloat.self) { $0.frame(in: .named(cardSpace)).midY } action: { mid in
                    headY = mid
                }
            if !command.isEmpty {
                Text(command)
                    .font(theme.mono(.callout))
                    .foregroundStyle(theme.ink)
                    .lines(4)
                    .padding(.horizontal, Spacing.row)
                    .padding(.vertical, Spacing.item)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .background(theme.command, in: RoundedRectangle(cornerRadius: Radius.row - 4, style: .continuous))
            }
            answers
                .environment(\.answerKeys, lead)
            if let note {
                // A refusal in ink after the attention mark: the
                // attention colour falls short of 4.5:1 as text on a
                // deck card in some themes (Bloom light).
                HStack(alignment: .firstTextBaseline, spacing: Spacing.tight) {
                    if note.failed {
                        Image(systemName: "exclamationmark.triangle.fill")
                            .foregroundStyle(theme.attention)
                            .accessibilityHidden(true)
                    }
                    Text(note.text)
                        .foregroundStyle(note.failed ? theme.ink : theme.muted)
                        .fixedSize(horizontal: false, vertical: true)
                }
                .font(theme.body(.footnote))
            }
        }
        .padding(Spacing.card + 2)
        .frame(maxWidth: .infinity, alignment: .leading)
        .coordinateSpace(.named(cardSpace))
        .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { cardHeight = $0 }
        .surface(lead ? .raised : .card, in: shape)
        .overlay {
            shape.strokeBorder(theme.colour(need.kind).opacity(ring || theme.contrast ? 1 : 0.35),
                               lineWidth: ring ? (theme.contrast ? 2.5 : 2) : 1)
        }
        .modifier(OneElement(on: voiceOver, label: spoken, value: note?.text ?? "", acts: acts,
                             at: UnitPoint(x: 0.5, y: cardHeight > 0 ? headY / cardHeight : 0.1)))
    }


    /// What VoiceOver says of the card: what it is, its project, its age,
    /// its task, the agent's words and the dialog.
    private var spoken: String {
        var parts = [need.what]
        if !need.project.isEmpty { parts.append(need.project) }
        if let since = need.since {
            parts.append(Self.ago.localizedString(for: min(since, now), relativeTo: now))
        }
        parts += [need.detail, need.text, command].filter { !$0.isEmpty }
        return parts.joined(separator: ", ").replacingOccurrences(of: " · ", with: ", ")
    }

    private static var ago: RelativeDateTimeFormatter {
        let f = RelativeDateTimeFormatter()
        f.unitsStyle = .full
        return f
    }
}

extension NeedCard where Link == NeedCardHead {
    /// A card whose head opens nothing (the gallery's).
    public init(need: Need, command: String = "", note: (text: String, failed: Bool)? = nil, lead: Bool = false,
                acts: [NeedAct] = [], now: Date = Date(), @ViewBuilder answers: () -> Answers) {
        self.init(need: need, command: command, note: note, lead: lead, acts: acts, now: now,
                  link: { $0 }, answers: answers)
    }
}

/// A need card's head: its StateGlyph in the kind's tone, the kind's
/// word, its project in mono and its age (and a chevron when it opens
/// something); what it is as the title, its task's title and the
/// agent's words under it.
public struct NeedCardHead: View {
    @Environment(\.theme) private var theme
    @Environment(\.verticalSizeClass) private var height
    let need: Need
    let now: Date
    var chevron = false

    /// With the chevron that says the head opens something.
    public func opens() -> NeedCardHead {
        var h = self
        h.chevron = true
        return h
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: Spacing.item) {
            HStack(spacing: 11) {
                StateGlyph(need.kind.tone, symbol: need.kind.symbol)
                    .symbolVariant(.fill)
                VStack(alignment: .leading, spacing: 2) {
                    Text(need.kind.word)
                        .font(theme.body(.headline, .bold))
                        .foregroundStyle(theme.ink)
                    if !need.project.isEmpty {
                        Text(need.project)
                            .font(theme.mono(.subheadline, .medium))
                            .foregroundStyle(theme.muted)
                            .lines(1)
                    }
                }
                Spacer(minLength: 0)
                let age = need.age(now: now)
                if !age.isEmpty {
                    Text(age)
                        .font(theme.mono(.subheadline, .medium))
                        .foregroundStyle(theme.muted)
                }
                if chevron {
                    Image(systemName: "chevron.right")
                        .font(theme.body(.footnote, .semibold))
                        .foregroundStyle(theme.muted)
                        .accessibilityHidden(true)
                }
            }
            Text(need.what)
                .font(theme.body(.title3, .semibold))
                .foregroundStyle(theme.ink)
                .fixedSize(horizontal: false, vertical: true)
            if !need.detail.isEmpty {
                Text(need.detail)
                    .font(theme.body(.subheadline))
                    .foregroundStyle(theme.muted)
                    .lines(2)
            }
            if !need.text.isEmpty {
                // Two lines, one on a short screen (a closed iPhone Duo
                // held sideways), so the answers stay in reach.
                Text(need.text)
                    .font(theme.body(.body))
                    .foregroundStyle(theme.ink)
                    .lines(height == .compact ? 1 : 2)
            }
        }
        .multilineTextAlignment(.leading)
        .frame(maxWidth: .infinity, alignment: .leading)
        .contentShape(Rectangle())
    }
}

/// The card as VoiceOver reads it: one element, its answers its actions,
/// activated at its head. Without VoiceOver, its buttons stay its own.
private struct OneElement: ViewModifier {
    let on: Bool
    let label: String
    let value: String
    let acts: [NeedAct]
    let at: UnitPoint

    func body(content: Content) -> some View {
        if on, #available(iOS 18, macOS 15, *) {
            content
                .accessibilityElement(children: .ignore)
                .accessibilityLabel(label)
                .accessibilityValue(value)
                .accessibilityActivationPoint(at)
                .accessibilityActions {
                    ForEach(acts) { a in
                        Button(a.name, action: a.action)
                    }
                }
        } else {
            content.accessibilityElement(children: .contain)
        }
    }
}

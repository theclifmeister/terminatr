import SwiftUI
import TerminatrKit

/// A need as the phone's Welcome shows it (T850): a deck card with its
/// StateGlyph, kind, project and ref, title and two answers, drawn and
/// never tapped. FloatingNeeds lays two over the mesh.
public struct SampleNeed: View {
    @Environment(\.theme) private var theme
    let tone: Attention.Tone
    let kind: String
    let ref: String
    let title: String
    let age: String
    let yes: String
    let no: String

    public init(_ tone: Attention.Tone, kind: String, ref: String, title: String, age: String, yes: String, no: String) {
        self.tone = tone
        self.kind = kind
        self.ref = ref
        self.title = title
        self.age = age
        self.yes = yes
        self.no = no
    }

    public var body: some View {
        let shape = RoundedRectangle(cornerRadius: Radius.card, style: .continuous)
        VStack(alignment: .leading, spacing: Spacing.row) {
            HStack(spacing: Spacing.row) {
                StateGlyph(tone, side: 32)
                VStack(alignment: .leading, spacing: 1) {
                    Text(kind)
                        .font(theme.body(.headline, .bold))
                        .foregroundStyle(theme.ink)
                    Text(ref)
                        .font(theme.mono(.caption, .medium))
                        .foregroundStyle(theme.muted)
                }
                Spacer(minLength: 0)
                Text(age)
                    .font(theme.mono(.caption))
                    .foregroundStyle(theme.muted)
            }
            Text(title)
                .font(theme.body(.title3, .bold))
                .foregroundStyle(theme.ink)
                .lineLimit(1)
            HStack(spacing: Spacing.item) {
                Button(yes) {}
                    .buttonStyle(.primary(tone))
                Button(no) {}
                    .buttonStyle(.secondary)
            }
        }
        .padding(Spacing.card)
        .surface(.raised, in: shape)
        .overlay(shape.strokeBorder(theme.colour(tone).opacity(0.6), lineWidth: 1.5))
        .allowsHitTesting(false)
    }
}

/// Two needs over the Welcome's mesh, tilted as if dropped there: an
/// approval and a review. Only a picture of what the app does, so
/// VoiceOver skips it and the panel's words say it.
public struct FloatingNeeds: View {
    @Environment(\.accessibilityReduceMotion) private var still
    @State private var up = false

    public init() {}

    public var body: some View {
        VStack(spacing: -12) {
            SampleNeed(.approval, kind: "Approval", ref: "acme-web · T12", title: "Checkout: Apple Pay button", age: "2m", yes: "Allow", no: "Deny")
                .rotationEffect(.degrees(-4))
                .offset(x: -6, y: up ? -4 : 0)
            SampleNeed(.review, kind: "Review", ref: "acme-web · T15", title: "Image CDN migration", age: "1h", yes: "Accept", no: "Send back")
                .rotationEffect(.degrees(3))
                .offset(x: 10, y: up ? 3 : 0)
        }
        .accessibilityHidden(true)
        .onAppear {
            guard !still else { return }
            withAnimation(.easeInOut(duration: 4).repeatForever(autoreverses: true)) { up = true }
        }
    }
}

/// The Welcome's panel (T850): the wordmark in the display face at
/// 52 pt, a line of what the app does, and its buttons, on the glass at
/// the foot of the screen.
public struct WordmarkPanel<Actions: View>: View {
    @Environment(\.theme) private var theme
    let blurb: String
    let actions: Actions

    public init(_ blurb: String, @ViewBuilder actions: () -> Actions) {
        self.blurb = blurb
        self.actions = actions()
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: Spacing.card) {
            Text("terminatr")
                .font(theme.title(.wordmark, .heavy))
                .tracking(-1.5)
                .foregroundStyle(theme.ink)
                .lineLimit(1)
                .minimumScaleFactor(0.5)
                .accessibilityAddTraits(.isHeader)
            Text(blurb)
                .font(theme.body(.title3))
                .foregroundStyle(theme.muted)
                .fixedSize(horizontal: false, vertical: true)
            VStack(spacing: Spacing.row) { actions }
                .padding(.top, Spacing.tight)
        }
        .padding(.horizontal, 22)
        .padding(.top, 26)
        .padding(.bottom, 24)
        .frame(maxWidth: .infinity, alignment: .leading)
        .surface(.glass, in: RoundedRectangle(cornerRadius: 44, style: .continuous))
    }
}

import SwiftUI
import TerminatrLink
import TerminatrUI

/// The first screen (T850): the theme's mesh, drifting, with two needs
/// floating over it to show what the app is for, and at the foot the
/// wordmark on glass with the demo and pairing, and under them the
/// privacy policy and support (T910). At accessibility text
/// sizes the needs step aside and the panel scrolls. Launched with
/// -open pair it opens pairing, with -open "pair CODE" that code's
/// confirm step (the Simulator's screenshots).
struct Welcome: View {
    @Environment(Phone.self) private var phone
    @Environment(\.theme) private var theme
    @Environment(\.dynamicTypeSize) private var typeSize
    @State private var opening = false
    @State private var pairing: Pairing?

    var body: some View {
        GeometryReader { g in
            ScrollView {
                VStack(spacing: Spacing.gutter) {
                    if !typeSize.isAccessibilitySize {
                        Spacer(minLength: Spacing.gutter)
                        FloatingNeeds()
                            .padding(.horizontal, Spacing.item)
                    }
                    Spacer(minLength: Spacing.gutter)
                    panel
                }
                .frame(maxWidth: 520)
                .padding(.horizontal, 10)
                .padding(.bottom, 10)
                .frame(maxWidth: .infinity, minHeight: g.size.height)
            }
            .scrollBounceBehavior(.basedOnSize)
        }
        .background { Backdrop(drifting: true) }
        .sheet(item: $pairing) { PairScreen(code: $0.code) }
        .onAppear(perform: openLaunched)
    }

    private var panel: some View {
        WordmarkPanel("Your coding agents' questions, approvals and reviews, and every project's threads, wherever you are.") {
            Button {
                opening = true
                Task {
                    await phone.startDemo()
                    opening = false
                }
            } label: {
                Label("Explore the demo", systemImage: "bolt.fill")
            }
            .buttonStyle(.primary())
            .disabled(opening)
            .accessibilityHint("Opens recorded projects and threads, read only")
            Button { pairing = Pairing() } label: {
                Label("Pair with a server", systemImage: "qrcode.viewfinder")
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.secondary)
            if let f = phone.failure {
                Text(f)
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.attention)
                    .fixedSize(horizontal: false, vertical: true)
            }
            WelcomeLinks()
        }
    }

    private func openLaunched() {
        guard let open = UserDefaults.standard.string(forKey: "open"), open == "pair" || open.hasPrefix("pair ") else { return }
        pairing = Pairing(code: try? PairingCode(String(open.dropFirst(5))))
    }
}

/// The pairing sheet, with the code it opens at, if any.
private struct Pairing: Identifiable {
    let id = UUID()
    var code: PairingCode?
}

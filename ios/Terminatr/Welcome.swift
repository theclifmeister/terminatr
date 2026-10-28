import SwiftUI
import TerminatrUI

/// The first screen: what the app does, the demo, and pairing (not yet:
/// it comes with device links, T787).
struct Welcome: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    @State private var opening = false

    var body: some View {
        VStack(alignment: .leading, spacing: 18) {
            Spacer()
            Text("terminatr")
                .font(theme.title(.largeTitle))
            Text("See what your coding agents need from you, from anywhere: their questions, approvals and reviews, and every project's threads and conversations.")
                .font(theme.body(.body))
                .foregroundStyle(theme.muted)
                .fixedSize(horizontal: false, vertical: true)
            Spacer()
            Button {
                opening = true
                Task {
                    await model.startDemo()
                    opening = false
                }
            } label: {
                Label("Explore the demo", systemImage: "play.rectangle")
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.borderedProminent)
            .controlSize(.large)
            .disabled(opening)
            .accessibilityHint("Opens recorded projects and threads, read only")
            Button {} label: {
                Label("Pair with a server", systemImage: "qrcode.viewfinder")
                    .frame(maxWidth: .infinity)
            }
            .buttonStyle(.bordered)
            .controlSize(.large)
            .disabled(true)
            Text("Pairing comes in a later version: on your Mac, tm device pair will show the code to scan.")
                .font(theme.body(.footnote))
                .foregroundStyle(theme.muted)
            if let f = model.failure {
                Text(f)
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.attention)
            }
        }
        .padding(24)
        .backdrop()
    }
}

import SwiftUI
import TerminatrKit
import TerminatrUI

/// A coordinator or thread (T840): its conversation or its terminal,
/// as the Mac's session window switches them (SessionTab), the switch
/// in the navigation bar. It opens on the conversation (-terminal YES
/// opens it on the terminal, the Simulator's screenshots); the switch is
/// kept for each session while the server is (Places), so going back to
/// one shows what it showed. While the terminal shows, Repaint asks the
/// pane afresh.
struct SessionScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    let ref: SessionRef
    /// Opened from a need: its card is above the conversation.
    var need: Need?
    @State private var tab: SessionTab = UserDefaults.standard.bool(forKey: "terminal") ? .terminal : .conversation
    @State private var repaints = 0

    var body: some View {
        Group {
            switch tab {
            case .conversation: ConversationScreen(ref: ref, need: need)
            case .terminal:
                TerminalScreen(ref: ref, repaints: repaints)
                    .navigationTitle(model.title(ref))
                    .navigationBarTitleDisplayMode(.inline)
            }
        }
        .toolbar {
            if tab == .terminal, model.sessionID(ref) != nil {
                ToolbarItem(placement: .topBarTrailing) {
                    Button { repaints += 1 } label: { Label("Repaint", systemImage: "arrow.clockwise") }
                        .accessibilityHint("Draws its terminal afresh")
                }
            }
            ToolbarItem(placement: .topBarTrailing) {
                Picker("View", selection: $tab) {
                    ForEach(SessionTab.allCases, id: \.self) { t in
                        Label(t.rawValue, systemImage: t == .conversation ? "text.bubble" : "terminal").tag(t)
                    }
                }
                .pickerStyle(.segmented)
                .labelStyle(.iconOnly)
                .fixedSize()
                .accessibilityLabel("Conversation or Terminal")
            }
        }
        .kept($tab, "tab \(ref)")
    }
}

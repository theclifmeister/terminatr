import SwiftUI
import TerminatrKit
import TerminatrUI

/// A coordinator or thread (T840): its conversation or its terminal,
/// as the Mac's session window switches them (SessionTab). Its header
/// (T848) is the bar's, glass: the back button, the title with where it
/// is and its state's pill (SessionTitle), and the switch as a glass
/// capsule (SessionToggle). It opens on the conversation (-terminal YES
/// opens it on the terminal, the Simulator's screenshots); the switch is
/// kept for each session while the server is (Places), so going back to
/// one shows what it showed. While the terminal shows, Repaint asks the
/// pane afresh. On a phone's width the tab bar hides here, so the
/// composer dock has the bottom; beside a list (an iPad, an open Duo)
/// it stays.
struct SessionScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    @Environment(\.horizontalSizeClass) private var width
    let ref: SessionRef
    /// Opened from a need: the conversation opens where it was asked.
    var need: Need?
    @State private var tab: SessionTab = UserDefaults.standard.bool(forKey: "terminal") ? .terminal : .conversation
    @State private var repaints = 0

    var body: some View {
        Group {
            switch tab {
            case .conversation: ConversationScreen(ref: ref, need: need)
            case .terminal: TerminalScreen(ref: ref, repaints: repaints)
            }
        }
        .navigationTitle(model.title(ref))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .principal) {
                SessionTitle(model.title(ref), place: model.place(ref), state: model.attention(ref))
            }
            if tab == .terminal, model.sessionID(ref) != nil {
                ToolbarItem(placement: .topBarTrailing) {
                    Button { repaints += 1 } label: { Label("Repaint", systemImage: "arrow.clockwise") }
                        .accessibilityHint("Draws its terminal afresh")
                }
                // Its own glass, not the switch's.
                if #available(iOS 26, *) {
                    ToolbarSpacer(.fixed, placement: .topBarTrailing)
                }
            }
            ToolbarItem(placement: .topBarTrailing) {
                // iOS 26's bar draws its items' glass: the toggle's own
                // is for earlier ones.
                if #available(iOS 26, *) {
                    SessionToggle(selection: $tab, framed: false)
                } else {
                    SessionToggle(selection: $tab)
                }
            }
        }
        .toolbar(width == .compact ? .hidden : .automatic, for: .tabBar)
        .kept($tab, "tab \(ref)")
    }
}

import SwiftUI
import TerminatrKit
import TerminatrUI

/// A coordinator or thread (T840): its conversation or its terminal,
/// as the Mac's session window switches them (SessionTab). Its header
/// (T848) is the bar's, glass: the back button, the title with where it
/// is, its state's pill and its context use (SessionTitle), a
/// coordinator's menu with Save & Clear (T868), and the switch as a glass
/// capsule (SessionToggle). It opens on the conversation (-terminal YES
/// opens it on the terminal, the Simulator's screenshots); the switch is
/// kept for each session while the server is (Places), so going back to
/// one shows what it showed. While the terminal shows, Repaint asks the
/// pane afresh. On a phone's width the tab bar hides here, so the
/// composer dock has the bottom, and the live strip with it; beside a
/// list (an iPad, an open Duo) they stay.
struct SessionScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    @Environment(\.horizontalSizeClass) private var width
    let ref: SessionRef
    /// Opened from a need: the conversation opens where it was asked.
    var need: Need?
    @State private var tab: SessionTab = UserDefaults.standard.bool(forKey: "terminal") ? .terminal : .conversation
    @State private var repaints = 0
    @State private var clearing = false

    var body: some View {
        Group {
            switch tab {
            case .conversation: ConversationScreen(ref: ref, need: need)
            case .terminal: TerminalScreen(ref: ref, repaints: repaints)
            }
        }
        .safeAreaInset(edge: .top) {
            if ref.isCoordinator, !ref.isRemote, let clear = model.clears[ref.project] {
                SaveClearStatus(clear: clear).padding(.top, Spacing.tight)
            }
        }
        .navigationTitle(model.title(ref))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .principal) {
                SessionTitle(model.title(ref), place: model.place(ref), state: model.attention(ref), context: model.context(ref))
            }
            if ref.isCoordinator, !ref.isRemote, model.clearsCoordinators, model.coordinatorSession(ref.project) != nil {
                ToolbarItem(placement: .topBarTrailing) {
                    Menu {
                        SaveClearButton(project: ref.project, asking: $clearing)
                    } label: {
                        Label("Coordinator", systemImage: "ellipsis")
                    }
                    .accessibilityLabel("Coordinator actions")
                }
                if #available(iOS 26, *) {
                    ToolbarSpacer(.fixed, placement: .topBarTrailing)
                }
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
        .hidesLiveStrip(width == .compact)
        .asksSaveClear(ref.project, isPresented: $clearing)
        .kept($tab, "tab \(ref)")
    }
}

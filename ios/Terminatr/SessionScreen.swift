import SwiftUI
import TerminatrKit
import TerminatrUI

/// A coordinator, a program's director (T973) or a thread (T840): its conversation or its terminal,
/// as the Mac's session window switches them (SessionTab). Its header
/// (T848) is the bar's, glass: the back button, the title with where it
/// is, its state's pill and its context use (SessionTitle), a
/// coordinator's menu with Save & Clear (T868), and the switch as a glass
/// capsule (SessionToggle). It opens on the conversation (-terminal YES
/// opens it on the terminal, the Simulator's screenshots); the switch is
/// kept for each session while the server is (Places), so going back to
/// one shows what it showed. While the terminal shows, Repaint asks the
/// pane afresh. On a phone's width the tab bar hides here, so the
/// composer dock has the bottom; beside a list (an iPad, an open Duo)
/// the bar stays.
struct SessionScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    @Environment(\.horizontalSizeClass) private var width
    @Environment(\.showParent) private var showParent
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
        .safeAreaInset(edge: .top, spacing: 0) {
            if let showParent { parentChip(showParent) }
        }
        .safeAreaInset(edge: .top) {
            if ref.isCoordinator || ref.director, !ref.isRemote, let clear = model.clears[PhoneModel.clearKey(ref)] {
                SaveClearStatus(clear: clear).padding(.top, Spacing.tight)
            }
        }
        .navigationTitle(model.title(ref))
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .principal) {
                SessionTitle(model.title(ref), place: model.place(ref), state: model.attention(ref), context: model.context(ref))
            }
            if ref.isCoordinator || ref.director, !ref.isRemote, model.clearsCoordinators, model.leadSession(ref) != nil {
                ToolbarItem(placement: .topBarTrailing) {
                    Menu {
                        SaveClearButton(ref: ref, asking: $clearing)
                    } label: {
                        Label(model.title(ref), systemImage: "ellipsis")
                    }
                    .accessibilityLabel("\(model.title(ref)) actions")
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
        .asksSaveClear(ref, isPresented: $clearing)
        .kept($tab, "tab \(ref)")
        // Opening a sleeping coordinator wakes it, as enter on its row in
        // the TUI (T980): it spins until it runs. Only on opening: one
        // that falls asleep while shown stays asleep.
        .task(id: ref) {
            if ref.isCoordinator, !ref.isRemote, model.wakes(ref.project) {
                try? await model.wake(ref.project)
            }
        }
        .environment(\.showTerminal, ShowTerminal { tab = .terminal })
    }

    /// The way to its project or program, beside a list that isn't
    /// theirs (Columns.showParent). In the screen, with the name on it,
    /// not the bar: an open iPhone Duo's bar is the rail, where an item
    /// is a bare icon that can outlive its screen.
    private func parentChip(_ parent: ShowParent) -> some View {
        Button(action: parent.go) {
            Label(ref.project, systemImage: ref.director ? "square.stack.3d.up" : "folder")
                .font(theme.body(.footnote, .semibold))
                .foregroundStyle(theme.ink)
                .lineLimit(1)
                .padding(.horizontal, Spacing.card)
                .frame(minHeight: tapTarget)
                .surface(.glass, in: Capsule())
                .contentShape(Capsule())
        }
        .buttonStyle(.plain)
        .hoverEffect()
        .accessibilityLabel("Open \(ref.director ? "program" : "project") \(ref.project)")
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, Spacing.gutter)
        .padding(.vertical, Spacing.tight)
    }
}

/// Switches a session's screen to its terminal: an answer its
/// conversation's card couldn't type offers it (NeedActs).
struct ShowTerminal {
    let go: () -> Void
}

extension EnvironmentValues {
    @Entry var showTerminal: ShowTerminal?
}

import SwiftUI
import TerminatrKit
import TerminatrUI

extension View {
    /// Save & Clear for project's coordinator (T868), as the Mac's: asked
    /// first in the TUI's words (ProjectAsk), then coordinator.clear,
    /// followed in the model's clears.
    func asksSaveClear(_ project: String, isPresented: Binding<Bool>) -> some View {
        modifier(SaveClearAsk(project: project, asking: isPresented))
    }
}

private struct SaveClearAsk: ViewModifier {
    @Environment(PhoneModel.self) private var model
    let project: String
    @Binding var asking: Bool

    func body(content: Content) -> some View {
        let ask = ProjectAsk.saveClear(project: project)
        content.confirmationDialog(ask.title, isPresented: $asking, titleVisibility: .visible) {
            Button(ask.verb) { Task { await model.saveClear(project) } }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text(ask.message)
        }
    }
}

/// The menu's Save & Clear…: at the write level, for a running
/// coordinator of this server's, while none is on its way.
struct SaveClearButton: View {
    @Environment(PhoneModel.self) private var model
    let project: String
    @Binding var asking: Bool

    var body: some View {
        if model.clearsCoordinators, model.coordinatorSession(project) != nil {
            Button { asking = true } label: {
                Label("Save & Clear…", systemImage: "eraser")
            }
            .disabled(model.clears[project]?.running == true)
            .accessibilityHint("The coordinator saves what it learned, then its conversation is cleared, as tm clear does")
        }
    }
}

/// Where a Save & Clear is (T868), on glass: a spinner while it saves
/// and clears, then what came of it, the context's drop. VoiceOver
/// hears each step.
struct SaveClearStatus: View {
    @Environment(\.theme) private var theme
    let clear: SaveClear

    var body: some View {
        HStack(spacing: Spacing.tight) {
            switch clear.phase {
            case .asking, .saving, .clearing:
                ProgressView().controlSize(.small)
            case .done:
                Image(systemName: "checkmark.circle.fill").foregroundStyle(theme.good)
            case .failed:
                Image(systemName: "exclamationmark.triangle.fill").foregroundStyle(theme.attention)
            }
            Text(clear.words)
                .font(theme.body(.footnote, .medium))
                .foregroundStyle(theme.ink)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(.horizontal, Spacing.card)
        .padding(.vertical, Spacing.item)
        .surface(.glass, in: Capsule())
        .accessibilityElement(children: .combine)
        .onChange(of: clear.words, initial: true) { _, w in
            AccessibilityNotification.Announcement(w).post()
        }
    }
}

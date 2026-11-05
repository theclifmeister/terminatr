import SwiftUI
import TerminatrKit
import TerminatrUI

/// The coordinator's kept questions (tm ask) while none is on its
/// screen: Answer now has it put them to the user in its conversation
/// (questions.open), where the question card answers them.
struct QuestionsCard: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let project: String
    let count: Int
    @State private var busy = false
    @State private var error: String?

    var body: some View {
        CardFrame(tag: "Questions · \(count) open", colour: theme.question) {
            HStack(alignment: .center, spacing: 10) {
                VStack(alignment: .leading, spacing: 3) {
                    Text(count == 1 ? "The coordinator has a question for you" : "The coordinator has \(count) questions for you")
                        .font(theme.body(.body))
                    Text(error ?? "It asks them here, one dialog at a time, when you answer now.")
                        .font(theme.body(.subheadline))
                        .foregroundStyle(error == nil ? theme.muted : theme.attention)
                }
                Spacer(minLength: 8)
                Button("Answer Now") { open() }
                    .buttonStyle(.flatPrimary)
                    .disabled(busy)
            }
        }
    }

    private func open() {
        busy = true
        error = nil
        Task {
            do {
                try await app.openQuestions(project)
            } catch {
                self.error = Dashboard.words(error)
            }
            busy = false
        }
    }
}

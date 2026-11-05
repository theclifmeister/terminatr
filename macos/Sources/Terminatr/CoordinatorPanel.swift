import SwiftUI
import TerminatrKit
import TerminatrUI

/// Over a coordinator's conversation (T748 milestone C): what else of its
/// project needs the user, as the dashboard's rows, in a list that acts
/// in place. Its own question menu, permission dialog and kept questions
/// are cards at the transcript's end (ConversationTab). It scrolls within
/// a third of the window at most, so the composer under the conversation
/// stays in reach.
struct CoordinatorPanel: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let model: SessionModel
    let session: AppAPI.Session?
    @State private var all = false

    var body: some View {
        // Its own needs are the cards and the questions at the foot.
        let needs = app.needs(of: model.ref.project).filter { n in
            !n.id.hasPrefix("questions:") && (n.session.isEmpty || n.session != session?.id)
        }
        let shown = all ? needs : Array(needs.prefix(Self.shown))
        let settled = app.settledLinks(model.ref.project)
        if !needs.isEmpty || !settled.isEmpty {
            ViewThatFits(in: .vertical) {
                content(needs: needs, shown: shown, settled: settled)
                ScrollView { content(needs: needs, shown: shown, settled: settled) }
                    .frame(maxHeight: 280)
            }
            .padding(.top, 13)
            .overlay(alignment: .bottom) { theme.line.frame(height: 1) }
        }
    }

    static let shown = 3

    private func content(needs: [Need], shown: [Need], settled: [(id: String, outcome: LinkOutcome)]) -> some View {
        VStack(alignment: .leading, spacing: 7) {
            PaneLabel("Needs you in \(model.ref.project) · \(needs.count)")
            if !shown.isEmpty {
                HairlineList { ForEach(shown) { NeedCard(need: $0) } }
            }
            ForEach(settled, id: \.id) { LinkOutcomeRow(outcome: $0.outcome, settled: true) }
            if needs.count > shown.count || all && needs.count > Self.shown {
                Button(all ? "Fewer" : "\(needs.count - shown.count) more") { all.toggle() }
                    .buttonStyle(.link)
                    .foregroundStyle(theme.action)
                    .font(theme.body(.subheadline))
            }
        }
        .frame(maxWidth: Flat.reading, alignment: .leading)
        .padding(.horizontal, Page.edge)
        .padding(.bottom, 13)
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

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

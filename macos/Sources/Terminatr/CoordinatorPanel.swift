import SwiftUI
import TerminatrKit

/// Over a coordinator's conversation (T748 milestone C): its open
/// questions first, the question menu it shows answered by the cards
/// (and, with none shown, the questions it keeps, tm ask, which Answer
/// now has it put to the user, questions.open), its permission dialog,
/// then what else of its project needs the user, as the dashboard's
/// cards. It scrolls within a third of the window at most, so the
/// composer under the conversation stays in reach.
struct CoordinatorPanel: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let model: SessionModel
    let session: AppAPI.Session?
    @State private var all = false

    var body: some View {
        let project = app.project(model.ref.project)
        let carded = AppModel.waitsOnCard(session)
        let kept = carded ? 0 : project?.questions ?? 0
        // Its own needs are the cards and the questions above.
        let needs = app.needs(of: model.ref.project).filter { n in
            !n.id.hasPrefix("questions:") && (n.session.isEmpty || n.session != session?.id)
        }
        let shown = all ? needs : Array(needs.prefix(Self.shown))
        if carded || kept > 0 || !needs.isEmpty {
            ViewThatFits(in: .vertical) {
                content(kept: kept, needs: needs, shown: shown)
                ScrollView { content(kept: kept, needs: needs, shown: shown) }
                    .frame(maxHeight: 280)
            }
            .padding(.top, 10)
            .overlay(alignment: .bottom) { theme.line.frame(height: 1) }
        }
    }

    static let shown = 3

    private func content(kept: Int, needs: [Need], shown: [Need]) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Cards(model: model, session: session, firstChoice: app.firstChoice)
            if kept > 0 {
                QuestionsCard(project: model.ref.project, count: kept)
                    .padding(.horizontal, 22)
                    .frame(maxWidth: 820)
            }
            if !needs.isEmpty {
                VStack(alignment: .leading, spacing: 8) {
                    Text("Needs you in \(model.ref.project) · \(needs.count)".uppercased())
                        .font(theme.mono(.label, .medium))
                        .tracking(1)
                        .foregroundStyle(theme.muted)
                    ForEach(shown) { NeedCard(need: $0) }
                    if needs.count > shown.count || all && needs.count > Self.shown {
                        Button(all ? "Fewer" : "\(needs.count - shown.count) more") { all.toggle() }
                            .buttonStyle(.link)
                            .foregroundStyle(theme.action)
                            .font(theme.body(.subheadline))
                    }
                }
                .padding(.horizontal, 22)
                .frame(maxWidth: 820)
            }
        }
        .padding(.bottom, 10)
        .frame(maxWidth: .infinity)
    }
}

/// The coordinator's kept questions (tm ask) while none is on its
/// screen: Answer now has it put them to the user in its conversation
/// (questions.open), where the question card answers them.
private struct QuestionsCard: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let project: String
    let count: Int
    @State private var busy = false
    @State private var error: String?

    var body: some View {
        CardFrame(tag: "Questions · \(count) open", colour: theme.question, tint: theme.questionTint) {
            HStack(alignment: .center, spacing: 10) {
                VStack(alignment: .leading, spacing: 3) {
                    Text(count == 1 ? "The coordinator has a question for you" : "The coordinator has \(count) questions for you")
                        .font(theme.title(.headline, .bold))
                    Text(error ?? "It asks them here, one dialog at a time, when you answer now.")
                        .font(theme.body(.subheadline))
                        .foregroundStyle(error == nil ? theme.muted : theme.attention)
                }
                Spacer(minLength: 8)
                Button("Answer Now") { open() }
                    .buttonStyle(.borderedProminent)
                    .tint(theme.action)
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

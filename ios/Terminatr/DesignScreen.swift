import SwiftUI
import TerminatrKit
import TerminatrUI

/// A project's designs on its screen (T864), as the TUI's Designs tab
/// and the Mac dashboard's list them (design.list, app API 31): under a
/// SectionHeading, one DesignTile each, those with open questions first
/// (Designs.ordered), each opening its DesignScreen. Read again when
/// the board or the link changes; nothing shows without designs.
struct DesignsTiles: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.navigate) private var navigate
    let project: AppAPI.Project
    @State private var designs: [AppAPI.Design] = []
    @State private var error: String?

    var body: some View {
        // A stack, not a Group: its task runs while it shows nothing.
        VStack(alignment: .leading, spacing: Spacing.row + 2) {
            Color.clear.frame(height: 0)
            if !designs.isEmpty {
                SectionHeading("Designs", detail: "\(designs.count)")
                TileGroup {
                    ForEach(Array(Designs.ordered(designs).enumerated()), id: \.element.name) { i, d in
                        let route = Route.design(project.slug, name: d.name)
                        PlainRouteLink(value: route) {
                            DesignTile(d, rule: i > 0, selected: navigate?.open == route)
                        }
                        .buttonStyle(.plain)
                    }
                }
            } else if let error {
                Failure(text: "The designs didn't load: \(error)")
            }
        }
        .task(id: DesignsRead(project: project, generation: model.generation)) {
            do {
                designs = try await model.designs(project.slug)
                error = nil
            } catch {
                self.error = Dashboard.words(error)
            }
        }
    }
}

/// When a project's designs are read again: its board, counts or open
/// questions changed, or the link came back.
struct DesignsRead: Equatable {
    let slug: String
    let board: [String]
    let counts: [String: Int]
    let questions: Int
    let generation: Int

    init(project p: AppAPI.Project, generation: Int) {
        slug = p.slug
        board = p.board.map { "\($0.ref) \($0.status) \($0.updated)" }
        counts = p.counts
        questions = p.questions
        self.generation = generation
    }
}

/// One design in full: its status, title and page, its decided answers,
/// its design task's open questions (at the answer level Answer… has
/// the coordinator put them to the user, as the TUI's enter does) and
/// its build tasks in after order, each a board task's opening its
/// TaskScreen. Nothing here changes a design: the coordinator records
/// them (tm design set).
struct DesignScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    @Environment(\.navigate) private var navigate
    let slug: String
    let name: String
    @State private var design: AppAPI.Design?
    @State private var loaded = false
    @State private var error: String?
    @State private var asking = false
    @State private var askError: String?

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Spacing.row + 2) {
                if let d = design {
                    header(d)
                    answers(d)
                    if !d.questions.isEmpty { questions(d) }
                    build(d)
                } else if let error {
                    Failure(text: error)
                } else if loaded {
                    Text("\(slug) has no design \(name) any more.")
                        .foregroundStyle(theme.muted)
                } else {
                    ProgressView()
                }
            }
            .padding(.horizontal, Spacing.gutter)
            .padding(.bottom, Spacing.gutter)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .task(id: model.project(slug).map { DesignsRead(project: $0, generation: model.generation) }) {
            do {
                design = try await model.designs(slug).first { $0.name == name }
                error = nil
            } catch {
                self.error = Dashboard.words(error)
            }
            loaded = true
        }
        .refreshable { await model.refresh() }
        .navigationTitle(design?.title ?? name)
        .headerTitle()
        .backdrop()
    }

    private func header(_ d: AppAPI.Design) -> some View {
        VStack(alignment: .leading, spacing: Spacing.item) {
            Text(d.title)
                .font(theme.title(.title, .heavy))
                .foregroundStyle(theme.ink)
                .accessibilityAddTraits(.isHeader)
            HStack(alignment: .firstTextBaseline, spacing: Spacing.tight) {
                ToneBadge(d.status, tone: Designs.tone(d))
                Text(Designs.built(d))
                    .font(theme.mono(.subheadline, .medium))
                    .foregroundStyle(theme.muted)
            }
            if !d.build.isEmpty {
                ProgressView(value: Designs.progress(d))
                    .tint(Designs.Status(d.status) == .done ? theme.good : theme.action)
            }
            let task = Designs.designTask(d)
            if !task.isEmpty {
                if d.taskStatus.isEmpty {
                    Text("Design task \(task)").foregroundStyle(theme.muted)
                } else {
                    PlainRouteLink(value: Route.task(slug, ref: d.task)) {
                        Label("Design task \(task)", systemImage: "pencil.and.ruler")
                            .font(theme.body(.body, .medium))
                    }
                }
            }
            if let url = Designs.page(d) {
                Link(destination: url) {
                    Label("Design page", systemImage: "link")
                        .font(theme.body(.body, .medium))
                }
                .accessibilityHint(url.absoluteString)
            }
        }
        .padding(.top, Spacing.item)
    }

    @ViewBuilder
    private func answers(_ d: AppAPI.Design) -> some View {
        SectionHeading("Answers", detail: "\(d.answers.count)")
        if d.answers.isEmpty {
            quiet("None recorded yet.")
        } else {
            TileGroup {
                ForEach(Array(d.answers.enumerated()), id: \.offset) { i, a in
                    line(rule: i > 0) {
                        Text("\(i + 1)")
                            .font(theme.mono(.subheadline, .medium))
                            .foregroundStyle(theme.muted)
                            .frame(minWidth: 22, alignment: .leading)
                        Text(a).textSelection(.enabled)
                    }
                }
            }
        }
    }

    @ViewBuilder
    private func questions(_ d: AppAPI.Design) -> some View {
        SectionHeading("Open questions", detail: "\(d.questions.count)")
        TileGroup {
            ForEach(Array(d.questions.enumerated()), id: \.element.id) { i, q in
                line(rule: i > 0) {
                    StateGlyph(.question, side: 24)
                    VStack(alignment: .leading, spacing: 2) {
                        if !q.header.isEmpty {
                            Text(q.header)
                                .font(theme.mono(.caption, .medium))
                                .foregroundStyle(theme.muted)
                        }
                        Text(q.question)
                    }
                }
            }
        }
        if model.can(AppAPI.Methods.questionsOpen.name) {
            Button { answer() } label: {
                Label("Answer…", systemImage: "questionmark.bubble")
            }
            .disabled(asking)
            .accessibilityHint("Asks the coordinator to put its open questions to you")
            if let askError { Failure(text: askError) }
        }
    }

    @ViewBuilder
    private func build(_ d: AppAPI.Design) -> some View {
        SectionHeading("Build", detail: Designs.built(d))
        if d.build.isEmpty {
            quiet("No task builds it yet.")
        } else {
            TileGroup {
                ForEach(Array(d.build.enumerated()), id: \.element.id) { i, t in
                    if t.archived {
                        buildRow(t, rule: i > 0)
                    } else {
                        let route = Route.task(slug, ref: t.id)
                        PlainRouteLink(value: route) {
                            buildRow(t, rule: i > 0)
                                .background(navigate?.open == route ? theme.wash(.working) : .clear)
                        }
                        .buttonStyle(.plain)
                    }
                }
            }
        }
    }

    private func buildRow(_ t: AppAPI.DesignTask, rule: Bool) -> some View {
        line(rule: rule) {
            StateGlyph(Designs.tone(task: t), side: 24)
            VStack(alignment: .leading, spacing: 2) {
                HStack(alignment: .firstTextBaseline, spacing: Spacing.tight) {
                    Text(t.id)
                        .font(theme.mono(.subheadline, .medium))
                        .foregroundStyle(t.archived ? theme.muted : theme.action)
                    Text(t.title)
                        .font(theme.body(.body, .semibold))
                        .lines(2)
                }
                let after = Designs.after(t)
                Text(([t.status] + (after.isEmpty ? [] : [after]) + (t.archived ? ["archived"] : [])).joined(separator: " · "))
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.muted)
            }
        }
    }

    /// A row of a TileGroup, a hairline over it after the first.
    private func line<C: View>(rule: Bool, @ViewBuilder _ content: () -> C) -> some View {
        HStack(alignment: .center, spacing: Spacing.row) {
            content()
            Spacer(minLength: 0)
        }
        .padding(.horizontal, Spacing.card)
        .padding(.vertical, Spacing.row)
        .frame(maxWidth: .infinity, minHeight: tapTarget, alignment: .leading)
        .overlay(alignment: .top) {
            if rule {
                Rectangle()
                    .fill(theme.line)
                    .frame(height: theme.contrast ? 1 : 0.5)
                    .padding(.leading, Spacing.card)
            }
        }
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
    }

    private func quiet(_ s: String) -> some View {
        Text(s)
            .font(theme.body(.subheadline))
            .foregroundStyle(theme.muted)
            .padding(Spacing.card)
            .frame(maxWidth: .infinity, alignment: .leading)
            .surface(.card, in: RoundedRectangle(cornerRadius: Radius.tile, style: .continuous))
    }

    private func answer() {
        asking = true
        askError = nil
        Task {
            do {
                try await model.openQuestions(slug)
            } catch {
                askError = Dashboard.words(error)
            }
            asking = false
        }
    }
}

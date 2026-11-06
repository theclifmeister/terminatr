import SwiftUI
import TerminatrKit
import TerminatrUI

/// Where a project's designs read from (T864, T888), and what reads them
/// again when it changes: the Designs tab and the Overview's line alike.
struct DesignsSource {
    let slug: String
    /// The paired server whose project this is (T888): its designs read
    /// through peer.design-list, read-only, without their pages or open
    /// questions; nil for one of this server's.
    let peer: AppAPI.Peer?
    /// What reads them again when it changes.
    let key: [String]

    /// A project of this server: read again when its board, its
    /// counts (a task's status, a done one) or its open questions
    /// change.
    init(project: AppAPI.Project) {
        slug = project.slug
        peer = nil
        key = project.board.map { "\($0.ref) \($0.status) \($0.updated)" } + project.counts.sorted { $0.key < $1.key }.map { "\($0.key)=\($0.value)" }
            + ["q\(project.questions)"]
    }

    /// A paired server's project: read again when its switch or its
    /// sessions' states change, as that server last said them.
    init(peer: AppAPI.Peer, project: AppAPI.PeerProject) {
        slug = project.project
        self.peer = peer
        key = [peer.alias, "\(project.pages)"] + project.sessions.map { "\($0.id) \($0.state) \($0.task)" }
    }

    enum Read {
        case designs([AppAPI.Design])
        case failed(String)
        /// That server doesn't share them (pages-off, no-pages, …).
        case notShared
    }

    @MainActor
    func read(_ app: AppModel) async -> Read {
        guard let peer else {
            do {
                return .designs(try await app.designs(slug))
            } catch {
                return .failed(Dashboard.words(error))
            }
        }
        guard app.readsPeerDesigns else { return .notShared }
        do {
            return .designs(try await app.peerDesigns(peer.alias, slug))
        } catch {
            let state: PeerPages.State<Int> = PeerPages.state(error)
            return state == .notShared ? .notShared : .failed(Dashboard.words(error))
        }
    }
}

/// The Designs tab's page: the section, scrolling as the dashboard does.
struct DesignsPane<Content: View>: View {
    @ViewBuilder let section: () -> Content

    var body: some View {
        ScrollView {
            section()
                .padding(.horizontal, 24)
                .padding(.vertical, 22)
                .frame(maxWidth: .infinity, alignment: .leading)
        }
        .scrollEdges()
    }
}

extension View {
    /// This view with the Overview's line of designs under way below it.
    func designsLine(_ source: DesignsSource) -> some View {
        modifier(DesignsLine(source: source))
    }
}

/// One small line, "2 designs: 1 building, 1 waiting for you", which opens
/// the Designs tab; nothing with neither, or while they can't be read.
/// A modifier, so a project with no line leaves no gap in the page.
private struct DesignsLine: ViewModifier {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let source: DesignsSource
    @State private var line: String?

    func body(content: Content) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            content
            if let line {
                Button(line) { app.projectTab = .designs }
                    .buttonStyle(.link)
                    .foregroundStyle(theme.action)
                    .font(theme.sized(Flat.label))
                    .help("Shows the project's designs")
            }
        }
        .task(id: source.key) {
            guard app.listsDesigns else { return }
            if case .designs(let designs) = await source.read(app) {
                line = Designs.underWay(designs)
            } else {
                line = nil
            }
        }
    }
}

/// A project's Designs tab (T864), as the TUI's Designs tab
/// lists them (design.list, app API 31): each design a row of a hairline
/// list with its title, open questions, a thin bar of its build and how
/// much is built, or its status, its other facts in its tooltip; a row
/// opens in place to its design task, answers and page, and the
/// decided answers, the open questions (Answer… has the coordinator put
/// them to the user, as the TUI's enter does) and the build tasks in
/// after order, a board task's opening the task view. Read again
/// whenever the board changes. A project without designs says so, and
/// a read that failed says why (Designs.listing). Nothing here changes
/// a design. A paired server's project's designs (T888) show the same,
/// read-only: no task opens, a design's page stays on that server, and
/// they carry no open questions.
struct DesignsSection: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let source: DesignsSource
    @State private var designs: [AppAPI.Design]?
    @State private var error: String?
    /// That server doesn't share them (pages-off, no-pages, …).
    @State private var notShared = false
    /// The designs opened to their detail, by name.
    @State private var open: Set<String> = []
    /// Whether the done and archived designs show too.
    @State private var all = false

    init(project: AppAPI.Project) {
        source = DesignsSource(project: project)
    }

    init(peer: AppAPI.Peer, project: AppAPI.PeerProject) {
        source = DesignsSource(peer: peer, project: project)
    }

    var body: some View {
        // A stack, not a Group: a Group with nothing in it has nothing to
        // run its task on, so the designs were never read (T876).
        ScrollViewReader { reader in
        VStack(alignment: .leading, spacing: 7) {
            let listing = Designs.listing(designs, error: error)
            let shown = designs.map { Designs.shown($0, all: all) } ?? []
            let heading = if case .designs = listing, !notShared { "Designs · \(shown.count)" } else { "Designs" }
            PaneLabel(heading)
            if notShared, let peer = source.peer {
                Text(PeerPages.notShared(by: PeerPages.server(peer)))
                    .font(theme.body(.subheadline))
                    .foregroundStyle(theme.muted)
            } else {
                switch listing {
                case .reading:
                    ProgressView().controlSize(.small)
                case .none:
                    Text(Designs.noneYet)
                        .font(theme.body(.subheadline))
                        .foregroundStyle(theme.muted)
                case .failed(let error):
                    Text("The designs didn't load: \(error)")
                        .font(theme.body(.subheadline))
                        .foregroundStyle(theme.attention)
                case .designs(let designs):
                    if !shown.isEmpty {
                        HairlineList {
                            ForEach(Designs.ordered(shown), id: \.name) { d in
                                DesignCard(project: source.slug, design: d, server: source.peer.map(PeerPages.server), open: Binding(
                                    get: { open.contains(d.name) },
                                    set: { if $0 { open.insert(d.name) } else { open.remove(d.name) } }))
                                .id(d.name)
                            }
                        }
                    }
                    let words = Designs.toggle(designs, all: all)
                    if !words.isEmpty {
                        Button(words) { all.toggle() }
                            .buttonStyle(.link)
                            .font(theme.body(.subheadline))
                    }
                }
            }
        }
        .guardingLinks()
        .task(id: source.key) {
            switch await source.read(app) {
            case .designs(let read):
                designs = read
                error = nil
                notShared = false
                // A chapter of History asked for this one: open, wherever it is.
                if let name = app.takeDesign(source.slug) {
                    open.insert(name)
                    if !Designs.shown(read, all: false).contains(where: { $0.name == name }) { all = true }
                    await Task.yield()
                    reader.scrollTo(name, anchor: .top)
                }
            case .failed(let why):
                error = why
            case .notShared:
                notShared = true
            }
        }
        }
    }
}

/// One design: its row, opened in place to its detail.
private struct DesignCard: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let project: String
    let design: AppAPI.Design
    /// The paired server's name, for one of its designs: read-only, no
    /// task opens and its page stays there; nil for this server's.
    var server: String?
    @Binding var open: Bool
    @State private var asking = false
    @State private var askError: String?

    var body: some View {
        ListRow {
            VStack(alignment: .leading, spacing: 10) {
                Button { open.toggle() } label: { summary.contentShape(Rectangle()) }
                    .buttonStyle(.plain)
                    .help(open ? "Folds \(design.title)" : "Shows \(design.title)'s answers, questions and build")
                    .accessibilityLabel(design.title)
                    .accessibilityValue(([open ? "open" : "folded", design.status, Designs.built(design)] + Designs.facts(design)).joined(separator: ", "))
                if open { detail }
            }
        }
    }

    private var summary: some View {
        HStack(spacing: 10) {
            Text(design.title)
                .lineLimit(1)
                .frame(minWidth: ThreadCard.titleMin, alignment: .leading)
                .layoutPriority(2)
            Spacer(minLength: 8)
            if !design.questions.isEmpty {
                Text(design.questions.count == 1 ? "1 open question" : "\(design.questions.count) open questions")
                    .font(theme.sized(Flat.label))
                    .foregroundStyle(theme.question)
                    .lineLimit(1)
                    .fixedSize()
            }
            if design.build.isEmpty && design.tasks.isEmpty {
                StatusTag(status: design.status)
            } else {
                Meter(fraction: Designs.progress(design), colour: Designs.Status(design.status) == .done ? theme.good : theme.action)
                    .frame(minWidth: 30, idealWidth: 90, maxWidth: 90)
                    .frame(height: 4)
                Text(Designs.built(design))
                    .font(theme.mono(.subheadline))
                    .foregroundStyle(theme.muted)
                    .fixedSize()
            }
            Image(systemName: "chevron.right")
                .rotationEffect(.degrees(open ? 90 : 0))
                .font(theme.body(.glyph, .semibold))
                .foregroundStyle(theme.muted)
                .accessibilityHidden(true)
        }
    }

    @ViewBuilder
    private var detail: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(([design.status] + Designs.facts(design)).joined(separator: " · "))
                .font(theme.sized(Flat.label))
                .foregroundStyle(theme.muted)
            if let url = Designs.page(design) {
                Link(destination: url) {
                    Label(design.artifact, systemImage: "link")
                        .lineLimit(1)
                        .truncationMode(.middle)
                }
                .font(theme.body(.subheadline))
                .help("Opens the design's page")
            }
            if let server, let stays = PeerPages.pageStays(design, server: server) {
                Label(stays, systemImage: "link")
                    .font(theme.body(.subheadline))
                    .foregroundStyle(theme.muted)
                    .help("A paired server's design pages never cross")
            }
            heading("Answers · \(design.answers.count)")
            if design.answers.isEmpty {
                muted("None recorded yet.")
            }
            ForEach(Array(design.answers.enumerated()), id: \.offset) { i, a in
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Text("\(i + 1).").font(theme.mono(.footnote)).foregroundStyle(theme.muted)
                    Text(a).textSelection(.enabled)
                }
            }
            if !design.questions.isEmpty && server == nil {
                heading("Open questions · \(design.questions.count)")
                ForEach(design.questions, id: \.id) { q in
                    HStack(alignment: .firstTextBaseline, spacing: 6) {
                        Text(q.id).font(theme.mono(.footnote, .medium)).foregroundStyle(theme.question)
                        Text(q.header.isEmpty ? q.question : "\(q.header): \(q.question)")
                    }
                }
                HStack(spacing: 8) {
                    Button("Answer…") { answer() }
                        .disabled(asking)
                        .help("Asks the coordinator to put its open questions to you; its question card answers them")
                    if let askError {
                        Text(askError).font(theme.body(.subheadline)).foregroundStyle(theme.attention)
                    }
                }
            }
            heading("Build · \(Designs.built(design))")
            if design.build.isEmpty {
                muted("No task builds it yet.")
            }
            ForEach(design.build, id: \.id) { t in
                buildRow(t)
            }
        }
    }

    private func buildRow(_ t: AppAPI.DesignTask) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Button { app.showTask(project: project, task: t.id) } label: {
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Text(t.id)
                        .font(theme.mono(.subheadline))
                        .foregroundStyle(theme.muted)
                    Text(t.title).lineLimit(1)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .disabled(t.archived || server != nil)
            .help(server.map { "\(t.id) is on \($0)'s board" } ?? (t.archived ? "\(t.id) is archived" : "Shows \(t.id) in full"))
            StatusTag(status: t.status)
            let after = Designs.after(t)
            if !after.isEmpty {
                Text(after).font(theme.sized(Flat.label)).foregroundStyle(theme.muted)
            }
            if t.archived {
                Text("archived").font(theme.sized(Flat.label)).foregroundStyle(theme.muted)
            }
        }
    }

    private func heading(_ s: String) -> some View {
        PaneLabel(s)
            .padding(.top, 2)
    }

    private func muted(_ s: String) -> some View {
        Text(s).font(theme.body(.subheadline)).foregroundStyle(theme.muted)
    }

    private func answer() {
        asking = true
        askError = nil
        Task {
            do {
                try await app.openQuestions(project)
            } catch {
                askError = Dashboard.words(error)
            }
            asking = false
        }
    }
}

/// A design's or a task's status as a small word in its colour.
private struct StatusTag: View {
    @Environment(\.theme) private var theme
    let status: String

    var body: some View {
        Text(status)
            .font(theme.sized(Flat.label))
            .foregroundStyle(colour)
            .lineLimit(1)
            .fixedSize()
    }

    private var colour: Color {
        switch status {
        case "building", "started", "done": theme.good
        case "designing": theme.action
        case "review": theme.question
        case "blocked": theme.attention
        default: theme.muted
        }
    }
}

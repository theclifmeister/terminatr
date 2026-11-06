import SwiftUI
import TerminatrKit
import TerminatrUI

/// Where a project's designs read from (T864, T888), and what reads them
/// again when it changes: its Designs page and its screen's line alike, as
/// the Mac's DesignsSource.
struct DesignsSource {
    let slug: String
    /// The paired server's alias, for one of its projects; nil for this
    /// server's.
    let peer: String?
    /// What reads them again when it changes.
    let key: AnyHashable

    init(project: AppAPI.Project, sessions: [AppAPI.Session], generation: Int) {
        slug = project.slug
        peer = nil
        key = DesignsRead(project: project, sessions: sessions, generation: generation)
    }

    /// A paired server's project: read again when its switch or its
    /// sessions change, as that server last said them, or the link
    /// came back.
    init(alias: String, project: AppAPI.PeerProject, generation: Int) {
        slug = project.project
        peer = alias
        key = PeerDesignsRead(project: project, generation: generation)
    }

    enum Read {
        case designs([AppAPI.Design])
        case failed(String)
        /// That server doesn't share them.
        case notShared
    }

    @MainActor
    func read(_ model: PhoneModel) async -> Read {
        do {
            guard let peer else { return .designs(try await model.designs(slug)) }
            guard model.readsPeerDesigns else { return .notShared }
            return .designs(try await model.peerDesigns(peer, slug))
        } catch {
            let state: PeerPages.State<Int> = PeerPages.state(error)
            return peer != nil && state == .notShared ? .notShared : .failed(Dashboard.words(error))
        }
    }
}

/// A project's Designs page (T864, T1068), scrolling under its bar. A paired
/// server's opens through PeerPageScreen, which draws the bar for it.
struct DesignsScreen: View {
    @Environment(PhoneModel.self) private var model
    let slug: String

    var body: some View {
        DesignsPage(source: model.project(slug).map { DesignsSource(project: $0, sessions: model.sessions, generation: model.generation) },
                    route: .designs(slug))
            .pageBar("Designs")
            .backdrop()
    }
}

/// The page's list, in a scroll view that returns to the design it opened.
struct DesignsPage: View {
    @Environment(PhoneModel.self) private var model
    let source: DesignsSource?
    let route: Route

    var body: some View {
        ScrollView {
            if let source {
                DesignsTiles(source: source)
                    .padding(Spacing.gutter)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
        }
        .refreshable { await model.refresh() }
        .keepsPlace(route: route)
    }
}

/// A project's designs (T864), as the TUI's Designs tab and the Mac's list
/// them (design.list, app API 31): one DesignTile each, those with open
/// questions first (Designs.ordered), each opening its DesignScreen. Read
/// again when the board or the link changes. A project without designs says
/// so, apart from a read that failed (Designs.listing). A paired server's
/// project's designs (T888, peer.design-list) show the same, read-only, or
/// "Not shared by <server>".
struct DesignsTiles: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.navigate) private var navigate
    @Environment(\.theme) private var theme
    let source: DesignsSource
    /// nil until design.list answered.
    @State private var designs: [AppAPI.Design]?
    @State private var error: String?
    /// That server doesn't share them.
    @State private var notShared = false
    /// Whether the done and archived designs show too.
    @State private var all = false

    var body: some View {
        // A ZStack, not a Group: its task runs while it shows nothing.
        ZStack(alignment: .topLeading) {
            Color.clear.frame(height: 0)
            VStack(alignment: .leading, spacing: Spacing.row + 2) {
                if notShared {
                    quietCard(PeerPages.notShared(by: model.peers.first(where: { $0.alias == source.peer }).map(PeerPages.server) ?? source.peer ?? ""))
                } else {
                    switch Designs.listing(designs, error: error) {
                    case .reading:
                        EmptyView()
                    case .none:
                        quietCard(Designs.noneYet)
                    case .failed(let error):
                        Failure(text: "The designs didn't load: \(error)")
                    case .designs(let designs):
                        let shown = Designs.shown(designs, all: all)
                        if !shown.isEmpty {
                            TileGroup {
                                ForEach(Array(Designs.ordered(shown).enumerated()), id: \.element.name) { i, d in
                                    let route = source.peer.map { Route.peerDesign(alias: $0, project: source.slug, name: d.name) } ?? Route.design(source.slug, name: d.name)
                                    PlainRouteLink(value: route) {
                                        DesignTile(d, rule: i > 0, selected: navigate.opens(route, on: model))
                                    }
                                    .buttonStyle(.plain)
                                }
                            }
                        }
                        let words = Designs.toggle(designs, all: all)
                        if !words.isEmpty {
                            Button(words) { all.toggle() }
                                .font(.subheadline)
                        }
                    }
                }
            }
        }
        // Kept, so a list laid out afresh (the columns move on when a design
        // opens) has its rows at once, and its scroll place with them.
        .kept($designs, "designs \(model.key) \(source.peer ?? "")/\(source.slug)")
        .kept($all, "designs all \(model.key) \(source.peer ?? "")/\(source.slug)")
        .task(id: source.key) {
            switch await source.read(model) {
            case .designs(let read):
                designs = read
                error = nil
                notShared = false
            case .notShared:
                notShared = true
            case .failed(let why):
                error = why
            }
        }
    }

    private func quietCard(_ s: String) -> some View {
        Text(s)
            .font(theme.body(.subheadline))
            .foregroundStyle(theme.muted)
            .padding(Spacing.card)
            .frame(maxWidth: .infinity, alignment: .leading)
            .surface(in: RoundedRectangle(cornerRadius: Radius.tile, style: .continuous))
    }
}

extension View {
    /// This view with its project's line of designs under way below it
    /// (T1068), opening route; nil source, no line.
    func designsLine(_ source: DesignsSource?, opens route: Route) -> some View {
        modifier(DesignsLine(source: source, route: route))
    }
}

/// One small line, "6 designs: 4 building, 2 waiting for you", which opens the
/// project's Designs page; nothing with neither, or until they are read. A
/// modifier, so a project with no line leaves no gap on its screen.
private struct DesignsLine: ViewModifier {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    let source: DesignsSource?
    let route: Route
    @State private var line: String?

    func body(content: Content) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            content
            if let line {
                PlainRouteLink(value: route) {
                    Text(line)
                        .font(theme.sized(Flat.label))
                        .foregroundStyle(theme.action)
                        .frame(maxWidth: .infinity, minHeight: tapTarget, alignment: .leading)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityHint("Opens the project's designs")
            }
        }
        .task(id: source?.key) {
            guard let source else { return }
            switch await source.read(model) {
            case .designs(let designs): line = Designs.underWay(designs)
            case .notShared: line = nil
            // A link that dropped: the last line stands, as the list's rows do.
            case .failed: break
            }
        }
    }
}

/// When a paired server's project's designs are read again: its pages
/// switch or its sessions' states changed, as that server last said
/// them, or the link came back.
struct PeerDesignsRead: Hashable {
    let project: String
    let pages: Bool
    let sessions: [String]
    let generation: Int

    init(project p: AppAPI.PeerProject, generation: Int) {
        project = p.project
        pages = p.pages
        sessions = p.sessions.map { "\($0.id) \($0.state) \($0.task)" }
        self.generation = generation
    }
}

/// When a project's designs are read again: its board, counts or open
/// questions changed, a thread started or ended a turn on a task
/// (Designs.working), or the link came back.
struct DesignsRead: Hashable {
    let slug: String
    let board: [String]
    let counts: [String: Int]
    let questions: Int
    let working: [String]
    let generation: Int

    init(project p: AppAPI.Project, sessions: [AppAPI.Session], generation: Int) {
        slug = p.slug
        board = p.board.map { "\($0.ref) \($0.status) \($0.updated)" }
        counts = p.counts
        questions = p.questions
        working = Designs.working(sessions, project: p.slug)
        self.generation = generation
    }
}

/// One design in full: its status, title and page, its decided answers,
/// its design task's open questions (at the answer level Answer… has
/// the coordinator put them to the user, as the TUI's enter does) and
/// its build tasks in after order, each a board task's opening its
/// TaskScreen. Nothing here changes a design: the coordinator records
/// them (tm design set). A paired server's design (T888, peer set)
/// shows the same read-only: no task opens, its page stays there, and
/// it carries no open questions.
struct DesignScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    @Environment(\.navigate) private var navigate
    let slug: String
    let name: String
    /// The paired server's alias, for one of its projects' designs.
    var peer: String?
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
                    if !d.questions.isEmpty && peer == nil { questions(d) }
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
        .task(id: readKey) {
            do {
                let all = if let peer { try await model.peerDesigns(peer, slug) } else { try await model.designs(slug) }
                design = all.first { $0.name == name }
                error = nil
            } catch {
                self.error = Dashboard.words(error)
            }
            loaded = true
        }
        .refreshable { await model.refresh() }
        .pageBar("Design")
        .backdrop()
    }

    /// What reads the design again when it changes.
    private var readKey: AnyHashable? {
        if let peer {
            return model.peers.first(where: { $0.alias == peer })?.projects.first(where: { $0.project == slug })
                .map { AnyHashable(PeerDesignsRead(project: $0, generation: model.generation)) }
        }
        return model.project(slug).map { AnyHashable(DesignsRead(project: $0, sessions: model.sessions, generation: model.generation)) }
    }

    /// The paired server's name, for one of its designs.
    private var server: String? {
        peer.map { alias in model.peers.first(where: { $0.alias == alias }).map(PeerPages.server) ?? alias }
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
                if d.taskStatus.isEmpty || peer != nil {
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
            if let server, let stays = PeerPages.pageStays(d, server: server) {
                Label(stays, systemImage: "link")
                    .font(theme.body(.subheadline))
                    .foregroundStyle(theme.muted)
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
                    if t.archived || peer != nil {
                        buildRow(t, rule: i > 0)
                    } else {
                        let route = Route.task(slug, ref: t.id)
                        PlainRouteLink(value: route) {
                            buildRow(t, rule: i > 0)
                                .background(navigate.opens(route, on: model) ? theme.wash(.working) : .clear)
                        }
                        .buttonStyle(.plain)
                    }
                }
            }
        }
    }

    private func buildRow(_ t: AppAPI.DesignTask, rule: Bool) -> some View {
        line(rule: rule) {
            StateGlyph(Designs.tone(task: t), side: 24, symbol: Designs.symbol(task: t))
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
            .surface(in: RoundedRectangle(cornerRadius: Radius.tile, style: .continuous))
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

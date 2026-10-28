import SwiftUI
import TerminatrKit
import TerminatrUI

/// The projects as the TUI's and the Mac's sidebar list them
/// (SidebarTree): this server's active ones, pinned first; each paired
/// server's shared ones; and the inactive ones folded under
/// "Inactive (n)". A project opens its coordinator and threads.
struct ProjectsScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    @State private var showInactive = false

    var body: some View {
        List {
            if let f = model.failure { Failure(text: f) }
            Section(model.peers.isEmpty ? "" : "This server") {
                ForEach(model.activeProjects, id: \.slug) { p in
                    NavigationLink(value: Route.project(p.slug)) { line(p) }
                }
            }
            ForEach(model.peers, id: \.alias) { peer in
                Section("\(peer.name.isEmpty ? peer.alias : peer.name) · \(PeerTag.of(peer).word)") {
                    ForEach(peer.projects, id: \.project) { p in
                        let coordinator = p.sessions.first { SessionRef($0, of: peer.alias)?.isCoordinator == true }
                        NavigationLink(value: Route.peerProject(alias: peer.alias, project: p.project)) {
                            ProjectLine(name: p.project, state: .of(peer: coordinator), needs: p.stale ? 0 : p.needsYou,
                                        dim: peer.state != "up" || p.stale)
                        }
                    }
                }
            }
            let gone = model.inactiveProjects
            if !gone.isEmpty {
                Section {
                    DisclosureGroup("Inactive (\(gone.count))", isExpanded: $showInactive) {
                        ForEach(gone, id: \.slug) { p in
                            NavigationLink(value: Route.project(p.slug)) { line(p).opacity(0.7) }
                        }
                    }
                    .font(theme.body(.body, .medium))
                    .foregroundStyle(theme.muted)
                }
            }
        }
        .refreshable { await model.refresh() }
        .navigationTitle("Projects")
        .toolbar { SettingsButton() }
        .backdrop()
    }

    private func line(_ p: AppAPI.Project) -> some View {
        ProjectLine(name: p.slug, state: .of(coordinator: model.coordinatorSession(p.slug), in: p),
                    needs: Need.count(model.needs, project: p.slug))
    }
}

/// One of this server's projects: its goal, its coordinator, running or
/// not (at the write level Start Coordinator under it), then every
/// unresolved thread, done ones too, in the TUI's order
/// (SidebarTree.threads). An inactive project's rows are dormant.
struct ProjectScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    let slug: String

    var body: some View {
        List {
            if let p = model.project(slug) {
                let dormant = !p.active
                if !p.goal.isEmpty {
                    Text(p.goal)
                        .font(theme.body(.subheadline))
                        .foregroundStyle(theme.muted)
                        .listRowBackground(Color.clear)
                }
                Section {
                    NavigationLink(value: Route.session(SessionRef(project: slug))) {
                        let c = model.coordinatorSession(slug)
                        SessionLine(label: "Coordinator", ref: nil, state: .of(coordinator: c, in: p),
                                    busy: Busy.of(coordinator: c, in: p), dormant: dormant)
                    }
                    if p.active, model.coordinatorSession(slug) == nil, model.can(AppAPI.Methods.projectOpen.name) {
                        StartCoordinatorButton(project: slug)
                    }
                }
                pages(p)
                Section("Threads") {
                    ForEach(SidebarTree.threads(of: p) { model.session($0) }, id: \.record.id) { t in
                        NavigationLink(value: Route.session(SessionRef(project: slug, thread: t.record.id))) {
                            SessionLine(label: t.record.title, ref: t.task?.ref, state: model.attention(t), busy: model.busy(t),
                                        pr: t.pr, dormant: dormant)
                        }
                    }
                }
            } else {
                ContentUnavailableView("No project \(slug)", systemImage: "folder.badge.questionmark")
            }
        }
        .refreshable { await model.refresh() }
        .navigationTitle(slug)
        .backdrop()
    }

    /// The project's read-only pages, as the Mac's beside its dashboard:
    /// Memory and Library with a tm that reads them.
    private func pages(_ p: AppAPI.Project) -> some View {
        let pages = model.pages
        let board = [Dashboard.onDeck(p).count, Dashboard.backlog(p).count, model.listsDone ? Dashboard.doneCount(p) : 0]
        return Section("Project") {
            NavigationLink(value: Route.overview(slug)) { Label("Overview", systemImage: "info.circle") }
            NavigationLink(value: Route.board(slug)) {
                LabeledContent {
                    Text(Self.boardWords(board)).font(theme.mono(.caption))
                } label: {
                    Label("Board", systemImage: "list.bullet.rectangle")
                }
            }
            if pages.contains(.memory) {
                NavigationLink(value: Route.memory(slug)) { Label("Memory", systemImage: "brain") }
            }
            if pages.contains(.library) {
                NavigationLink(value: Route.library(slug)) { Label("Library", systemImage: "books.vertical") }
            }
        }
        .font(theme.body(.body))
    }

    /// "2 on deck · 3 backlog · 9 done", the empty ones left out.
    static func boardWords(_ n: [Int]) -> String {
        zip(n, ["on deck", "backlog", "done"]).filter { $0.0 > 0 }.map { "\($0.0) \($0.1)" }.joined(separator: " · ")
    }
}

/// A paired server's shared project: its coordinator, running there or
/// not, and its threads (SidebarTree.sessions), as the Mac lists them.
struct PeerProjectScreen: View {
    @Environment(PhoneModel.self) private var model
    let alias: String
    let project: String

    var body: some View {
        List {
            if let p = model.peers.first(where: { $0.alias == alias })?.projects.first(where: { $0.project == project }) {
                ForEach(SidebarTree.sessions(of: p, alias: alias), id: \.ref) { s, ref, task, title in
                    NavigationLink(value: Route.session(ref)) {
                        SessionLine(label: ref.isCoordinator ? "Coordinator" : title.isEmpty ? ref.thread : title, ref: task.isEmpty ? nil : task,
                                    state: .of(peer: s))
                    }
                }
            }
        }
        .refreshable { await model.refresh() }
        .navigationTitle(project)
        .backdrop()
    }
}

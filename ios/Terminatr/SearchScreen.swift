import SwiftUI
import TerminatrKit
import TerminatrUI

/// Search over a project's history (T924), as the Mac's Search Project…
/// and the TUI's search popup: a project picked (this server's only),
/// words, a kind, then the hits (kind, id, title, day), each opening its
/// SearchHitScreen. The Search tab's (T929, Root.searchTab), on the
/// project in view; on iOS 17 from the Projects header and a project's
/// screen (Route.search). -open search:SLUG opens it. The search stays
/// while the screen is in the path, so Back from a hit comes back to
/// its hits.
struct SearchScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    let slug: String
    @State private var search: SearchModel?
    @FocusState private var focused: Bool

    var body: some View {
        VStack(spacing: 0) {
            if let search {
                // Above the list, not a row of its inset-grouped section:
                // that section's rounded clip cut the chips short at both
                // ends (T931). Here they scroll to the screen's edges.
                query(search)
                PanelPage { results(search) }
            } else {
                Spacer(minLength: 0)
            }
        }
        .task {
            if search == nil {
                let m = SearchModel(project: slug, calls: model.searchCalls)
                // A launch argument's words (-search WORDS) search at once.
                if let w = UserDefaults.standard.string(forKey: "search"), !w.isEmpty {
                    m.words = w
                    search = m
                    await m.search()
                } else {
                    search = m
                }
            }
        }
        .pageBar("Search")
        .backdrop()
    }

    /// The project searched, a menu of this server's others, above the
    /// words: in the screen, not the bar's title, which an open iPhone
    /// Duo's column never showed (its bar goes to the rail, T929).
    private var projectMenu: some View {
        Menu {
            ForEach(model.activeProjects + model.inactiveProjects, id: \.slug) { p in
                Button {
                    search?.switchProject(p.slug)
                } label: {
                    if p.slug == search?.project { Label(p.slug, systemImage: "checkmark") } else { Text(p.slug) }
                }
            }
        } label: {
            HStack(spacing: 4) {
                Text(search?.project ?? slug)
                    .font(theme.sized(14.5, .medium))
                Image(systemName: "chevron.down")
                    .font(theme.body(.caption, .semibold))
            }
            .foregroundStyle(theme.ink)
        }
        .accessibilityLabel("Project, \(search?.project ?? slug)")
        .accessibilityHint("Picks the project to search")
    }

    private func query(_ s: SearchModel) -> some View {
        @Bindable var s = s
        return VStack(alignment: .leading, spacing: 10) {
            projectMenu
                .padding(.horizontal, Spacing.gutter)
            HStack(spacing: 8) {
                Image(systemName: "magnifyingglass")
                    .foregroundStyle(theme.muted)
                TextField("Search \(s.project)'s history", text: $s.words)
                    .font(theme.sized(Flat.text))
                    .focused($focused)
                    .submitLabel(.search)
                    .autocorrectionDisabled()
                    .textInputAutocapitalization(.never)
                    .onSubmit { Task { await s.search() } }
                if s.searching { ProgressView() }
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 10)
            .surface(in: RoundedRectangle(cornerRadius: Radius.row))
            .padding(.horizontal, Spacing.gutter)
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 6) {
                    kindChip(s, id: "", label: "All")
                    ForEach(Search.kinds) { kindChip(s, id: $0.id, label: $0.label) }
                }
                .padding(.horizontal, Spacing.gutter)
            }
        }
        .padding(.vertical, 8)
    }

    private func kindChip(_ s: SearchModel, id: String, label: String) -> some View {
        let on = s.kind == id
        return Button {
            s.kind = id
            if s.canSearch { Task { await s.search() } }
        } label: {
            Text(label)
                .font(theme.body(.subheadline, on ? .semibold : .regular))
                .padding(.horizontal, 12)
                .padding(.vertical, 6)
                .foregroundStyle(on ? theme.onAction : theme.ink)
                .background(on ? theme.action : theme.pane, in: RoundedRectangle(cornerRadius: Radius.row))
                .overlay(RoundedRectangle(cornerRadius: Radius.row).strokeBorder(theme.line, lineWidth: on ? 0 : 1))
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(on ? .isSelected : [])
    }

    @ViewBuilder
    private func results(_ s: SearchModel) -> some View {
        if let failure = s.failure {
            Failure(text: failure)
        } else if let r = s.result {
            SectionHeading(Search.summary(r))
            if !r.hits.isEmpty {
                PanelList {
                    ForEach(r.hits) { h in
                        RowLink(value: Route.searchHit(s.project, id: h.id)) {
                            SearchHitRow(h)
                        }
                    }
                }
            }
        } else if !s.searching {
            Group {
                Text("Notes, tasks, threads' briefs and reports, the journal, designs, the library and the inbox. A first search builds the project's index: it can take a few seconds.")
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.muted)
            }
        }
    }
}

/// A search hit (T924): its kind, id, day and title, what it opens (its
/// task, its thread, its note), then its text or its timeline, picked
/// above them; a timeline row opens that chunk's own screen.
struct SearchHitScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    let slug: String
    let id: String
    @State private var search: SearchModel?
    /// Text (0) or Timeline (1); a launch argument (-searchLayer 1) picks it.
    @State private var layer = UserDefaults.standard.integer(forKey: "searchLayer")

    var body: some View {
        PanelPage {
            if let s = search {
                if let hit = s.hit, !hit.kind.isEmpty {
                    PanelList {
                        ListRow { SearchHitHeader(hit) }
                        ForEach(Search.targets(hit)) { t in
                            target(t)
                        }
                    }
                }
                MiniTabs([(id: 0, title: "Text"), (id: 1, title: "Timeline")], selection: $layer)
                if layer == 0 { text(s) } else { timeline(s) }
            }
        }
        .task(id: model.generation) {
            let s = search ?? SearchModel(project: slug, calls: model.searchCalls)
            search = s
            await s.show(id: id)
        }
        .refreshable { await search?.show(id: id) }
        .pageBar(id)
        .backdrop()
    }

    @ViewBuilder
    private func target(_ t: Search.Target) -> some View {
        switch t {
        case .task(let ref):
            RowLink(value: Route.task(slug, ref: ref)) { Label(t.label, systemImage: t.symbol) }
        case .thread(let th):
            if model.project(slug)?.threads.contains(where: { $0.record.id == th }) == true {
                RowLink(value: Route.session(SessionRef(project: slug, thread: th))) { Label(t.label, systemImage: t.symbol) }
            } else {
                ListRow {
                    Label("\(th) is resolved", systemImage: t.symbol)
                        .foregroundStyle(theme.muted)
                }
            }
        case .note(let path):
            if model.pages.contains(.memory) {
                RowLink(value: Route.memoryFile(slug, path: path)) { Label(t.label, systemImage: t.symbol) }
            }
        }
    }

    @ViewBuilder
    private func text(_ s: SearchModel) -> some View {
        if let failure = s.shownFailure {
            Failure(text: failure)
        } else if let t = s.text {
            MarkdownView(text: t.text)
                .font(theme.sized(Flat.text))
                .foregroundStyle(theme.ink)
                .textSelection(.enabled)
            if t.cut {
                Text("Cut at \(t.text.utf8.count) of \(t.bytes) bytes: open it for the rest.")
                    .font(theme.sized(12.5))
                    .foregroundStyle(theme.muted)
            }
        } else {
            ProgressView()
        }
    }

    @ViewBuilder
    private func timeline(_ s: SearchModel) -> some View {
        if let tl = s.timeline {
            SectionHeading("Its task's and thread's history around it")
            PanelList {
                ForEach(Search.rows(tl)) { row in
                    if row.at {
                        ListRow(selected: true) { SearchHitRow(row.hit, at: true) }
                    } else {
                        RowLink(value: Route.searchHit(slug, id: row.hit.id)) { SearchHitRow(row.hit) }
                    }
                }
            }
        } else if s.shownFailure != nil {
            Text("No timeline for this hit.")
                .font(theme.sized(Flat.text))
                .foregroundStyle(theme.muted)
        } else {
            ProgressView()
        }
    }
}

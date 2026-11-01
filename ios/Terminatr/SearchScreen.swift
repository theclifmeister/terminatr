import SwiftUI
import TerminatrKit
import TerminatrUI

/// Search over a project's history (T924), as the Mac's Search Project…
/// and the TUI's search popup: a project picked (this server's only),
/// words, a kind, then the hits (kind, id, title, day), each opening its
/// SearchHitScreen. From the Projects header and a project's pages
/// (Route.search, -open search:SLUG). The search stays while the screen
/// is in the path, so Back from a hit comes back to its hits.
struct SearchScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    let slug: String
    @State private var search: SearchModel?
    @FocusState private var focused: Bool

    var body: some View {
        List {
            if let search {
                Section {
                    query(search)
                }
                .listRowBackground(Color.clear)
                results(search)
            }
        }
        .listStyle(.insetGrouped)
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
                    focused = true
                }
            }
        }
        .navigationTitle("Search")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar {
            ToolbarItem(placement: .principal) { projectMenu }
        }
        .backdrop()
    }

    /// The title: the project searched, a menu of this server's others.
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
                    .font(theme.title(.headline, .bold))
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
            HStack(spacing: 8) {
                Image(systemName: "magnifyingglass")
                    .foregroundStyle(theme.muted)
                TextField("Search \(s.project)'s history", text: $s.words)
                    .font(theme.body(.body))
                    .focused($focused)
                    .submitLabel(.search)
                    .autocorrectionDisabled()
                    .textInputAutocapitalization(.never)
                    .onSubmit { Task { await s.search() } }
                if s.searching { ProgressView() }
            }
            .padding(.horizontal, 12)
            .padding(.vertical, 10)
            .surface(.card, in: RoundedRectangle(cornerRadius: Radius.tile, style: .continuous))
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(spacing: 6) {
                    kindChip(s, id: "", label: "All")
                    ForEach(Search.kinds) { kindChip(s, id: $0.id, label: $0.label) }
                }
            }
        }
        .listRowInsets(EdgeInsets(top: 4, leading: 0, bottom: 4, trailing: 0))
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
                .background(on ? theme.action : theme.cardTint, in: Capsule())
                .overlay(Capsule().strokeBorder(theme.edge, lineWidth: on ? 0 : 1))
        }
        .buttonStyle(.plain)
        .accessibilityAddTraits(on ? .isSelected : [])
    }

    @ViewBuilder
    private func results(_ s: SearchModel) -> some View {
        if let failure = s.failure {
            Section { Failure(text: failure) }
        } else if let r = s.result {
            Section {
                ForEach(r.hits) { h in
                    PlainRouteLink(value: Route.searchHit(s.project, id: h.id)) {
                        SearchHitRow(h)
                    }
                }
            } header: {
                Text(Search.summary(r))
            }
        } else if !s.searching {
            Section {
                Text("Notes, tasks, threads' briefs and reports, the journal, designs, the library and the inbox. A first search builds the project's index: it can take a few seconds.")
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.muted)
            }
            .listRowBackground(Color.clear)
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
    @State private var layer = 0

    var body: some View {
        List {
            if let s = search {
                if let hit = s.hit, !hit.kind.isEmpty {
                    Section {
                        SearchHitHeader(hit)
                            .padding(.vertical, 4)
                        ForEach(Search.targets(hit)) { t in
                            target(t)
                        }
                    }
                }
                Section {
                    Picker("Show", selection: $layer) {
                        Text("Text").tag(0)
                        Text("Timeline").tag(1)
                    }
                    .pickerStyle(.segmented)
                    .listRowBackground(Color.clear)
                    .listRowInsets(EdgeInsets())
                }
                if layer == 0 { text(s) } else { timeline(s) }
            }
        }
        .listStyle(.insetGrouped)
        .task(id: model.generation) {
            let s = search ?? SearchModel(project: slug, calls: model.searchCalls)
            search = s
            await s.show(id: id)
        }
        .refreshable { await search?.show(id: id) }
        .navigationTitle(id)
        .navigationBarTitleDisplayMode(.inline)
        .backdrop()
    }

    @ViewBuilder
    private func target(_ t: Search.Target) -> some View {
        switch t {
        case .task(let ref):
            PlainRouteLink(value: Route.task(slug, ref: ref)) { Label(t.label, systemImage: t.symbol) }
        case .thread(let th):
            if model.project(slug)?.threads.contains(where: { $0.record.id == th }) == true {
                PlainRouteLink(value: Route.session(SessionRef(project: slug, thread: th))) { Label(t.label, systemImage: t.symbol) }
            } else {
                Label("\(th) is resolved", systemImage: t.symbol)
                    .foregroundStyle(theme.muted)
            }
        case .note(let path):
            if model.pages.contains(.memory) {
                PlainRouteLink(value: Route.memoryFile(slug, path: path)) { Label(t.label, systemImage: t.symbol) }
            }
        }
    }

    @ViewBuilder
    private func text(_ s: SearchModel) -> some View {
        Section {
            if let failure = s.shownFailure {
                Failure(text: failure)
            } else if let t = s.text {
                MarkdownView(text: t.text)
                    .textSelection(.enabled)
                    .padding(.vertical, 6)
                if t.cut {
                    Text("Cut at \(t.text.utf8.count) of \(t.bytes) bytes: open it for the rest.")
                        .font(theme.body(.footnote))
                        .foregroundStyle(theme.muted)
                }
            } else {
                ProgressView()
            }
        }
    }

    @ViewBuilder
    private func timeline(_ s: SearchModel) -> some View {
        if let tl = s.timeline {
            Section {
                ForEach(Search.rows(tl)) { row in
                    if row.at {
                        SearchHitRow(row.hit, at: true)
                            .listRowBackground(theme.cardTint)
                    } else {
                        PlainRouteLink(value: Route.searchHit(slug, id: row.hit.id)) { SearchHitRow(row.hit) }
                    }
                }
            } header: {
                Text("Its task's and thread's history around it")
            }
        } else if s.shownFailure != nil {
            Section { Text("No timeline for this hit.").foregroundStyle(theme.muted) }
        } else {
            Section { ProgressView() }
        }
    }
}

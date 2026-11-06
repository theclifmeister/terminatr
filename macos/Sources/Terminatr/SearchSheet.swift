import SwiftUI
import TerminatrKit
import TerminatrUI

/// Search Project… (⇧⌘F, T924): tm search over a project's history in
/// three layers, as the TUI's search popup: the hits (kind, id, title,
/// day), a hit's text, and its timeline, whose rows show their own
/// text; Open shows the hit's task, thread or note in the window. This
/// server's projects only. The search stays in AppModel while the sheet
/// is closed, so ⇧⌘F comes back to it.
struct SearchSheet: View {
    @Environment(AppModel.self) private var app
    @Environment(\.dismiss) private var dismiss
    @Environment(\.theme) private var theme
    @Bindable var model: SearchModel
    @State private var layer: Layer = .text
    @State private var listWidth = 340.0
    @FocusState private var fieldFocused: Bool

    enum Layer: String, CaseIterable {
        case text = "Text"
        case timeline = "Timeline"
    }

    var body: some View {
        FlatSheet {
            bar
        } content: {
            PickSplit(width: $listWidth, range: 280...440) {
                hits
            } detail: {
                detail
            }
            .frame(minWidth: 760, idealWidth: 920, maxWidth: .infinity, minHeight: 480, idealHeight: 620, maxHeight: .infinity)
        } footer: {
            Text("Searches \(model.project)'s notes, tasks, threads, journal, designs, library and inbox")
                .font(theme.sized(Flat.label))
                .foregroundStyle(theme.muted)
            Spacer()
            Button("Close") { dismiss() }
                .keyboardShortcut(.cancelAction)
        }
        .onAppear { fieldFocused = true }
    }

    // MARK: the query

    private var bar: some View {
        HStack(spacing: 10) {
            SheetPicker(label: "Project", shown: model.project, selection: Binding(get: { model.project }, set: { model.switchProject($0) })) {
                ForEach(app.activeProjects + app.inactiveProjects, id: \.slug) { Text($0.slug).tag($0.slug) }
            }
            .help("The project whose history to search")
            HStack(spacing: 6) {
                Image(systemName: "magnifyingglass")
                    .foregroundStyle(theme.muted)
                TextField("Search \(model.project)'s history", text: $model.words)
                    .focused($fieldFocused)
                    .onSubmit { Task { await model.search() } }
                if model.searching {
                    ProgressView().controlSize(.small)
                }
            }
            .sheetField()
            SheetPicker(label: "Kind", shown: Search.kinds.first { $0.id == model.kind }?.label ?? "All kinds", selection: $model.kind) {
                Text("All kinds").tag("")
                Divider()
                ForEach(Search.kinds) { Label($0.label, systemImage: $0.symbol).tag($0.id) }
            }
            .help("Only hits of this kind")
            .onChange(of: model.kind) {
                if model.result != nil { Task { await model.search() } }
            }
            Button("Search") { Task { await model.search() } }
                .buttonStyle(.flatPrimary)
                .keyboardShortcut(.defaultAction)
                .disabled(!model.canSearch || model.searching)
        }
        .padding(14)
    }

    // MARK: layer 1: the hits

    private var hits: some View {
        VStack(alignment: .leading, spacing: 0) {
            if let failure = model.failure {
                Text(failure)
                    .foregroundStyle(theme.attention)
                    .padding(16)
                Spacer()
            } else if let r = model.result {
                Text(Search.summary(r))
                    .font(theme.monoSized(Flat.label))
                    .foregroundStyle(theme.muted)
                    .padding(.horizontal, 16)
                    .padding(.top, 10)
                    .padding(.bottom, 4)
                let picked = Binding(get: { model.shown?.id },
                                     set: { id in if let h = r.hits.first(where: { $0.id == id }) { open(h) } })
                PickList(selection: picked, order: r.hits.map(\.id), name: "Hits") {
                    ForEach(r.hits) { h in
                        PickRow(id: h.id, selection: picked) { SearchHitRow(h) }
                    }
                }
            } else {
                VStack(alignment: .leading, spacing: 8) {
                    Text(model.searching ? "Searching…" : "Words to find, then Return.")
                        .foregroundStyle(theme.muted)
                    Text("A first search builds the project's index: it can take a few seconds.")
                        .font(theme.sized(Flat.label))
                        .foregroundStyle(theme.muted)
                }
                .padding(16)
                Spacer()
            }
        }
    }

    // MARK: layers 2 and 3: the hit's text and timeline

    @ViewBuilder
    private var detail: some View {
        if let hit = model.shown {
            VStack(alignment: .leading, spacing: 0) {
                VStack(alignment: .leading, spacing: 12) {
                    SearchHitHeader(hit)
                    HStack(spacing: 8) {
                        ForEach(Search.targets(hit)) { t in
                            Button { go(t) } label: { Label(t.label, systemImage: t.symbol) }
                                .disabled(!opens(t))
                                .help(help(t))
                        }
                        Spacer()
                        ForEach(Layer.allCases, id: \.self) { l in
                            Button(l.rawValue) { layer = l }
                                .buttonStyle(FlatButtonStyle(primary: layer == l))
                                .accessibilityAddTraits(layer == l ? .isSelected : [])
                        }
                    }
                }
                .padding(16)
                theme.line.frame(height: 1)
                switch layer {
                case .text: text
                case .timeline: timeline(hit)
                }
            }
        } else {
            VStack {
                Spacer()
                Text(model.result?.hits.isEmpty == false ? "Pick a hit to read it." : "")
                    .foregroundStyle(theme.muted)
                Spacer()
            }
            .frame(maxWidth: .infinity)
        }
    }

    private var text: some View {
        ScrollView {
            Group {
                if let failure = model.shownFailure {
                    Text(failure).foregroundStyle(theme.attention)
                } else if let t = model.text {
                    VStack(alignment: .leading, spacing: 10) {
                        MarkdownView(text: t.text)
                            .textSelection(.enabled)
                        if t.cut {
                            Text("Cut at \(t.text.utf8.count) of \(t.bytes) bytes: open it for the rest.")
                                .font(theme.sized(Flat.label))
                                .foregroundStyle(theme.muted)
                        }
                    }
                } else {
                    ProgressView().controlSize(.small)
                }
            }
            .padding(16)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .scrollEdges()
    }

    @ViewBuilder
    private func timeline(_ hit: AppAPI.SearchHit) -> some View {
        if let tl = model.timeline, tl.at.id == hit.id {
            ScrollView {
                VStack(alignment: .leading, spacing: 2) {
                    PaneLabel("Its task's and thread's history around it, in time order")
                        .padding(.horizontal, 8)
                        .padding(.bottom, 6)
                    ForEach(Search.rows(tl)) { row in
                        Button {
                            if !row.at { open(row.hit, keep: .timeline) }
                        } label: {
                            SearchHitRow(row.hit, at: row.at)
                                .padding(.horizontal, 8)
                                .padding(.vertical, 6)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .background(row.at ? theme.selection : .clear, in: RoundedRectangle(cornerRadius: Radius.row))
                                .contentShape(RoundedRectangle(cornerRadius: Radius.row))
                        }
                        .buttonStyle(.plain)
                    }
                }
                .padding(8)
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .scrollEdges()
        } else if model.shownFailure != nil {
            Text("No timeline for this hit.")
                .foregroundStyle(theme.muted)
                .padding(16)
            Spacer()
        } else {
            ProgressView().controlSize(.small)
                .padding(16)
            Spacer()
        }
    }

    // MARK: acts

    private func open(_ h: AppAPI.SearchHit, keep: Layer = .text) {
        layer = keep
        Task { await model.show(h) }
    }

    private func opens(_ t: Search.Target) -> Bool {
        switch t {
        case .thread(let id): app.listsThread(id, in: model.project)
        case .task, .note: app.project(model.project) != nil
        }
    }

    private func help(_ t: Search.Target) -> String {
        switch t {
        case .task(let id): "Shows \(id) in the task view"
        case .thread(let id): opens(t) ? "Shows \(id) in the window" : "\(id) is resolved: it isn't in the sidebar any more"
        case .note(let path): "Shows \(path) in the project's Memory"
        }
    }

    private func go(_ t: Search.Target) {
        dismiss()
        app.openSearchTarget(t, project: model.project)
    }
}

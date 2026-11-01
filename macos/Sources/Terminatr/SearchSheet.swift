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
    @FocusState private var fieldFocused: Bool

    enum Layer: String, CaseIterable {
        case text = "Text"
        case timeline = "Timeline"
    }

    var body: some View {
        VStack(spacing: 0) {
            bar
            theme.line.frame(height: 1)
            HSplitView {
                hits
                    .frame(minWidth: 280, idealWidth: 340, maxWidth: 440)
                detail
                    .frame(minWidth: 360, maxWidth: .infinity)
            }
            theme.line.frame(height: 1)
            HStack {
                Text("Searches \(model.project)'s notes, tasks, threads, journal, designs, library and inbox")
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.muted)
                Spacer()
                Button("Close") { dismiss() }
                    .keyboardShortcut(.cancelAction)
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 10)
        }
        .frame(minWidth: 760, idealWidth: 920, minHeight: 480, idealHeight: 620)
        .onAppear { fieldFocused = true }
    }

    // MARK: the query

    private var bar: some View {
        HStack(spacing: 10) {
            Picker("Project", selection: Binding(get: { model.project }, set: { model.switchProject($0) })) {
                ForEach(app.activeProjects + app.inactiveProjects, id: \.slug) { Text($0.slug).tag($0.slug) }
            }
            .labelsHidden()
            .fixedSize()
            .help("The project whose history to search")
            HStack(spacing: 6) {
                Image(systemName: "magnifyingglass")
                    .foregroundStyle(theme.muted)
                TextField("Search \(model.project)'s history", text: $model.words)
                    .textFieldStyle(.plain)
                    .font(theme.body(.title3))
                    .focused($fieldFocused)
                    .onSubmit { Task { await model.search() } }
                if model.searching {
                    ProgressView().controlSize(.small)
                }
            }
            .padding(.horizontal, 10)
            .padding(.vertical, 7)
            .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 8))
            .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(theme.edge, lineWidth: 1))
            Picker("Kind", selection: $model.kind) {
                Text("All kinds").tag("")
                Divider()
                ForEach(Search.kinds) { Label($0.label, systemImage: $0.symbol).tag($0.id) }
            }
            .labelsHidden()
            .fixedSize()
            .help("Only hits of this kind")
            .onChange(of: model.kind) {
                if model.result != nil { Task { await model.search() } }
            }
            Button("Search") { Task { await model.search() } }
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
                    .font(theme.mono(.caption))
                    .foregroundStyle(theme.muted)
                    .padding(.horizontal, 16)
                    .padding(.top, 10)
                    .padding(.bottom, 4)
                List(selection: Binding(get: { model.shown?.id },
                                        set: { id in if let h = r.hits.first(where: { $0.id == id }) { open(h) } })) {
                    ForEach(r.hits) { SearchHitRow($0).tag($0.id) }
                }
                .listStyle(.sidebar)
                .scrollContentBackground(.hidden)
            } else {
                VStack(alignment: .leading, spacing: 8) {
                    Text(model.searching ? "Searching…" : "Words to find, then Return.")
                        .foregroundStyle(theme.muted)
                    Text("A first search builds the project's index: it can take a few seconds.")
                        .font(theme.body(.footnote))
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
                        Picker("Layer", selection: $layer) {
                            ForEach(Layer.allCases, id: \.self) { Text($0.rawValue).tag($0) }
                        }
                        .pickerStyle(.segmented)
                        .labelsHidden()
                        .fixedSize()
                    }
                }
                .padding(20)
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
                                .font(theme.body(.footnote))
                                .foregroundStyle(theme.muted)
                        }
                    }
                } else {
                    ProgressView().controlSize(.small)
                }
            }
            .padding(20)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .scrollEdges()
    }

    @ViewBuilder
    private func timeline(_ hit: AppAPI.SearchHit) -> some View {
        if let tl = model.timeline, tl.at.id == hit.id {
            List {
                Section {
                    ForEach(Search.rows(tl)) { row in
                        Button {
                            if !row.at { open(row.hit, keep: .timeline) }
                        } label: {
                            SearchHitRow(row.hit, at: row.at)
                        }
                        .buttonStyle(.plain)
                        .listRowBackground(row.at ? theme.cardTint : Color.clear)
                    }
                } header: {
                    Text("Its task's and thread's history around it, in time order")
                        .font(theme.body(.footnote))
                }
            }
            .scrollContentBackground(.hidden)
        } else if model.shownFailure != nil {
            Text("No timeline for this hit.")
                .foregroundStyle(theme.muted)
                .padding(20)
            Spacer()
        } else {
            ProgressView().controlSize(.small)
                .padding(20)
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

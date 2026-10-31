import SwiftUI
import TerminatrKit
import TerminatrUI

/// The project dashboard's Designs (T864), as the TUI's Designs tab
/// lists them (design.list, app API 31): each design a card with its
/// status, title, build's progress, design task, open questions and
/// answers, and its page; a card opens in place to its detail: the
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
    let slug: String
    /// The paired server whose project this is (T888): its designs read
    /// through peer.design-list, read-only, without their pages or open
    /// questions; nil for one of this server's.
    let peer: AppAPI.Peer?
    /// What reads them again when it changes.
    let key: [String]
    @State private var designs: [AppAPI.Design]?
    @State private var error: String?
    /// That server doesn't share them (pages-off, no-pages, …).
    @State private var notShared = false
    /// The designs opened to their detail, by name.
    @State private var open: Set<String> = []
    /// Whether the done and archived designs show too.
    @State private var all = false

    /// A project of this server's: read again when its board, its
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

    var body: some View {
        // A stack, not a Group: a Group with nothing in it has nothing to
        // run its task on, so the designs were never read (T876).
        VStack(alignment: .leading, spacing: 10) {
            let listing = Designs.listing(designs, error: error)
            let shown = designs.map { Designs.shown($0, all: all) } ?? []
            let heading = if case .designs = listing, !notShared { "Designs · \(shown.count)" } else { "Designs" }
            Text(heading.uppercased())
                .font(theme.mono(.caption2, .medium))
                .tracking(1)
                .foregroundStyle(theme.muted)
                .padding(.top, 6)
            if notShared, let peer {
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
                    ForEach(Designs.ordered(shown), id: \.name) { d in
                        DesignCard(project: slug, design: d, server: peer.map(PeerPages.server), open: Binding(
                            get: { open.contains(d.name) },
                            set: { if $0 { open.insert(d.name) } else { open.remove(d.name) } }))
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
        .task(id: key) { await read() }
    }

    private func read() async {
        guard let peer else {
            do {
                designs = try await app.designs(slug)
                error = nil
            } catch {
                self.error = Dashboard.words(error)
            }
            return
        }
        guard app.readsPeerDesigns else {
            notShared = true
            return
        }
        do {
            designs = try await app.peerDesigns(peer.alias, slug)
            error = nil
            notShared = false
        } catch {
            let state: PeerPages.State<Int> = PeerPages.state(error)
            if state == .notShared {
                notShared = true
            } else {
                self.error = Dashboard.words(error)
            }
        }
    }
}

/// One design: its card, opened in place to its detail.
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
        VStack(alignment: .leading, spacing: 8) {
            Button { open.toggle() } label: { summary.contentShape(Rectangle()) }
                .buttonStyle(.plain)
                .help(open ? "Folds \(design.title)" : "Shows \(design.title)'s answers, questions and build")
                .accessibilityValue(open ? "open" : "folded")
            if open { detail }
        }
        .padding(12)
        .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 12))
        .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(theme.edge, lineWidth: 1))
    }

    private var summary: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Image(systemName: "chevron.right")
                    .rotationEffect(.degrees(open ? 90 : 0))
                    .font(theme.body(.glyph, .bold))
                    .foregroundStyle(theme.muted)
                StatusTag(status: design.status)
                Text(design.title)
                    .font(theme.body(.body, .semibold))
                    .lineLimit(1)
                Spacer(minLength: 8)
                Text(Designs.built(design))
                    .font(theme.mono(.footnote))
                    .foregroundStyle(theme.muted)
            }
            if !design.build.isEmpty {
                ProgressView(value: Designs.progress(design))
                    .tint(Designs.Status(design.status) == .done ? theme.good : theme.action)
            }
            Text(Designs.facts(design).joined(separator: " · "))
                .font(theme.body(.subheadline))
                .foregroundStyle(design.questions.isEmpty ? theme.muted : theme.question)
        }
    }

    @ViewBuilder
    private var detail: some View {
        VStack(alignment: .leading, spacing: 10) {
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
        .padding(.leading, 20)
    }

    private func buildRow(_ t: AppAPI.DesignTask) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Button { app.showTask(project: project, task: t.id) } label: {
                HStack(alignment: .firstTextBaseline, spacing: 8) {
                    Text(t.id)
                        .font(theme.mono(.subheadline, .medium))
                        .foregroundStyle(t.archived ? theme.muted : theme.action)
                        .frame(minWidth: 44, alignment: .leading)
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
                Text(after).font(theme.mono(.footnote)).foregroundStyle(theme.muted)
            }
            if t.archived {
                Text("archived").font(theme.mono(.footnote)).foregroundStyle(theme.muted)
            }
        }
    }

    private func heading(_ s: String) -> some View {
        Text(s.uppercased())
            .font(theme.mono(.label, .medium))
            .tracking(1)
            .foregroundStyle(theme.muted)
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

/// A design's or a task's status as a small tag in its colour.
private struct StatusTag: View {
    @Environment(\.theme) private var theme
    let status: String

    var body: some View {
        Text(status)
            .font(theme.mono(.label, .medium))
            .foregroundStyle(colour)
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

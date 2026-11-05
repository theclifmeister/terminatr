import SwiftUI
import TerminatrKit
import TerminatrUI

/// A program's screen (the programs design §11.2, §12): its home, as a
/// project's dashboard is a project's, mirroring the TUI's program
/// dashboard. Its director first, then NEEDS YOU (program.needs, ranked
/// by the server), CHILDREN (its members in its order, a paired one's
/// from peer.list), UMBRELLAS and DEPENDENCIES (program.get). A need or
/// a child shows its project, where its cards answer it; the director's
/// card shows its pane. The director's recommendation on a question
/// shows under it with Accept (T982). The program's settings stay the
/// TUI's.
struct ProgramPage: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    let program: AppAPI.Program
    @State private var detail: AppAPI.ProgramDetail?
    @State private var needs: AppAPI.ProgramNeeds?
    @State private var failed: String?
    @State private var asking: ProgramAsk?
    /// The question whose recommendation is being accepted.
    @State private var accepting: String?
    /// Bumped after an act: the screen reads the program again.
    @State private var acted = 0

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                header
                directorCard
                section("Needs you", count: needs?.needs.count) { needsList }
                section("Children", count: program.members.count) { children }
                if let d = detail, !d.umbrellas.isEmpty {
                    section("Umbrellas", count: d.umbrellas.count) { umbrellas(d) }
                }
                if let d = detail, !d.waits.isEmpty {
                    section("Dependencies", count: d.waits.count) { dependencies(d) }
                }
                if let failed {
                    Text(failed)
                        .font(theme.body(.callout))
                        .foregroundStyle(theme.attention)
                }
            }
            .padding(.horizontal, 28)
            .padding(.top, 18)
            .padding(.bottom, 28)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .scrollEdges()
        // Read again as the program or what of it needs the user changes.
        .task(id: Reload(program: program, needs: app.needs(of: program).map(\.id), connected: app.bridge != nil, acted: acted)) { await read() }
        .sheet(item: $asking) { a in
            ProgramAskSheet(program: program.slug, ask: a) { acted += 1 }
        }
    }

    private struct Reload: Equatable {
        let program: AppAPI.Program
        let needs: [String]
        let connected: Bool
        let acted: Int
    }

    /// A need's click: a program question or umbrella asks here, a
    /// member's shows its project, whose cards answer it.
    private func act(_ n: AppAPI.ProgramNeed) {
        switch ProgramFacts.act(n) {
        case .answer(let ref, let q):
            asking = .answer(ref: ref, question: q, text: n.need.text)
        case .umbrella(let task):
            asking = .umbrella(task: task, title: n.need.title, text: n.need.text)
        case .open(let project, let session):
            app.open(project: project, session: session)
        }
    }

    private func read() async {
        do {
            (detail, needs) = try await app.programDetail(program.slug)
            failed = nil
        } catch {
            failed = "Not known now: \(error.localizedDescription)"
        }
    }

    // MARK: the parts

    private var header: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(alignment: .firstTextBaseline, spacing: 8) {
                Text("program")
                    .font(theme.mono(.subheadline, .medium))
                    .padding(.horizontal, 7)
                    .padding(.vertical, 1)
                    .background(theme.pane, in: Capsule())
                Text(program.slug)
                    .font(theme.title(.display, .heavy))
                Spacer()
                if app.controlsProjects {
                    Menu {
                        Button("Rename Program…") { asking = .rename }
                        Button("Delete Program…", role: .destructive) { asking = .delete }
                            .disabled(app.directorSession(program.slug) != nil)
                    } label: {
                        Label("Program", systemImage: "ellipsis.circle")
                    }
                    .fixedSize()
                    .help("Rename or delete \(program.slug); its members, authority and caps are set in tm's console")
                }
            }
            if !program.goal.isEmpty {
                Text(program.goal)
                    .foregroundStyle(theme.muted)
                    .lineLimit(2)
            }
            Text(ProgramFacts.line(program, working: detail?.working))
                .font(theme.mono(.footnote))
                .foregroundStyle(theme.muted)
            if !program.error.isEmpty {
                Text(program.error)
                    .font(theme.body(.callout))
                    .foregroundStyle(theme.attention)
            }
        }
    }

    /// Its director: its state, and a click shows its pane.
    private var directorCard: some View {
        let s = app.directorSession(program.slug)
        let a = Attention.of(director: s, in: program)
        return Button { app.select(.session(.director(program.slug))) } label: {
            HStack(spacing: 8) {
                Circle()
                    .fill(theme.colour(a.tone))
                    .frame(width: 8, height: 8)
                    .accessibilityHidden(true)
                RoleLabel(.director, points: 14)
                Text(a.why.isEmpty ? a.word : a.why)
                    .font(theme.body(.callout))
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
                Spacer(minLength: 8)
                if a.waits {
                    Text(a.word)
                        .font(theme.mono(.caption2, .medium))
                        .padding(.horizontal, 6)
                        .padding(.vertical, 1)
                        .background(theme.colour(a.tone), in: Capsule())
                        .foregroundStyle(theme.on(a.tone))
                }
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 10)
            .background(theme.pane, in: RoundedRectangle(cornerRadius: 8))
            .overlay(RoundedRectangle(cornerRadius: 8).strokeBorder(theme.line, lineWidth: 1))
            .contentShape(RoundedRectangle(cornerRadius: 8))
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Director")
        .accessibilityValue(a.why.isEmpty ? a.word : a.why)
        .accessibilityAddTraits(.isButton)
        .contextMenu { NewWindowButton(ref: .director(program.slug)) }
    }

    @ViewBuilder
    private var needsList: some View {
        if let needs {
            if needs.needs.isEmpty {
                note("Nothing waits on you in \(program.slug).")
            }
            ForEach(Array(needs.needs.enumerated()), id: \.offset) { _, n in
                Button { act(n) } label: {
                    line(lead: n.priority, place: n.program ? "program" : ProgramFacts.place(n.project, server: n.server),
                         text: ProgramFacts.need(n), tail: n.merged.isEmpty ? n.need.kind : "merged \(n.merged.count + 1)")
                }
                .buttonStyle(.plain)
                if let r = n.recommend {
                    recommendation(r)
                }
            }
            if needs.quiet > 0 {
                note(needs.quiet == 1 ? "1 child needs nothing" : "\(needs.quiet) children need nothing")
            }
        } else {
            ProgressView().controlSize(.small)
        }
    }

    /// The director's recommended answer under its need (T982), with
    /// Accept: the user's own answer, as program.recommend.accept gives
    /// it. Clicking the need picks another, as before.
    private func recommendation(_ r: AppAPI.Recommendation) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(ProgramFacts.recommends(r))
                .font(theme.body(.callout))
                .foregroundStyle(theme.muted)
                .lineLimit(2)
            Spacer(minLength: 8)
            if app.acceptsRecommendations {
                Button("Accept") { accept(r) }
                    .controlSize(.small)
                    .disabled(accepting == r.question)
                    .help("Answer \(r.question) with \(ProgramFacts.answer(r)), as yours")
            }
        }
        .padding(.leading, 64)
        .padding(.trailing, 12)
    }

    private func accept(_ r: AppAPI.Recommendation) {
        accepting = r.question
        Task {
            defer { accepting = nil }
            do {
                _ = try await app.programRecommendAccept(program.slug, question: r.question)
                failed = nil
            } catch {
                failed = "Not answered: \(error.localizedDescription)"
            }
            acted += 1
        }
    }

    @ViewBuilder
    private var children: some View {
        let shown = Dictionary(needs?.members.map { (ProgramFacts.place($0.project, server: $0.server), $0) } ?? [], uniquingKeysWith: { a, _ in a })
        // program.get's members carry their invite state and words; the
        // sidebar's, until it reads.
        ForEach(detail?.members ?? program.members, id: \.self) { m in
            let place = ProgramFacts.place(m.project, server: m.server)
            Button {
                app.select(.project(m.project, server: m.server))
            } label: {
                line(lead: "", place: m.server.isEmpty ? m.project : "\(m.server)/\(m.project)",
                     text: ProgramFacts.child(m, project: app.project(m.project), peer: app.peer(m.server), needs: shown[place]?.needs),
                     tail: "")
            }
            .buttonStyle(.plain)
            .disabled(m.server.isEmpty ? app.project(m.project) == nil
                      : app.peer(m.server)?.projects.contains { $0.project == m.project } != true)
        }
    }

    private func umbrellas(_ d: AppAPI.ProgramDetail) -> some View {
        ForEach(d.umbrellas, id: \.task.ref) { u in
            VStack(alignment: .leading, spacing: 4) {
                line(lead: u.task.priority, place: u.task.ref, text: u.task.title,
                     tail: "\(u.parts.filter { $0.state == "met" }.count)/\(u.parts.count)")
                ForEach(u.parts, id: \.ref) { part in
                    HStack(spacing: 8) {
                        Text(part.ref)
                            .font(theme.mono(.caption, .medium))
                            .foregroundStyle(theme.action)
                        Text(part.title)
                            .font(theme.body(.caption))
                            .lineLimit(1)
                        Spacer(minLength: 4)
                        Text(part.word.isEmpty ? part.state : part.word)
                            .font(theme.mono(.caption2))
                            .foregroundStyle(part.state == "met" ? theme.colour(.working) : theme.muted)
                    }
                    .padding(.leading, 70)
                }
            }
        }
    }

    private func dependencies(_ d: AppAPI.ProgramDetail) -> some View {
        ForEach(d.waits, id: \.self) { w in
            let dep = d.deps[w.after]
            line(lead: "", place: w.task, text: "\(w.title) ← \(w.after)",
                 tail: dep.map { $0.word.isEmpty ? $0.state : $0.word } ?? "")
        }
    }

    // MARK: drawing

    private func section<C: View>(_ title: String, count: Int?, @ViewBuilder content: () -> C) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(count.map { "\(title) \($0)" } ?? title)
                .font(theme.body(.caption))
                .foregroundStyle(theme.muted)
                .padding(.top, 6)
            content()
        }
    }

    /// A row of a part: its priority (or nothing), its place, its words
    /// and a quiet word at its end.
    private func line(lead: String, place: String, text: String, tail: String) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(lead)
                .font(theme.mono(.caption, .medium))
                .foregroundStyle(lead == "must" ? theme.attention : theme.muted)
                .frame(width: 44, alignment: .leading)
            Text(place)
                .font(theme.mono(.subheadline, .medium))
                .foregroundStyle(theme.action)
                .lineLimit(1)
            Text(text)
                .font(theme.body(.body))
                .lineLimit(1)
            Spacer(minLength: 8)
            Text(tail)
                .font(theme.mono(.caption))
                .foregroundStyle(theme.muted)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 6)
        .background(theme.pane, in: RoundedRectangle(cornerRadius: 6))
        .overlay(RoundedRectangle(cornerRadius: 6).strokeBorder(theme.line, lineWidth: 1))
        .contentShape(RoundedRectangle(cornerRadius: 6))
        .accessibilityElement(children: .combine)
    }

    private func note(_ s: String) -> some View {
        Text(s)
            .font(theme.body(.callout))
            .foregroundStyle(theme.muted)
    }
}

import SwiftUI
import TerminatrKit
import TerminatrUI

// Programs on the phone (B19, programs design §12): a program is one
// row on the Projects screen with one needs count, and its screen shows
// the rolled-up Needs you first, then its projects. Only while the
// server's hello says programs (PhoneModel.programs is empty without).

extension Attention {
    /// What a program's director says of itself: its session's state,
    /// and the program's open questions while none runs, as a
    /// coordinator's (Attention.of(coordinator:in:)).
    static func of(director s: AppAPI.Session?, questions: Int) -> Attention {
        let stand = AppAPI.Project(slug: "", active: true, questions: questions,
                                   usage: AppAPI.Usage(turns: 0, input: 0, output: 0, cacheRead: 0, cacheCreation: 0, costUsd: 0))
        let a = Attention.of(coordinator: s, in: stand)
        return s == nil && questions == 0 ? Attention(word: "not running", tone: .idle, why: "no director runs", waits: false) : a
    }
}

extension PhoneModel {
    /// Program g's director's session, while one runs.
    func directorSession(_ g: AppAPI.Program) -> AppAPI.Session? {
        directorSession(g.slug)
    }

    /// The threads of program g's members, local and paired, as their
    /// rows show them: the program row's meter.
    func programThreads(_ g: AppAPI.Program) -> [Attention.Tone] {
        g.members.flatMap { m -> [Attention.Tone] in
            if m.server.isEmpty {
                guard let p = project(m.project), p.active else { return [] }
                return SidebarTree.threads(of: p) { session($0) }.map { attention($0).tone }
            }
            guard let p = ProgramList.peerProject(m, in: peers), !p.stale else { return [] }
            return SidebarTree.sessions(of: p, alias: m.server).filter { !$0.ref.isCoordinator }.map { Attention.of(peer: $0.session).tone }
        }
    }
}

/// A program's card on the Projects screen: its monogram and name, its
/// director's state and how many projects it holds, its rolled-up needs
/// as one count, and its members' threads.
struct ProgramCard: View {
    @Environment(\.theme) private var theme
    @Environment(\.dynamicTypeSize) private var typeSize
    let program: AppAPI.Program
    let director: Attention
    let needs: Int
    let threads: [Attention.Tone]
    var selected = false
    var rule = false

    var body: some View {
        let stacked = typeSize.isAccessibilitySize
        VStack(alignment: .leading, spacing: Spacing.item) {
            HStack(alignment: stacked ? .top : .center, spacing: Spacing.row) {
                ProjectMonogram(program.slug, side: 32)
                    .overlay(alignment: .bottomTrailing) {
                        Image(systemName: "square.stack.3d.up.fill")
                            .font(.system(size: 11 * theme.scale, weight: .bold))
                            .foregroundStyle(theme.onPrompt)
                            .padding(2)
                            .background(theme.action, in: Circle())
                            .offset(x: 5, y: 5)
                            .accessibilityHidden(true)
                    }
                if !stacked { title }
                Spacer(minLength: 0)
                if needs > 0 { NeedsOrb(needs) }
            }
            if stacked { title }
            if !threads.isEmpty { ThreadMeter(threads) }
        }
        .projectRow(selected: selected, rule: rule)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(program.slug), program")
        .accessibilityValue(spoken)
    }

    private var title: some View {
        VStack(alignment: .leading, spacing: 1) {
            HStack(spacing: Spacing.tight) {
                Text(program.slug)
                    .font(theme.title(.title3, .bold))
                    .foregroundStyle(theme.ink)
                    .lines(1)
                Text("· program")
                    .font(theme.body(.subheadline, .medium))
                    .foregroundStyle(theme.muted)
                    .lineLimit(1)
                    .fixedSize()
                if program.paused {
                    Image(systemName: "pause.fill")
                        .font(theme.body(.footnote))
                        .foregroundStyle(theme.muted)
                }
            }
            HStack(spacing: Spacing.tight) {
                if director.tone == .working {
                    BusyMark(size: 6 * theme.scale)
                } else {
                    Image(systemName: director.tone.symbol)
                        .foregroundStyle(director.tone == .idle ? theme.muted : theme.colour(director.tone))
                }
                let line = "Director · " + (director.why.isEmpty ? director.word : director.why)
                Group {
                    if typeSize.isAccessibilitySize {
                        Text(line + " · " + Self.projects(program.members.count))
                            .lines(1)
                    } else {
                        HStack(spacing: 0) {
                            Text(line)
                                .lines(1)
                            Text(" · " + Self.projects(program.members.count))
                                .lineLimit(1)
                                .fixedSize()
                        }
                    }
                }
                .foregroundStyle(theme.muted)
            }
            .font(theme.body(.subheadline))
        }
    }

    static func projects(_ n: Int) -> String { n == 1 ? "1 project" : "\(n) projects" }

    private var spoken: String {
        ["director \(director.spoken)", Self.projects(program.members.count), needs > 0 ? "\(needs) need you" : nil,
         program.paused ? "paused" : nil]
            .compactMap { $0 }.joined(separator: "; ")
    }
}

/// A program's screen (B19): its name and goal, then its rolled-up
/// Needs you first, its director's and its members' (the same list
/// Needs you and the badge count, so each need shows once), each card
/// answerable at the phone's level as on Needs you; then its director (a tap opens its session, T973),
/// its projects (this server's, and a paired server's as that server's
/// mirror lists it, opening the same screen as under that server), and
/// its open umbrella tasks with their parts (program.get), which the
/// write level accepts or sends back while in review. The program's
/// own questions (program.needs) lead its Needs you as question cards,
/// answered here only once the bridge lets the phone answer them
/// (ProgramQuestionCard). Program settings and renaming stay the Mac's
/// and the TUI's.
struct ProgramScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    @Environment(\.navigate) private var navigate
    let slug: String
    @State private var detail: AppAPI.ProgramDetail?
    @State private var failure: String?
    /// The program's own questions, as program.needs sends them.
    @State private var questions: [AppAPI.ProgramNeed] = []
    /// Members' questions the director recommends an answer to (T982).
    @State private var recommended: [AppAPI.ProgramNeed] = []
    @State private var clearing = false

    var body: some View {
        ScrollView {
            if let g = model.program(slug) {
                let needs = model.programNeeds(g)
                LazyVStack(alignment: .leading, spacing: Spacing.row + 2) {
                    ProjectHeader(slug, goal: g.goal)
                    stats(g, needs: needs.count)
                    if !g.error.isEmpty { Failure(text: g.error) }
                    if let failure { Failure(text: failure) }
                    SectionHeading("Needs you", detail: needs.isEmpty ? "" : "\(needs.count)")
                    // Its questions as cards, in place of the need that
                    // counts them.
                    let shown = questions.isEmpty ? needs : needs.filter { !$0.id.hasPrefix(PhoneModel.programQuestionsPrefix) }
                    ForEach(questions, id: \.need.id) { q in
                        ProgramQuestionCard(program: slug, need: q) { await load() }
                    }
                    ForEach(recommended, id: \.need.id) { n in
                        RecommendationCard(program: slug, need: n) { await load() }
                    }
                    if needs.isEmpty {
                        quiet("Nothing in \(slug) needs you")
                    } else {
                        ForEach(shown) { n in
                            NeedTile(need: n, route: NeedsScreen.route(n, in: model), lead: false)
                        }
                    }
                    // Its director opens as a coordinator does (T973): its
                    // conversation, the composer at the write level, its
                    // questions; starting it stays the Mac's (Q209).
                    let director = model.directorSession(g)
                    let route = Route.session(.director(slug))
                    TileGroup {
                        PlainRouteLink(value: route) {
                            SessionTile("Director", state: .of(director: director, questions: g.questions), busy: Busy.of(director),
                                        context: model.context(.director(slug)), selected: navigate.opens(route, on: model))
                        }
                        .buttonStyle(.plain)
                        .contextMenu { SaveClearButton(ref: .director(slug), asking: $clearing) }
                    }
                    .asksSaveClear(.director(slug), isPresented: $clearing)
                    if let clear = model.clears[PhoneModel.clearKey(.director(slug))] { SaveClearStatus(clear: clear) }
                    if director == nil { DirectorStart() }
                    SectionHeading("Projects", detail: "\(g.members.count)")
                    if g.members.isEmpty { quiet("No projects yet") }
                    if !g.members.isEmpty {
                        ProjectPanel {
                            ForEach(Array(g.members.enumerated()), id: \.element) { i, m in
                                ProgramMemberCard(member: m, words: detail?.members.first { $0.project == m.project && $0.server == m.server }, rule: i > 0)
                            }
                        }
                    }
                    umbrellas
                }
                .padding(.horizontal, Spacing.gutter)
                .padding(.bottom, Spacing.gutter)
            } else {
                ContentUnavailableView("No program \(slug)", systemImage: "square.stack.3d.up.slash")
                    .padding(.top, 60)
            }
        }
        .task(id: model.generation) { await load() }
        .refreshable {
            await model.refresh()
            await load()
        }
        .keepsPlace(route: Route.program(slug))
        .navigationTitle(slug)
        .screenBar { EmptyView() }
        .backdrop()
    }

    private func load() async {
        guard let g = model.program(slug), model.can(AppAPI.Methods.programGet.name) else { return }
        do {
            detail = try await model.programDetail(slug)
            if model.can(AppAPI.Methods.programNeeds.name) {
                let all = try await model.programNeedsList(slug).needs
                questions = g.questions > 0 ? all.filter(ProgramQuestionCard.shows) : []
                recommended = all.filter { $0.recommend != nil && !ProgramQuestionCard.shows($0) }
            } else {
                questions = []
                recommended = []
            }
            failure = nil
        } catch {
            failure = error.localizedDescription
        }
    }

    /// Needs, projects and the threads working against its cap.
    private func stats(_ g: AppAPI.Program, needs: Int) -> some View {
        TileRow {
            StatTile(needs, needs == 1 ? "needs you" : "need you")
            StatTile(g.members.count, g.members.count == 1 ? "project" : "projects")
            StatTile(detail?.working, g.parallelThreads > 0 ? "of \(g.parallelThreads) working" : "working")
        }
        .accessibilityElement(children: .contain)
        .accessibilityHint(g.authority == "delegate" ? "The director starts work itself" : "The director proposes, you start")
    }

    @ViewBuilder
    private var umbrellas: some View {
        if let us = detail?.umbrellas, !us.isEmpty {
            SectionHeading("Umbrellas", detail: "\(us.count)")
            ForEach(us, id: \.task.ref) { u in
                UmbrellaTile(program: slug, umbrella: u) { await load() }
            }
        }
    }

    private func quiet(_ text: String) -> some View {
        Text(text)
            .font(theme.body(.subheadline))
            .foregroundStyle(theme.muted)
            .padding(Spacing.card)
            .frame(maxWidth: .infinity, alignment: .leading)
            .surface(.panel, in: RoundedRectangle(cornerRadius: Radius.tile, style: .continuous))
    }
}

/// How to start a program's director, while none runs: on the Mac or
/// in tm's console, never from the phone (Q209).
struct DirectorStart: View {
    @Environment(\.theme) private var theme

    static let words = "Start it on your Mac: Start the Director in its pane, or enter on its row in tm's console. Its conversation shows here once it runs."

    var body: some View {
        Label(Self.words, systemImage: "desktopcomputer")
            .font(theme.body(.footnote))
            .foregroundStyle(theme.muted)
            .fixedSize(horizontal: false, vertical: true)
            .padding(.horizontal, Spacing.card)
    }
}

/// A program member's card (B19): this server's project, or a paired
/// server's as its mirror (peer.list) lists it, named with its server
/// ("mini/acme-ios": a program mixes servers) and opening the same
/// screen as its row under that server; else, dim, what its invite
/// says (words: program.get's member, when read). On the program's
/// screen, and under its row on the Projects screen.
struct ProgramMemberCard: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.navigate) private var navigate
    let member: AppAPI.ProgramMember
    var words: AppAPI.ProgramMember?
    var rule = false

    private var m: AppAPI.ProgramMember { member }

    var body: some View {
        if m.server.isEmpty {
            if let p = model.project(m.project) {
                let route = Route.project(p.slug)
                PlainRouteLink(value: route) {
                    ProjectCard(name: p.slug, coordinator: model.coordinator(of: p),
                                needs: Need.count(model.needs, project: p.slug),
                                threads: p.active ? SidebarTree.threads(of: p) { model.session($0) }.map { model.attention($0).tone } : [],
                                pinned: p.pinned, dim: !p.active, selected: navigate.opens(route, on: model), rule: rule,
                                tag: p.active ? "" : "inactive")
                }
                .buttonStyle(.plain)
            } else {
                absent(ProgramList.key(m), words: words?.error.isEmpty == false ? words!.error : "not on this server")
            }
        } else if let p = ProgramList.peerProject(m, in: model.peers),
                  let peer = model.peers.first(where: { $0.alias == m.server }) {
            let sessions = SidebarTree.sessions(of: p, alias: peer.alias)
            let coordinator = sessions.first { $0.ref.isCoordinator }?.session ?? nil
            let route = Route.peerProject(alias: peer.alias, project: p.project)
            PlainRouteLink(value: route) {
                ProjectCard(name: ProgramList.key(m), coordinator: .of(peer: coordinator),
                            needs: p.stale ? 0 : Need.count(model.needs, project: ProgramList.key(m)),
                            threads: sessions.filter { !$0.ref.isCoordinator }.map { Attention.of(peer: $0.session).tone },
                            dim: peer.state != "up" || p.stale, selected: navigate.opens(route, on: model), rule: rule,
                            tag: words?.dormant == true ? "programs off there" : "")
            }
            .buttonStyle(.plain)
        } else {
            absent(ProgramList.key(m), words: words.map { !$0.words.isEmpty ? $0.words : $0.state } ?? "not shared with you")
        }
    }

    /// A member the phone can't open: its name and why, dim.
    private func absent(_ name: String, words: String) -> some View {
        ProjectCard(name: name, coordinator: Attention(word: words, tone: .idle, why: words, waits: false),
                    needs: 0, threads: [], dim: true, rule: rule)
            .accessibilityHint(words)
    }
}

/// An umbrella task of a program's board: its ref, title and status,
/// then each part with its state; in review, at the write level, Accept
/// and Send Back (program.umbrella.accept, .send-back).
private struct UmbrellaTile: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    let program: String
    let umbrella: AppAPI.ProgramUmbrella
    let acted: () async -> Void
    @State private var sendingBack = false
    @State private var note = ""
    @State private var working = false
    @State private var outcome: String?

    var body: some View {
        let t = umbrella.task
        VStack(alignment: .leading, spacing: Spacing.item) {
            HStack(alignment: .firstTextBaseline, spacing: Spacing.tight) {
                Text(t.ref)
                    .font(theme.mono(.subheadline, .semibold))
                    .foregroundStyle(theme.muted)
                Text(t.title)
                    .font(theme.body(.headline))
                    .foregroundStyle(theme.ink)
                    .lines(2)
                Spacer(minLength: 0)
                ToneBadge(t.status, tone: t.status == "review" ? .review : .idle, compact: true)
            }
            ForEach(umbrella.parts, id: \.ref) { part in
                HStack(spacing: Spacing.tight) {
                    Image(systemName: part.state == "met" || part.status == "done" || part.merged ? "checkmark.circle.fill" : "circle")
                        .foregroundStyle(theme.muted)
                        .accessibilityHidden(true)
                    Text(part.ref)
                        .font(theme.mono(.footnote))
                        .foregroundStyle(theme.muted)
                        .lineLimit(1)
                    Text(part.title)
                        .font(theme.body(.footnote))
                        .foregroundStyle(theme.ink)
                        .lines(1)
                    Spacer(minLength: 0)
                    Text(part.word.isEmpty ? part.status : part.word)
                        .font(theme.body(.footnote, .medium))
                        .foregroundStyle(theme.muted)
                        .lineLimit(1)
                }
                .accessibilityElement(children: .combine)
            }
            if t.status == "review", model.can(AppAPI.Methods.programUmbrellaAccept.name) {
                HStack(spacing: Spacing.item) {
                    Button("Accept") { act { try await model.acceptUmbrella(program: program, task: t.ref) } }
                        .buttonStyle(.ghost)
                    if model.can(AppAPI.Methods.programUmbrellaSendBack.name) {
                        Button("Send Back…") { note = ""; sendingBack = true }
                            .buttonStyle(.ghost)
                    }
                }
                .disabled(working)
            }
            if let outcome {
                Text(outcome)
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.muted)
            }
        }
        .padding(Spacing.card)
        .frame(maxWidth: .infinity, alignment: .leading)
        .surface(.card, in: RoundedRectangle(cornerRadius: Radius.tile, style: .continuous))
        .alert("Send \(t.ref) back", isPresented: $sendingBack) {
            TextField("What's missing", text: $note, axis: .vertical)
            Button("Send Back") {
                let text = note.trimmingCharacters(in: .whitespacesAndNewlines)
                act { try await model.sendBackUmbrella(program: program, task: t.ref, note: text) }
            }
            .disabled(note.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("The director hands your note on.")
        }
    }

    private func act(_ call: @escaping () async throws -> String) {
        working = true
        Task {
            do {
                let status = try await call()
                outcome = status.isEmpty ? nil : "Now \(status)"
                await acted()
            } catch {
                outcome = error.localizedDescription
            }
            working = false
        }
    }
}

/// One of a program's own questions (programs design §7.2): its words,
/// its options, and the member questions it covers. Its answer goes to
/// all of them (program.answer), the user's alone: the phone answers it
/// while its bridge lists program.answer, at the write level (Q208);
/// below it the card says to answer at the computer.
struct ProgramQuestionCard: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    let program: String
    let need: AppAPI.ProgramNeed
    let answered: () async -> Void
    @State private var picks: Set<Int> = []
    @State private var working = false
    @State private var outcome: String?

    /// Whether n is a program question this card shows, as the TUI's
    /// programQuestion: the program's, with its question and the answer
    /// act.
    static func shows(_ n: AppAPI.ProgramNeed) -> Bool {
        n.program && !(n.need.question?.questions.isEmpty ?? true) && n.acts.contains("answer")
    }

    /// Its ref for program.answer: "Q2" of "ask:Q2".
    var ref: String {
        need.need.id.hasPrefix("ask:") ? String(need.need.id.dropFirst(4)) : need.need.id
    }

    var body: some View {
        let item = need.need.question?.questions.first { !$0.answered } ?? need.need.question?.questions.first
        let answers = model.answersPrograms
        VStack(alignment: .leading, spacing: Spacing.item) {
            HStack(spacing: Spacing.tight) {
                Image(systemName: "questionmark.bubble.fill")
                    .foregroundStyle(theme.colour(Attention.Tone.question))
                    .accessibilityHidden(true)
                Text("Program question · \(ref)")
                    .font(theme.body(.subheadline, .semibold))
                    .foregroundStyle(theme.ink)
                Spacer(minLength: 0)
                if !need.priority.isEmpty {
                    ToneBadge(need.priority, tone: .idle, compact: true)
                }
            }
            if let item {
                Text(item.question)
                    .font(theme.body(.headline))
                    .foregroundStyle(theme.ink)
                    .fixedSize(horizontal: false, vertical: true)
                ForEach(Array(item.options.enumerated()), id: \.offset) { i, o in
                    option(i, o, multi: item.multiSelect, answers: answers)
                }
                if answers, item.multiSelect {
                    Button("Answer") { answer(item.options.indices.filter(picks.contains).map { item.options[$0].label }) }
                        .buttonStyle(.ghost)
                        .disabled(picks.isEmpty || working)
                }
            }
            if let r = need.recommend {
                RecommendationLine(program: program, recommendation: r, answered: answered)
            }
            if !need.merged.isEmpty {
                Text("Covers " + need.merged.joined(separator: ", "))
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.muted)
            }
            if !answers {
                Label("Answer it at your computer: this phone answers a program's questions only with write access.", systemImage: "desktopcomputer")
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.muted)
            }
            if let outcome {
                Text(outcome)
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.muted)
            }
        }
        .padding(Spacing.card)
        .frame(maxWidth: .infinity, alignment: .leading)
        .surface(.card, in: RoundedRectangle(cornerRadius: Radius.tile, style: .continuous))
    }

    @ViewBuilder
    private func option(_ i: Int, _ o: AppAPI.QuestionOption, multi: Bool, answers: Bool) -> some View {
        let words = VStack(alignment: .leading, spacing: 1) {
            Text(o.label)
                .font(theme.body(.subheadline, .medium))
                .foregroundStyle(theme.ink)
            if !o.description.isEmpty {
                Text(o.description)
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.muted)
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        if answers {
            Button {
                if multi {
                    if picks.contains(i) { picks.remove(i) } else { picks.insert(i) }
                } else {
                    answer([o.label])
                }
            } label: {
                HStack(spacing: Spacing.item) {
                    if multi {
                        Image(systemName: picks.contains(i) ? "checkmark.square.fill" : "square")
                            .foregroundStyle(theme.action)
                    }
                    words
                }
                .padding(Spacing.row)
                .surface(.glass, in: RoundedRectangle(cornerRadius: Radius.row, style: .continuous))
                .contentShape(RoundedRectangle(cornerRadius: Radius.row, style: .continuous))
            }
            .buttonStyle(.plain)
            .disabled(working)
        } else {
            HStack(spacing: Spacing.item) {
                Image(systemName: "circle")
                    .foregroundStyle(theme.muted)
                    .accessibilityHidden(true)
                words
            }
        }
    }

    private func answer(_ choices: [String]) {
        working = true
        Task {
            do {
                let r = try await model.answerProgram(program, ref: ref, choices: choices)
                outcome = r.fanned.isEmpty ? "Answered" : "Answered · sent to \(r.fanned.count) member questions"
                await answered()
            } catch {
                outcome = error.localizedDescription
            }
            working = false
        }
    }
}

/// The director's recommended answer to a question (T982): what it
/// recommends and why, and Accept, the user's own answer as
/// program.recommend.accept gives it, while the bridge lists that (the
/// write level). Picking another stays where the question is answered.
struct RecommendationLine: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    let program: String
    let recommendation: AppAPI.Recommendation
    let answered: () async -> Void
    @State private var working = false
    @State private var outcome: String?

    var body: some View {
        VStack(alignment: .leading, spacing: Spacing.tight) {
            Label(ProgramFacts.recommends(recommendation), systemImage: "lightbulb")
                .font(theme.body(.footnote))
                .foregroundStyle(theme.muted)
                .fixedSize(horizontal: false, vertical: true)
            if model.acceptsRecommendations {
                Button("Accept: \(ProgramFacts.answer(recommendation))") { accept() }
                    .buttonStyle(.ghost)
                    .disabled(working)
            }
            if let outcome {
                Text(outcome)
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.muted)
            }
        }
    }

    private func accept() {
        working = true
        Task {
            do {
                let r = try await model.acceptRecommendation(program, question: recommendation.question)
                outcome = r.fanned.isEmpty ? "Answered \(r.question)" : "Answered · sent to \(r.fanned.count) member questions"
                await answered()
            } catch {
                outcome = error.localizedDescription
            }
            working = false
        }
    }
}

/// A member coordinator's question the director recommends an answer to
/// (T982): its member, its words, and the recommendation with Accept.
/// Picking another answer is done where its tile leads, as before.
struct RecommendationCard: View {
    @Environment(\.theme) private var theme
    let program: String
    let need: AppAPI.ProgramNeed
    let answered: () async -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: Spacing.item) {
            HStack(spacing: Spacing.tight) {
                Image(systemName: "questionmark.bubble")
                    .foregroundStyle(theme.colour(Attention.Tone.question))
                    .accessibilityHidden(true)
                Text(ProgramFacts.place(need.project, server: need.server) + " · " + (need.recommend?.question.split(separator: "/").last.map(String.init) ?? ""))
                    .font(theme.body(.subheadline, .semibold))
                    .foregroundStyle(theme.ink)
                Spacer(minLength: 0)
            }
            if let q = need.need.question?.questions.first {
                Text(q.question)
                    .font(theme.body(.headline))
                    .foregroundStyle(theme.ink)
                    .fixedSize(horizontal: false, vertical: true)
            }
            if let r = need.recommend {
                RecommendationLine(program: program, recommendation: r, answered: answered)
            }
        }
        .padding(Spacing.card)
        .frame(maxWidth: .infinity, alignment: .leading)
        .surface(.card, in: RoundedRectangle(cornerRadius: Radius.tile, style: .continuous))
    }
}

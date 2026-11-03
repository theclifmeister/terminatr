import SwiftUI
import TerminatrKit
import TerminatrUI

/// A session's conversation from its transcript (transcript.watch: its
/// last items when the server pages, transcript.page for the earlier
/// ones on a tap), as the Mac's conversation tab draws it: prompts, the agent's text,
/// tool calls folded into runs (RunChip, a tap unfolds one), and what
/// it waits on at the end as its hero (HeroCard, T848): the open
/// question with its options as numbered rows, or the approval with
/// its command, answered there at the answer level (NeedActs), else to
/// read. At the write level the composer dock floats under it
/// (PhoneComposer). Opened from a need (Route.need, T826) it opens at
/// its end, the hero in view, and marks where the need was asked
/// (Transcript.moment).
struct ConversationScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let ref: SessionRef
    /// The question or approval its card shows above the conversation.
    var need: Need?
    @State private var transcript = Transcript()
    /// The session transcript is of: a redial's watch of the same one
    /// keeps it on the screen (rejoin), another starts afresh.
    @State private var watched: String?
    @State private var failure: String?
    @State private var unfolded: Set<Int> = []
    @State private var paging = false
    @State private var pages = false
    /// Whether the conversation is at its end, or the user scrolled up
    /// (iOS 18, from the scroll geometry; 17 always follows): a signal
    /// only, the scroll is SwiftUI's (ConversationScroll, T976).
    @State private var follow = Follower()
    /// How many of the last entries the conversation draws: laid out
    /// exactly, not lazily (T976), so a long one draws its last turns,
    /// and Earlier messages more.
    @State private var drawn = Self.page
    /// The first entry drawn, kept as output comes (T976): a window that
    /// slid would drop a row from its head as each one came at its foot,
    /// and a taller row gone than come shrinks the content under an
    /// offset that stays (the anchor for size changes is the top): the
    /// view past its end for a frame. Kept, the content only grows.
    @State private var head: Int?
    /// The row the user had at the top, scrolled up, before the layout
    /// changed (an iPhone Duo opened or closed): scrolled back to once
    /// the transcript has it (Places, T825).
    @State private var restoring: Int?
    /// Opened from a need: scrolls to its moment once the transcript
    /// has it.
    @State private var seeking = false

    /// The items a stream opens with when the server pages: the screen's
    /// worth and some.
    static let tailItems = 60

    /// The entries drawn at first, and added by each Earlier messages.
    static let page = 60

    /// What changes as output comes: the entries' count, and the last
    /// one's text and result as it streams.
    static func tail(_ t: Transcript) -> [Int] {
        [t.entries.count, t.entries.last?.id ?? -1, t.entries.last?.text.count ?? 0, t.entries.last?.result == nil ? 0 : 1]
    }

    var body: some View {
        let id = model.sessionID(ref)
        let session = id.flatMap { id in model.sessions.first { $0.id == id } }
        let entries = transcript.tail(drawn, keeping: head)
        let earlier = transcript.entries.count - entries.count
        let rows = Transcript.rows(entries)
        let moment = moment(rows, session)
        let waits = need ?? model.needs.first { $0.session == id && ($0.kind == .question || $0.kind == .approval) }
        ScrollViewReader { scroll in
            ScrollView {
                VStack(alignment: .leading, spacing: 12) {
                    if id == nil {
                        if ref.director {
                            ContentUnavailableView("The director isn't running", systemImage: "moon.zzz",
                                                   description: Text(DirectorStart.words))
                                .padding(.top, 60)
                        } else if ref.isCoordinator, !ref.isRemote, model.sleep(of: ref.project) != .awake {
                            SleepingCoordinator(project: ref.project)
                                .padding(.top, 60)
                        } else {
                            ContentUnavailableView("No session runs", systemImage: "moon.zzz",
                                                   description: Text("Its conversation shows while a session runs it."))
                                .padding(.top, 60)
                        }
                    }
                    if let failure { Failure(text: failure) }
                    if earlier > 0 || (pages && !transcript.before.isEmpty), let id { earlierButton(id, drawnOnly: earlier > 0) }
                    ForEach(rows) { row in
                        Group {
                            switch row {
                            // The question it waits on is the hero, at the end.
                            case .entry(let e) where e.asksUser && e.result == nil && session?.question != nil: EmptyView()
                            case .entry(let e): EntryLine(entry: e, agent: session?.agent ?? "", session: id)
                            case .tools(let run): runView(run)
                            }
                        }
                        .background(alignment: .leading) {
                            if row.id == moment, let need { MomentMark(colour: theme.colour(need.kind)) }
                        }
                        .id(row.id)
                        // What the scroll tests look for (T976).
                        .accessibilityIdentifier(row.id == rows.last?.id ? "conversation.last" : "")
                    }
                    // Never a blank conversation (T930): until the stream
                    // answers, while the agent names its transcript, or
                    // when tm can't read it, it says so, as the Mac's does.
                    if let id, failure == nil { state(id, session) }
                    if let session { waiting(session, need: waits) }
                    // Queued prompts and the working line (T893).
                    if let session { ConversationTail(ref: ref, session: session, transcript: transcript) }
                    // Scrolled to, the end row reaches the true end
                    // (T807): the last card then sits the cards' gap
                    // over the composer, and no more (T882).
                    Color.clear.frame(height: 0).id("end")
                }
                .padding([.horizontal, .top], 16)
                .scrollTargetLayout()
            }
            .accessibilityIdentifier("conversation")
            // A drag down the conversation folds the keyboard (T857).
            .scrollDismissesKeyboard(.interactively)
            .modifier(ConversationScroll(follow: follow, newest: transcript.entries.last?.id, screen: "iphone conversation",
                                         detail: { ["role": session?.role ?? "", "rows": "\(rows.count)", "entries": "\(transcript.entries.count)"] },
                                         top: { top in
                                             if restoring == nil { model.places.keep("top \(ref)", follow.following ? nil : top) }
                                         }, toEnd: { toEnd(scroll) }))
            // New output: the follower goes to the end (SwiftUI's scroll,
            // with the bottom anchor under it: the first rows coming into
            // an empty view don't move an anchored offset); scrolled up,
            // the screen stays and Jump to latest counts it. Laid out afresh, it goes back to the row
            // the user had at the top.
            .onChange(of: Self.tail(transcript)) {
                head = transcript.tail(drawn, keeping: head).first?.id
                if seeking, restoring == nil, self.moment(Transcript.rows(transcript.tail(drawn, keeping: head)), session) != nil {
                    // The hero is after the moment: the end shows both.
                    seeking = false
                    toEnd(scroll)
                    return
                }
                if let r = restoring {
                    let rows = Transcript.rows(transcript.tail(drawn, keeping: head)).map(\.id)
                    guard let first = rows.first, rows.last ?? Int.min >= r else { return }
                    restoring = nil
                    if r >= first {
                        scroll.scrollTo(rows.last { $0 <= r } ?? first, anchor: .top)
                        return
                    }
                }
                let follows = follow.arrived(transcript.entries.map(\.id))
                ScrollGuard.trace("arrived \(transcript.entries.count) toEnd=\(follows) \(follow.trace)")
                if follows { toEnd(scroll) }
            }
            // The agent's turn starting or ending, a queued prompt sent:
            // the follower stays at the end.
            .onChange(of: Busy.of(session) != nil) {
                if follow.following { toEnd(scroll) }
            }
            .onChange(of: model.queuedSent[ref]?.count ?? 0) {
                if follow.following { toEnd(scroll) }
            }
            .onAppear {
                if let top: Int = model.places.value("top \(ref)") { restoring = top }
                seeking = need != nil
            }
            .overlay(alignment: .bottom) {
                if !follow.following {
                    Button {
                        jump(scroll)
                    } label: {
                        Label(follow.label, systemImage: "arrow.down")
                            .font(theme.body(.subheadline, .semibold))
                            .padding(.horizontal, 14)
                            .padding(.vertical, 8)
                            .frosted(Capsule())
                            .overlay(Capsule().strokeBorder(theme.edge, lineWidth: 1))
                            .shadow(color: .black.opacity(0.18), radius: 8, y: 3)
                    }
                    .buttonStyle(.plain)
                    .padding(.bottom, 10)
                }
            }
        }
        .safeAreaInset(edge: .bottom, spacing: 0) {
            // The cards' gap between the conversation and the composer;
            // the agent's turn shows at the conversation's end (T893).
            if model.writes, let id {
                PhoneComposer(ref: ref, session: id, running: session?.state == "working", held: session?.queueHeld ?? "")
                    .padding(.top, 12)
            }
        }
        .refreshable { await model.refresh() }
        .backdrop()
        // A redial is a new bridge: the stream opens again on it.
        .task(id: "\(id ?? "")/\(model.generation)") { await watch(id) }
        .kept($unfolded, "runs \(ref)")
    }

    /// Jump to latest: follow again, and SwiftUI's
    /// scroll to the end row, exact as the rows are laid out exactly
    /// (T976). Follow holds it through a flick's momentum (T829).
    private func jump(_ scroll: ScrollViewProxy) {
        follow.jump(newest: transcript.entries.last?.id)
        withAnimation(reduceMotion ? nil : .default) { toEnd(scroll) }
    }

    /// SwiftUI's scroll to the end row: the only scroll here besides
    /// the bottom anchor and the user's (T976).
    private func toEnd(_ scroll: ScrollViewProxy) {
        scroll.scrollTo("end", anchor: .bottom)
    }

    /// What the conversation is short of, under its rows: a spinner
    /// until the stream's first answer (a fresh launch over the relay),
    /// the transcript not named yet, one tm can't read, nothing in it
    /// yet, or the session ended (ConversationState, the Mac's words).
    @ViewBuilder
    private func state(_ id: String, _ session: AppAPI.Session?) -> some View {
        let state = ConversationState.of(caps: model.hello?.server?.caps ?? [], agent: session?.agent ?? "",
                                         status: watched == id ? transcript.status : nil, failure: nil)
        if state == .loading {
            ProgressView()
                .frame(maxWidth: .infinity)
                .padding(.top, transcript.entries.isEmpty ? 40 : 0)
                .accessibilityLabel("Loading the conversation")
        } else if let words = Self.words(state, closed: transcript.closed, empty: transcript.entries.isEmpty) {
            Text(words)
                .font(theme.body(.subheadline))
                .foregroundStyle(theme.muted)
                .multilineTextAlignment(.center)
                .onGlass()
                .frame(maxWidth: .infinity)
                .padding(.top, transcript.entries.isEmpty ? 40 : 0)
        }
    }

    /// The line a conversation's state says, if any.
    private static func words(_ state: ConversationState, closed: String?, empty: Bool) -> String? {
        if let closed { return "The session ended (\(closed))." }
        switch state {
        case .loading, .mapped: return empty ? "No messages yet." : nil
        case .waiting: return "Waiting for the agent to name its transcript…"
        case .unavailable(let why): return why
        }
    }

    /// The row the need was asked at.
    private func moment(_ rows: [Transcript.Row], _ session: AppAPI.Session?) -> Int? {
        guard let need, let e = transcript.moment(need.kind, since: session?.question?.since) else { return nil }
        return Transcript.row(of: e, in: rows)
    }

    /// Earlier messages: the ones the transcript has and the view
    /// doesn't draw yet first, then the server's earlier page.
    private func earlierButton(_ id: String, drawnOnly: Bool) -> some View {
        Button {
            if drawnOnly {
                drawn += Self.page
            } else {
                Task { await page(id) }
            }
        } label: {
            HStack {
                Text("Earlier messages")
                if paging { ProgressView().controlSize(.small) }
            }
            .font(theme.body(.footnote, .semibold))
            .frame(maxWidth: .infinity)
        }
        .buttonStyle(.bordered)
        .disabled(paging)
    }

    private func page(_ id: String) async {
        guard let b = model.bridge else { return }
        paging = true
        defer { paging = false }
        do {
            let had = transcript.entries.count
            transcript.prepend(try await b.call(AppAPI.Methods.transcriptPage, AppAPI.TranscriptPageParams(session: id, before: transcript.before)))
            drawn = max(drawn, had) + transcript.entries.count - had
        } catch {
            failure = error.localizedDescription
        }
    }

    @ViewBuilder
    private func runView(_ run: Transcript.ToolRun) -> some View {
        let open = unfolded.contains(run.id)
        VStack(alignment: .leading, spacing: 6) {
            Button {
                withAnimation(reduceMotion ? nil : .snappy(duration: 0.2)) {
                    if open { unfolded.remove(run.id) } else { unfolded.insert(run.id) }
                }
            } label: {
                RunChip(run: run, open: open)
            }
            .buttonStyle(.plain)
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(run.spoken + ", " + ToolRunHeader.stateWord(run))
            .accessibilityValue(open ? "expanded" : "collapsed")
            .accessibilityAddTraits(.isButton)
            if open {
                ForEach(run.calls) { ToolLine(entry: $0) }
                    .padding(.leading, 14)
            } else if let live = run.live {
                ToolLine(entry: live).padding(.leading, 14)
            }
        }
    }

    /// What the session waits on, as the conversation's hero: each open
    /// question (QuestionPlan.shown), the need's with its options to
    /// answer, the rest to read; or its dialog, with the need's buttons.
    @ViewBuilder
    private func waiting(_ s: AppAPI.Session, need: Need?) -> some View {
        if let q = s.question {
            ForEach(QuestionPlan.shown(q), id: \.self) { i in
                let item = q.questions[i]
                let waits = "question \(s.id) \(q.since?.timeIntervalSince1970 ?? 0) \(i)"
                let word = "Asks · " + QuestionPlan.tag(q, index: i)
                let line = Self.line("AskUserQuestion", since: q.since)
                // Each card answers its own question (Need.answer), not
                // only a menu's one open question: two questions left the
                // first card's options untappable (T887).
                if let need, need.kind == .question, let a = Need.answer(s.id, q, index: i, picks: Compatibility.answersPicks(model.hello)),
                   model.can(AppAPI.Methods.sessionAnswer.name) {
                    NeedActs(need: need, answer: a) { parts in
                        HeroCard(tone: .question, symbol: Need.Kind.question.symbol, word: word, line: line, title: item.question,
                                 note: parts.note, waiting: waits) {
                            parts.buttons
                        }
                    }
                } else {
                    // A multi-select of the agent's menu, before a tm that
                    // types it (app API 39), is picked in its terminal: the
                    // card says so, its options only read.
                    let several = QuestionPlan(q, index: i, picks: Compatibility.answersPicks(model.hello)).paneOnly
                    HeroCard(tone: .question, symbol: Need.Kind.question.symbol, word: word, line: line, title: item.question,
                             note: several ? ("It takes several options: pick them in the terminal view.", false) : nil,
                             waiting: waits) {
                        VStack(spacing: Spacing.item) {
                            ForEach(Array(item.options.enumerated()), id: \.offset) { n, o in
                                AnswerOption(key: n + 1, option: o, action: nil)
                            }
                        }
                    }
                }
            }
        }
        if s.state == "blocked", s.reason == "permission" {
            let tool = transcript.pendingTool
            let word = s.escape ? "Approval · outside the sandbox" : "Approval"
            let line = Self.line(tool?.tool ?? "", since: need?.since)
            let command = tool.map { "\($0.tool): \($0.text)" } ?? ""
            let waits = "approval \(s.id) \(tool?.call ?? "")"
            if let need, need.kind == .approval, model.can(AppAPI.Methods.sessionApprove.name) {
                NeedActs(need: need) { parts in
                    HeroCard(tone: .approval, symbol: Need.Kind.approval.symbol, word: word, line: line,
                             title: parts.command.isEmpty ? "The agent asks to go on." : parts.command,
                             command: command, note: parts.note, waiting: waits) {
                        parts.buttons
                    }
                }
            } else {
                HeroCard(tone: .approval, symbol: Need.Kind.approval.symbol, word: word, line: line,
                         title: "The agent asks to go on.", command: command,
                         note: ("Approve it on your Mac.", false), waiting: waits)
            }
        }
    }

    /// A hero's line: the tool, and how long it has waited.
    private static func line(_ tool: String, since: Date?) -> String {
        let age = Need.age(since: since)
        return [tool, age.isEmpty ? "" : age == "now" ? "waiting" : "waiting \(age)"].filter { !$0.isEmpty }.joined(separator: " · ")
    }

    private func watch(_ id: String?) async {
        // A redial reads the same session again: what is shown stays
        // until the fresh read is in, then takes it in place (T859).
        let again = id != nil && id == watched && !transcript.entries.isEmpty
        if !again { transcript = Transcript() }
        watched = id
        failure = nil
        guard let id, let b = model.bridge else { return }
        let hello = await b.hello
        pages = hello?.methods.contains(AppAPI.Methods.transcriptPage.name) == true
        let tail = hello?.server?.caps.contains("transcript-tail") == true ? Self.tailItems : 0
        do {
            let sub = try await b.subscribe(AppAPI.Methods.transcriptWatch, AppAPI.TranscriptParams(session: id, tail: tail))
            var fresh: Transcript? = again ? Transcript() : nil
            if fresh == nil { transcript.start(sub.result.status) } else { fresh?.start(sub.result.status) }
            for try await e in sub.events {
                guard var f = fresh else {
                    transcript.apply(e.name, e.data)
                    continue
                }
                f.apply(e.name, e.data)
                fresh = f
                if e.name == "transcript.reset" || e.name == "transcript.items" {
                    transcript.rejoin(f)
                    fresh = nil
                }
            }
        } catch {
            // A drop says nothing here: the redial reads it again.
            if !Task.isCancelled, !PhoneModel.isDrop(error) { failure = error.localizedDescription }
        }
    }
}

/// The mark beside the row a need was asked at.
struct MomentMark: View {
    let colour: Color

    var body: some View {
        Capsule()
            .fill(colour)
            .frame(width: 3)
            .offset(x: -9)
            .accessibilityHidden(true)
    }
}

/// A need's route: its session's conversation, with the question or
/// approval it waits on above while it lasts; answered, the
/// conversation as any other; its terminal a switch away (SessionScreen).
struct NeedConversation: View {
    @Environment(PhoneModel.self) private var model
    let ref: SessionRef

    var body: some View {
        let id = model.sessionID(ref)
        SessionScreen(ref: ref, need: model.needs.first { $0.session == id && ($0.kind == .question || $0.kind == .approval) })
    }
}

/// One entry, as the Mac's EntryView draws it, without its folds: a
/// prompt as a bubble on the right, its files under it, the agent's
/// text as a card, a call that sends files as their card (T860).
struct EntryLine: View {
    @Environment(\.theme) private var theme
    let entry: Transcript.Entry
    let agent: String
    /// The session its files are read from (session.file).
    var session: String?
    @State private var unfolded = false

    var body: some View {
        switch entry.kind {
        case .prompt where entry.folds && !unfolded:
            FoldedPrompt(entry: entry, when: entry.at?.formatted(date: .omitted, time: .shortened) ?? "") { unfolded = true }
        case .prompt:
            HStack {
                Spacer(minLength: 40)
                VStack(alignment: .trailing, spacing: 4) {
                    turn
                        .onGlass()
                        .onTapGesture { if entry.folds { unfolded = false } }
                    Text(entry.preview() ?? entry.text)
                        .foregroundStyle(theme.onPrompt)
                        .padding(.horizontal, 13)
                        .padding(.vertical, 9)
                        .background(LinearGradient(colors: theme.prompt, startPoint: .topLeading, endPoint: .bottomTrailing),
                                    in: RoundedRectangle(cornerRadius: 14))
                    if !entry.files.isEmpty {
                        PhoneAttachments(files: entry.attachments, session: session, alignment: .trailing)
                    }
                }
            }
        case .text:
            VStack(alignment: .leading, spacing: 6) {
                turn
                MarkdownView(text: entry.text)
            }
            .padding(14)
            .frame(maxWidth: .infinity, alignment: .leading)
            .frosted(RoundedRectangle(cornerRadius: 14))
            .background(theme.agentCard, in: RoundedRectangle(cornerRadius: 14))
            .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(theme.edge, lineWidth: 1))
        case .files:
            PhoneAttachmentCard(entry: entry, session: session,
                                turn: [entry.who(agent: agent), entry.at?.formatted(date: .omitted, time: .shortened) ?? ""]
                                    .filter { !$0.isEmpty }.joined(separator: " · "))
        case .tool:
            ToolLine(entry: entry)
        case .result:
            if let outcome = entry.questionOutcome {
                AnsweredQuestionCard(outcome: outcome)
            } else if let r = entry.result {
                HStack(spacing: Spacing.tight) {
                    if r.error {
                        Image(systemName: "exclamationmark.triangle.fill")
                            .foregroundStyle(theme.attention)
                            .accessibilityHidden(true)
                    }
                    Text("\(r.text)\(r.lines > 1 ? " · \(r.lines) lines" : "")")
                        .foregroundStyle(r.error ? theme.ink : theme.muted)
                }
                    .font(theme.mono(.footnote))
                    .lineLimit(1)
                    .onGlass()
                    .padding(.leading, 12)
            }
        case .note:
            // A centred divider: "Conversation compacted", "Interrupted".
            HStack(spacing: 10) {
                Rectangle().fill(theme.edge).frame(height: 1)
                Text(entry.text)
                    .font(theme.body(.footnote, .medium))
                    .foregroundStyle(theme.muted)
                    .fixedSize()
                Rectangle().fill(theme.edge).frame(height: 1)
            }
            .padding(.vertical, 6)
            .accessibilityElement(children: .combine)
        }
    }

    private var turn: some View {
        Text([entry.who(agent: agent), entry.at?.formatted(date: .omitted, time: .shortened) ?? ""].filter { !$0.isEmpty }.joined(separator: " · "))
            .font(theme.mono(.caption, .medium))
            .foregroundStyle(theme.muted)
    }
}

extension View {
    /// A line over the mesh on a glass capsule: the mesh is no place for
    /// small or muted text (ThemeColours.checks, T851).
    func onGlass() -> some View {
        padding(.horizontal, Spacing.item)
            .padding(.vertical, 2)
            .surface(.glass, in: Capsule())
    }
}

/// A sleeping coordinator's screen while it doesn't run (T980): it
/// slept after a long idle, its conversation kept; opening it wakes it
/// (SessionScreen), and while it wakes it spins. Below the write level
/// the phone can't wake it: the Mac or the console does.
private struct SleepingCoordinator: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    let project: String

    var body: some View {
        if model.sleep(of: project) == .waking {
            VStack(spacing: Spacing.row) {
                ProgressView()
                Text("Waking the coordinator")
                    .font(theme.title(.headline, .bold))
                Text("Its conversation is kept: it shows once the coordinator runs.")
                    .font(theme.body(.subheadline))
                    .foregroundStyle(theme.muted)
                    .multilineTextAlignment(.center)
            }
            .frame(maxWidth: .infinity)
            .accessibilityElement(children: .combine)
        } else {
            ContentUnavailableView {
                Label("The coordinator is asleep", systemImage: "moon.zzz.fill")
            } description: {
                Text(model.can(AppAPI.Methods.projectOpen.name)
                     ? "It slept after a long idle, its conversation kept: opening it wakes it."
                     : "It slept after a long idle, its conversation kept: opening it on the Mac or in tm's console wakes it.")
            } actions: {
                if model.can(AppAPI.Methods.projectOpen.name), model.project(project)?.active == true {
                    StartCoordinatorButton(project: project)
                }
            }
        }
    }
}

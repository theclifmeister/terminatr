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
    /// Whether the conversation follows its end, or the user scrolled
    /// up (iOS 18, from the scroll geometry; 17 always follows).
    @State private var follow = Follow()
    /// Jump to latest's glide to the end (T889).
    @State private var glider = Glider()
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

    /// What changes as output comes: the entries' count, and the last
    /// one's text and result as it streams.
    static func tail(_ t: Transcript) -> [Int] {
        [t.entries.count, t.entries.last?.id ?? -1, t.entries.last?.text.count ?? 0, t.entries.last?.result == nil ? 0 : 1]
    }

    var body: some View {
        let id = model.sessionID(ref)
        let session = id.flatMap { id in model.sessions.first { $0.id == id } }
        let rows = Transcript.rows(transcript.entries)
        let moment = moment(rows, session)
        let waits = need ?? model.needs.first { $0.session == id && ($0.kind == .question || $0.kind == .approval) }
        ScrollViewReader { scroll in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 12) {
                    if id == nil {
                        ContentUnavailableView("No session runs", systemImage: "moon.zzz",
                                               description: Text("Its conversation shows while a session runs it."))
                            .padding(.top, 60)
                    }
                    if let failure { Failure(text: failure) }
                    if pages, !transcript.before.isEmpty, let id { earlier(id) }
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
                    }
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
                .background { ScrollViewHook(glider: glider) }
            }
            // A drag down the conversation folds the keyboard (T857).
            .scrollDismissesKeyboard(.interactively)
            .modifier(PhoneFollowGeometry(follow: $follow, newest: transcript.entries.last?.id) { top in
                if restoring == nil { model.places.keep("top \(ref)", follow.following ? nil : top) }
            } toEnd: {
                scroll.scrollTo("end", anchor: .bottom)
            })
            // New output: the follower goes to the end; scrolled up, the
            // screen stays and Jump to latest counts it. Laid out afresh,
            // it goes back to the row the user had at the top.
            .onChange(of: Self.tail(transcript)) {
                if seeking, restoring == nil, self.moment(Transcript.rows(transcript.entries), session) != nil {
                    // The hero is after the moment: the end shows both.
                    seeking = false
                    scroll.scrollTo("end", anchor: .bottom)
                    return
                }
                if let r = restoring {
                    let rows = Transcript.rows(transcript.entries).map(\.id)
                    guard let first = rows.first, rows.last ?? Int.min >= r else { return }
                    restoring = nil
                    if r >= first {
                        scroll.scrollTo(rows.last { $0 <= r } ?? first, anchor: .top)
                        return
                    }
                }
                if follow.arrived(transcript.entries.map(\.id)) { scroll.scrollTo("end", anchor: .bottom) }
            }
            .onAppear {
                if let top: Int = model.places.value("top \(ref)") { restoring = top }
                seeking = need != nil
            }
            .onChange(of: Busy.of(session) != nil) {
                if follow.following, !follow.gliding { scroll.scrollTo("end", anchor: .bottom) }
            }
            .onChange(of: model.queuedSent[ref]?.count ?? 0) {
                if follow.following, !follow.gliding { scroll.scrollTo("end", anchor: .bottom) }
            }
            .onChange(of: follow.gliding) { _, g in if !g { glider.stop() } }
            .onDisappear { glider.stop() }
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

    /// Jump to latest: a glide to the end that lands on it as the rows
    /// on the way lay out (T889); without the scroll view to drive, the
    /// scroll SwiftUI animates (T829's pull back on top).
    private func jump(_ scroll: ScrollViewProxy) {
        glider.stop()
        follow.glide(newest: transcript.entries.last?.id)
        if glider.start(instant: reduceMotion, done: { follow.landed() }) { return }
        follow.landed()
        follow.jump(newest: transcript.entries.last?.id)
        withAnimation(reduceMotion ? nil : .default) { scroll.scrollTo("end", anchor: .bottom) }
    }

    /// The row the need was asked at.
    private func moment(_ rows: [Transcript.Row], _ session: AppAPI.Session?) -> Int? {
        guard let need, let e = transcript.moment(need.kind, since: session?.question?.since) else { return nil }
        return Transcript.row(of: e, in: rows)
    }

    private func earlier(_ id: String) -> some View {
        Button {
            Task { await page(id) }
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
            transcript.prepend(try await b.call(AppAPI.Methods.transcriptPage, AppAPI.TranscriptPageParams(session: id, before: transcript.before)))
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
                if let need, need.kind == .question, let a = Need.answer(s.id, q, index: i),
                   model.can(AppAPI.Methods.sessionAnswer.name) {
                    NeedActs(need: need, answer: a) { parts in
                        HeroCard(tone: .question, symbol: Need.Kind.question.symbol, word: word, line: line, title: item.question,
                                 note: parts.note, waiting: waits) {
                            parts.buttons
                        }
                    }
                } else {
                    // A multi-select of the agent's menu is picked in its
                    // terminal: the card says so, its options only read.
                    let several = QuestionPlan(q, index: i).paneOnly
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
            Text(entry.text)
                .font(theme.body(.subheadline, .medium))
                .foregroundStyle(theme.muted)
                .onGlass()
                .frame(maxWidth: .infinity)
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

/// The scroll geometry and phases into Follow: on iOS 18; 17 has none
/// to read, and its conversation always follows. The screen opens at the
/// end, and content that grows keeps its place (Follow scrolls a
/// follower down). Jump to latest glides (Glider, T889); without one, a
/// jump that a flick's momentum carries up is pulled back to the end
/// (toEnd, T829).
struct PhoneFollowGeometry: ViewModifier {
    @Binding var follow: Follow
    let newest: Int?
    /// Told the row at the top as the user scrolls (iOS 18).
    let top: (Int?) -> Void
    let toEnd: () -> Void

    func body(content: Content) -> some View {
        if #available(iOS 18, *) {
            content
                .defaultScrollAnchor(.bottom, for: .initialOffset)
                .defaultScrollAnchor(.top, for: .sizeChanges)
                .onScrollGeometryChange(for: [Double].self) { g in
                    [g.contentOffset.y, g.visibleRect.height, g.contentSize.height, g.contentInsets.bottom]
                } action: { _, g in
                    if follow.scrolled(offset: g[0], visible: g[1], content: g[2], inset: g[3], newest: newest) { toEnd() }
                }
                .onScrollPhaseChange { _, p in
                    follow.moved(by: p == .tracking || p == .interacting, resting: p == .idle)
                }
                .onScrollTargetVisibilityChange(idType: Int.self, threshold: 0.01) { top($0.first) }
        } else {
            content.defaultScrollAnchor(.bottom)
        }
    }
}

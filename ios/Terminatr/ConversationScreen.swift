import SwiftUI
import TerminatrKit
import TerminatrUI

/// A session's conversation from its transcript (transcript.watch: its
/// last items when the server pages, transcript.page for the earlier
/// ones on a tap), as the Mac's conversation tab draws it: prompts, the agent's text,
/// tool calls folded into runs (ToolRunHeader, a tap unfolds one), and
/// the open question (QuestionSummary) or approval at the end. At the
/// write level the prompt box and the terminal's keys are under it
/// (PhoneComposer); below it, nothing. Opened from a need (Route.need,
/// T826) the need's card is above it, with its buttons, and it opens
/// scrolled to where the need was asked (Transcript.moment), marked.
struct ConversationScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let ref: SessionRef
    /// The question or approval its card shows above the conversation.
    var need: Need?
    @State private var transcript = Transcript()
    @State private var failure: String?
    @State private var unfolded: Set<Int> = []
    @State private var paging = false
    @State private var pages = false
    /// Whether the conversation follows its end, or the user scrolled
    /// up (iOS 18, from the scroll geometry; 17 always follows).
    @State private var follow = Follow()
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
                            case .entry(let e): EntryLine(entry: e, agent: session?.agent ?? "")
                            case .tools(let run): runView(run)
                            }
                        }
                        .background(alignment: .leading) {
                            if row.id == moment, let need { MomentMark(colour: theme.colour(need.kind)) }
                        }
                        .id(row.id)
                    }
                    if let session, need == nil { waiting(session) }
                    // The foot's padding is the end row's: scrolled to,
                    // it reaches the true end (T807).
                    Color.clear.frame(height: 1).padding(.bottom, 16).id("end")
                }
                .padding([.horizontal, .top], 16)
                .scrollTargetLayout()
            }
            .modifier(PhoneFollowGeometry(follow: $follow, newest: transcript.entries.last?.id) { top in
                if restoring == nil { model.places.keep("top \(ref)", follow.following ? nil : top) }
            })
            // New output: the follower goes to the end; scrolled up, the
            // screen stays and Jump to latest counts it. Laid out afresh,
            // it goes back to the row the user had at the top.
            .onChange(of: Self.tail(transcript)) {
                if seeking, restoring == nil, let m = self.moment(Transcript.rows(transcript.entries), session) {
                    seeking = false
                    scroll.scrollTo(m, anchor: .bottom)
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
                if follow.following { scroll.scrollTo("end", anchor: .bottom) }
            }
            .overlay(alignment: .bottom) {
                if !follow.following {
                    Button {
                        follow.follow(newest: transcript.entries.last?.id)
                        withAnimation(reduceMotion ? nil : .default) { scroll.scrollTo("end", anchor: .bottom) }
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
        .safeAreaInset(edge: .top, spacing: 0) {
            if let need, let session { NeedCard(need: need) { waiting(session, pinned: true) } }
        }
        .safeAreaInset(edge: .bottom) {
            if id != nil {
                VStack(spacing: 0) {
                    // While the agent has its turn, as the Mac's (T803).
                    if let busy = Busy.of(session) {
                        WorkingBar(busy: busy, activity: Activity.of(transcript, current: session?.current ?? ""))
                            .padding(.horizontal, 16)
                            .padding(.bottom, 8)
                    }
                    if model.writes, let id {
                        PhoneComposer(ref: ref, session: id, running: session?.state == "working", queued: session?.queuedPrompts ?? 0)
                    }
                }
            }
        }
        .refreshable { await model.refresh() }
        .navigationTitle(model.title(ref))
        .navigationBarTitleDisplayMode(.inline)
        .backdrop()
        // A redial is a new bridge: the stream opens again on it.
        .task(id: "\(id ?? "")/\(model.generation)") { await watch(id) }
        .kept($unfolded, "runs \(ref)")
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
                withAnimation(.snappy(duration: 0.2)) {
                    if open { unfolded.remove(run.id) } else { unfolded.insert(run.id) }
                }
            } label: {
                ToolRunHeader(run: run, open: open)
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

    /// What the session waits on: its open question, or its dialog. A
    /// need's card (pinned) has its buttons below.
    @ViewBuilder
    private func waiting(_ s: AppAPI.Session, pinned: Bool = false) -> some View {
        if let q = s.question {
            ForEach(QuestionPlan.shown(q), id: \.self) { i in
                let waits = "question \(s.id) \(q.since?.timeIntervalSince1970 ?? 0) \(i)"
                if pinned, need?.answer?.index == i, model.can(AppAPI.Methods.sessionAnswer.name) {
                    // Its options are the need's buttons, below.
                    CardFrame(tag: QuestionPlan.tag(q, index: i), colour: theme.question, tint: theme.questionTint, waiting: waits) {
                        Text(q.questions[i].question)
                            .font(theme.title(.headline, .bold))
                            .fixedSize(horizontal: false, vertical: true)
                    }
                } else {
                    QuestionSummary(menu: q, index: i, waiting: waits)
                }
            }
        }
        if s.state == "blocked", s.reason == "permission" {
            CardFrame(tag: s.escape ? "Approval · outside the sandbox" : "Approval", colour: theme.approval, tint: theme.approvalTint, waiting: "approval \(s.id) \(transcript.pendingTool?.call ?? "")") {
                Text(transcript.pendingTool.map { "\($0.tool): \($0.text)" } ?? "The agent asks to go on.")
                    .font(theme.mono(.subheadline))
                    .fixedSize(horizontal: false, vertical: true)
                if !pinned {
                    Text("Approve it on your Mac.")
                        .font(theme.body(.footnote))
                        .foregroundStyle(theme.muted)
                }
            }
        }
    }

    private func watch(_ id: String?) async {
        transcript = Transcript()
        failure = nil
        guard let id, let b = model.bridge else { return }
        let hello = await b.hello
        pages = hello?.methods.contains(AppAPI.Methods.transcriptPage.name) == true
        let tail = hello?.server?.caps.contains("transcript-tail") == true ? Self.tailItems : 0
        do {
            let sub = try await b.subscribe(AppAPI.Methods.transcriptWatch, AppAPI.TranscriptParams(session: id, tail: tail))
            transcript.start(sub.result.status)
            for try await e in sub.events {
                transcript.apply(e.name, e.data)
            }
        } catch {
            if !Task.isCancelled { failure = error.localizedDescription }
        }
    }
}

/// A need's card above its conversation: what the session waits on and
/// the need's buttons (NeedActs), scrolling on its own past a third of
/// the screen (220 points on a short one) so the conversation stays in
/// view.
struct NeedCard<Waiting: View>: View {
    @Environment(\.theme) private var theme
    let need: Need
    @ViewBuilder let waiting: Waiting
    @State private var height: CGFloat = 0

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 8) {
                waiting
                NeedActs(need: need)
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 10)
            .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { height = $0 }
        }
        .scrollBounceBehavior(.basedOnSize)
        .containerRelativeFrame(.vertical) { h, _ in min(height, max(h / 3, 220)) }
        .background(.bar)
        .overlay(alignment: .bottom) { Rectangle().fill(theme.edge).frame(height: 1) }
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
/// conversation as any other.
struct NeedConversation: View {
    @Environment(PhoneModel.self) private var model
    let ref: SessionRef

    var body: some View {
        let id = model.sessionID(ref)
        ConversationScreen(ref: ref, need: model.needs.first { $0.session == id && ($0.kind == .question || $0.kind == .approval) })
    }
}

/// One entry, as the Mac's EntryView draws it, without its files and
/// folds: a prompt as a bubble on the right, the agent's text as a card.
struct EntryLine: View {
    @Environment(\.theme) private var theme
    let entry: Transcript.Entry
    let agent: String
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
                        .onTapGesture { if entry.folds { unfolded = false } }
                    Text(entry.preview() ?? entry.text)
                        .foregroundStyle(theme.onPrompt)
                        .padding(.horizontal, 13)
                        .padding(.vertical, 9)
                        .background(LinearGradient(colors: theme.prompt, startPoint: .topLeading, endPoint: .bottomTrailing),
                                    in: RoundedRectangle(cornerRadius: 14))
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
        case .tool, .files:
            ToolLine(entry: entry)
        case .result:
            if let r = entry.result {
                Text("\(r.text)\(r.lines > 1 ? " · \(r.lines) lines" : "")")
                    .font(theme.mono(.footnote))
                    .foregroundStyle(r.error ? theme.attention : theme.muted)
                    .lineLimit(1)
                    .padding(.leading, 12)
            }
        case .note:
            Text(entry.text)
                .font(theme.body(.subheadline, .medium))
                .foregroundStyle(theme.muted)
                .frame(maxWidth: .infinity)
        }
    }

    private var turn: some View {
        Text([entry.who(agent: agent), entry.at?.formatted(date: .omitted, time: .shortened) ?? ""].filter { !$0.isEmpty }.joined(separator: " · "))
            .font(theme.mono(.caption, .medium))
            .foregroundStyle(theme.muted)
    }
}

/// The scroll geometry into Follow: on iOS 18; 17 has none to read, and
/// its conversation always follows. The screen opens at the end, and
/// content that grows keeps its place (Follow scrolls a follower down).
struct PhoneFollowGeometry: ViewModifier {
    @Binding var follow: Follow
    let newest: Int?
    /// Told the row at the top as the user scrolls (iOS 18).
    let top: (Int?) -> Void

    func body(content: Content) -> some View {
        if #available(iOS 18, *) {
            content
                .defaultScrollAnchor(.bottom, for: .initialOffset)
                .defaultScrollAnchor(.top, for: .sizeChanges)
                .onScrollGeometryChange(for: [Double].self) { g in
                    [g.contentOffset.y, g.visibleRect.height, g.contentSize.height, g.contentInsets.bottom]
                } action: { _, g in
                    follow.scrolled(offset: g[0], visible: g[1], content: g[2], inset: g[3], newest: newest)
                }
                .onScrollTargetVisibilityChange(idType: Int.self, threshold: 0.01) { top($0.first) }
        } else {
            content.defaultScrollAnchor(.bottom)
        }
    }
}

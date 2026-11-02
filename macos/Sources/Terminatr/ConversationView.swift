import AppKit
import SwiftUI
import TerminatrKit
import TerminatrUI

/// The Conversation tab: the transcript, the cards for what the session
/// waits on (a question, a permission dialog, a task in review), and
/// the composer, always at the foot. A coordinator's open questions and
/// its project's needs lead, above the transcript (CoordinatorPanel); a
/// thread's cards sit over the composer, and while one is open and the
/// transcript is scrolled up, Jump to the open card brings it back. ⌘F
/// opens a find bar over it: the entries that show the words wear a
/// ring, and ↩, ⌘G and ⇧⌘G scroll from one to the next. A long
/// transcript draws its last turns (drawn), and more on a click, so
/// showing it costs no more than a short one. Consecutive tool calls
/// fold into one group (ToolRunView). A paired server's session shows
/// its conversation alone: no card of this server's, a question open
/// there as Answer in Terminal (its text stays there), and the composer
/// at write, else a muted View only line.
struct ConversationTab: View {
    @Environment(AppModel.self) private var app
    @Environment(\.theme) private var theme
    /// Reduce Motion (T748 J): the transcript grows and scrolls without
    /// animating.
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let model: SessionModel
    let session: AppAPI.Session?
    let state: ConversationState
    @State private var finding = false
    @State private var query = ""
    @State private var at = 0
    @FocusState private var findFocused: Bool
    /// Whether the transcript follows its end, or the user scrolled up
    /// (macOS 15, from the scroll geometry; 14 always follows).
    @State private var follow = Follow()
    /// Jump to latest's glide to the end, and the follower's (T895).
    @State private var glider = Glider()
    /// Bumped by Jump to the open card: the cards flash.
    @State private var flash = 0
    /// How many of the last entries the transcript draws.
    @State private var drawn = Self.page

    /// The entries drawn at first, and added by each Show earlier.
    static let page = 60

    var body: some View {
        let matches = finding ? Transcript.find(query, in: model.transcript.entries) : []
        let found = Set(matches)
        let current = matches.isEmpty ? nil : matches[min(at, matches.count - 1)]
        let coordinator = model.ref.isCoordinator
        let remote = model.ref.isRemote ? app.remoteConversation(model.ref) : nil
        let peerSession = model.ref.session(in: app.peers)
        let review = coordinator || remote != nil ? nil : app.thread(model.ref).flatMap { t in t.task?.status == "review" ? t : nil }
        let open = AppModel.waitsOnCard(session) || review != nil || peerSession?.asking == true
        let entries = model.transcript.tail(drawn, keeping: current)
        // The AskUserQuestion call the open question card answers.
        let asking = session?.question != nil && AppModel.waitsOnCard(session)
        let pending = model.transcript.pendingTool?.id
        let earlier = model.transcript.entries.count - entries.count
        let rows = Transcript.rows(entries)
        VStack(spacing: 0) {
            if finding {
                findBar(matches.count)
            }
            if coordinator && remote == nil {
                CoordinatorPanel(model: model, session: session)
                    .modifier(Flash(n: flash))
            }
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 12) {
                        if earlier > 0 {
                            Button("Show \(min(earlier, Self.page)) earlier of \(earlier)") { drawn += Self.page }
                                .buttonStyle(.borderless)
                                .font(theme.body(.footnote))
                                .frame(maxWidth: .infinity)
                        } else if model.transcript.skipped > 0 {
                            Text("\(model.transcript.skipped) earlier items not shown")
                                .font(theme.body(.footnote))
                                .foregroundStyle(theme.muted)
                                .frame(maxWidth: .infinity)
                        }
                        ForEach(rows) { row in
                            switch row {
                            case .entry(let e):
                                // A paired session has no session here: its files stay there (T772).
                                EntryView(entry: e, agent: session?.agent ?? peerSession?.agent ?? "agent", session: session?.id,
                                          answer: asking && e.id == pending ? { flash += 1 } : nil)
                                    .modifier(FoundRing(found: found.contains(e.id), current: e.id == current))
                                    .id(e.id)
                            case .tools(let run):
                                ToolRunView(run: run, model: model, found: found, current: current)
                                    .id(run.id)
                            }
                        }
                        switch state {
                        case .loading:
                            ProgressView().frame(maxWidth: .infinity)
                        case .waiting:
                            Text("Waiting for the agent to name its transcript…")
                                .font(theme.body(.subheadline))
                                .foregroundStyle(theme.muted)
                                .frame(maxWidth: .infinity)
                        case .unavailable(let why):
                            Text(why)
                                .font(theme.body(.subheadline))
                                .foregroundStyle(theme.muted)
                                .frame(maxWidth: .infinity)
                        case .mapped:
                            EmptyView()
                        }
                        if let closed = model.transcript.closed {
                            Text(model.ref.isRemote ? RemoteConversation.ended(closed, alias: model.ref.server) : "The session ended (\(closed)).")
                                .font(theme.body(.subheadline))
                                .foregroundStyle(theme.muted)
                                .frame(maxWidth: .infinity)
                        }
                        // The foot's padding is the end row's: scrolled to,
                        // it reaches the true end (T807).
                        Color.clear.frame(height: 1).padding(.bottom, 14).id(Self.end)
                    }
                    .padding(.horizontal, Page.edge)
                    .padding(.top, 14)
                    .background { ScrollViewHook(glider: glider) }
                }
                .modifier(FollowAnchor())
                // Streamed text lands in place: no implicit animation.
                .transaction { if reduceMotion { $0.animation = nil } }
                .modifier(FollowGeometry(follow: $follow, newest: model.transcript.entries.last?.id) {
                    proxy.scrollTo(Self.end, anchor: .bottom)
                })
                // New output, or a growing last entry: the follower goes
                // to the end; one scrolled up stays, and counts it. A
                // glide under way stays on the end it moves.
                .onChange(of: Self.tail(model.transcript)) {
                    if follow.arrived(model.transcript.entries.map(\.id)) {
                        toEnd(proxy)
                    } else if follow.gliding {
                        glider.renew()
                    }
                }
                // The Working bar or a card coming or going.
                .onChange(of: Busy.of(session) != nil) {
                    if follow.following { toEnd(proxy) }
                }
                // A prompt sent: back to the end.
                .onChange(of: model.sent) { jumpToLatest(proxy) }
                // The user took hold of the scroll: the glide ends there.
                .onChange(of: follow.gliding) { _, g in if !g { glider.stop() } }
                .onDisappear { glider.leave() }
                .onChange(of: current) { _, c in
                    guard let c, let row = Transcript.row(of: c, in: rows) else { return }
                    // A match in a folded group unfolds it, and it stays so.
                    if row != c { model.unfoldedRuns.insert(row) }
                    glider.stop()
                    withAnimation(motion) { proxy.scrollTo(row, anchor: .center) }
                }
                .scrollEdges()
                // The cards and the composer at the foot: the transcript
                // ends above them and scrolls behind them under the edge.
                .footBar(
                    VStack(spacing: 10) {
                        // While the agent has its turn: how long, and
                        // what it does now (T803).
                        if let busy = Busy.of(session) {
                            WorkingBar(busy: busy, activity: Activity.of(model.transcript, current: session?.current ?? ""))
                                .padding(.horizontal, Page.edge)
                                .transition(.opacity)
                        }
                        VStack(spacing: 10) {
                            if let review, let task = review.task {
                                ReviewCard(project: model.ref.project, thread: review, task: task)
                                    .padding(.horizontal, Page.edge)
                            }
                            if remote != nil {
                                if peerSession?.asking == true {
                                    RemoteQuestionCard(model: model)
                                }
                            } else if !coordinator {
                                Cards(model: model, session: session, firstChoice: app.firstChoice)
                            }
                        }
                        .modifier(Flash(n: flash))
                        if let remote, !remote.sends {
                            ViewOnlyLine(server: model.ref.server)
                        } else {
                            Composer(model: model, session: session)
                        }
                    }
                    .overlay(alignment: .top) {
                        if !follow.following {
                            // Above the bar, over the transcript's edge.
                            jump(proxy, toCard: open && (!coordinator || remote != nil))
                                .padding(.bottom, 8)
                                .frame(height: 0, alignment: .bottom)
                        }
                    }
                )
            }
        }
        .guardingLinks()
        // What sets no font of its own: the system's body, scaled.
        .font(theme.body(.body))
        .onChange(of: app.find) { _, r in
            if r.step == 0 || !finding {
                finding = true
                findFocused = true
            } else {
                step(r.step, of: matches.count)
            }
        }
        .onChange(of: query) { at = 0 }
    }

    /// A scroll's animation: none with Reduce Motion.
    private var motion: Animation? { reduceMotion ? nil : .default }

    /// The transcript's last row: what following scrolls to.
    static let end = "end"

    /// What changes as output comes: the entries' count, and the last
    /// one's text and result as it streams.
    static func tail(_ t: Transcript) -> [Int] {
        [t.entries.count, t.entries.last?.id ?? -1, t.entries.last?.text.count ?? 0, t.entries.last?.result == nil ? 0 : 1]
    }

    /// Jump to latest, or to the open card when one waits (its cards
    /// flash): frosted, with a shadow, so it reads over whatever scrolls
    /// behind it. Either follows the end again.
    private func jump(_ proxy: ScrollViewProxy, toCard: Bool) -> some View {
        Button {
            jumpToLatest(proxy)
            if toCard { flash += 1 }
        } label: {
            Label(toCard ? "Jump to the open card" : follow.label, systemImage: "arrow.down")
                .font(theme.body(.subheadline, .semibold))
                .padding(.horizontal, 12)
                .padding(.vertical, 6)
                .frosted(Capsule())
                .overlay(Capsule().strokeBorder(theme.edge, lineWidth: 1))
                .shadow(color: .black.opacity(0.18), radius: 8, y: 3)
        }
        .buttonStyle(.plain)
        .help("Scrolls to the end, where the card waits")
    }

    /// Jump to latest, or a prompt sent: a glide to the end that lands
    /// on it as the rows on the way lay out (T895); without the scroll
    /// view to drive, the scroll SwiftUI animates (T829's pull back on
    /// top).
    private func jumpToLatest(_ proxy: ScrollViewProxy) {
        glider.stop()
        follow.glide(newest: model.transcript.entries.last?.id)
        if glider.start(instant: reduceMotion, done: { follow.landed() }) { return }
        follow.landed()
        follow.jump(newest: model.transcript.entries.last?.id)
        withAnimation(motion) { proxy.scrollTo(Self.end, anchor: .bottom) }
    }

    /// The follower to the end (T895, the iPhone's T901): new output, the
    /// agent's turn starting or ending. An instant glide that stays on
    /// the end as each frame has it until that holds: scrollTo aims once
    /// at the end a LazyVStack estimates. A glide already running stays
    /// on the end for the new output too. Without the scroll view to
    /// drive, SwiftUI's scroll.
    private func toEnd(_ proxy: ScrollViewProxy) {
        if glider.running { return glider.renew() }
        if glider.start(instant: true, done: { follow.landed() }) {
            follow.glide(newest: nil)
            return
        }
        proxy.scrollTo(Self.end, anchor: .bottom)
    }

    private func step(_ by: Int, of n: Int) {
        guard n > 0 else { return }
        at = ((min(at, n - 1) + by) % n + n) % n
    }

    private func findBar(_ n: Int) -> some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass")
                .foregroundStyle(theme.muted)
                .accessibilityHidden(true)
            TextField("Find in the conversation", text: $query)
                .textFieldStyle(.plain)
                .focused($findFocused)
                .onSubmit { step(1, of: n) }
                .onExitCommand { close() }
            if !query.trimmingCharacters(in: .whitespaces).isEmpty {
                Text(n == 0 ? "No matches" : "\(min(at, n - 1) + 1) of \(n)")
                    .font(theme.mono(.caption))
                    .foregroundStyle(theme.muted)
                    .fixedSize()
            }
            Button { step(-1, of: n) } label: { Image(systemName: "chevron.up") }
                .accessibilityLabel("Previous match")
                .disabled(n == 0)
                .help("Previous match (⇧⌘G)")
            Button { step(1, of: n) } label: { Image(systemName: "chevron.down") }
                .accessibilityLabel("Next match")
                .disabled(n == 0)
                .help("Next match (⌘G)")
            Button("Done") { close() }
        }
        .buttonStyle(.borderless)
        .font(theme.body(.callout))
        .padding(.horizontal, 12)
        .padding(.vertical, 7)
        .frosted(RoundedRectangle(cornerRadius: 10))
        .overlay(RoundedRectangle(cornerRadius: 10).strokeBorder(theme.edge, lineWidth: 1))
        .padding(.horizontal, Page.edge)
        .padding(.bottom, 4)
    }

    private func close() {
        finding = false
        query = ""
    }
}

/// A question open in a paired server's session: its text stays on that
/// server (peer.list says only that one is open), so the card sends the
/// user to the session's pane, where the question shows and its keys
/// answer it.
struct RemoteQuestionCard: View {
    @Environment(\.theme) private var theme
    let model: SessionModel

    var body: some View {
        CardFrame(tag: "Question on \(model.ref.server)", colour: theme.question, tint: theme.questionTint, waiting: "remote question \(model.ref.card)") {
            Text("A question is open in this session. Its text stays on \(model.ref.server): it shows in the pane, where your keys answer it.")
                .fixedSize(horizontal: false, vertical: true)
            Button("Answer in Terminal") { model.pick(.terminal) }
                .buttonStyle(.borderedProminent)
                .tint(theme.action)
                .help("Opens the session's pane")
        }
        .padding(.horizontal, Page.edge)
    }
}

/// The composer's place on a paired server's session at view: that
/// server lets this one read, not send.
struct ViewOnlyLine: View {
    @Environment(\.theme) private var theme
    let server: String

    var body: some View {
        Label("View only: \(server) lets this server read this session, not send to it", systemImage: "eye")
            .font(theme.body(.footnote))
            .foregroundStyle(theme.muted)
            .padding(.horizontal, Page.edge)
            .padding(.top, 10)
            .padding(.bottom, 14)
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// The scroll geometry and phases into Follow: on macOS 15; 14 has none
/// to read, and its conversation always follows. Jump to latest glides
/// (Glider, T895); without one, a jump that trackpad momentum carries up
/// is pulled back to the end (toEnd, T829).
struct FollowGeometry: ViewModifier {
    @Binding var follow: Follow
    let newest: Int?
    let toEnd: () -> Void

    func body(content: Content) -> some View {
        if #available(macOS 15, *) {
            content.onScrollGeometryChange(for: [Double].self) { g in
                [g.contentOffset.y, g.visibleRect.height, g.contentSize.height, g.contentInsets.bottom, g.contentInsets.top]
            } action: { _, g in
                if follow.scrolled(offset: g[0], visible: g[1], content: g[2], inset: g[3], top: g[4], newest: newest) { toEnd() }
            }
            .onScrollPhaseChange { _, p in
                if follow.moved(by: p == .tracking || p == .interacting, resting: p == .idle) { toEnd() }
            }
        } else {
            content
        }
    }
}

/// Opens at the end; on macOS 15 content that grows keeps its place
/// (Follow scrolls a follower down), so a user scrolled up never jumps.
/// 14 keeps to the bottom as it grows.
struct FollowAnchor: ViewModifier {
    func body(content: Content) -> some View {
        if #available(macOS 15, *) {
            content
                .defaultScrollAnchor(.bottom, for: .initialOffset)
                .defaultScrollAnchor(.top, for: .sizeChanges)
        } else {
            content.defaultScrollAnchor(.bottom)
        }
    }
}

/// A find's match wears a ring: a strong one on the current match.
struct FoundRing: ViewModifier {
    @Environment(\.theme) private var theme
    let found: Bool
    let current: Bool

    func body(content: Content) -> some View {
        content.overlay {
            if found {
                RoundedRectangle(cornerRadius: 14)
                    .strokeBorder(theme.action.opacity(current ? 1 : 0.4), lineWidth: current ? 2 : 1)
                    .padding(-3)
            }
        }
    }
}

/// The open cards flash once when the user jumps to them.
struct Flash: ViewModifier {
    @Environment(\.theme) private var theme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let n: Int
    @State private var on = false

    func body(content: Content) -> some View {
        content
            .overlay {
                RoundedRectangle(cornerRadius: 16)
                    .strokeBorder(theme.action, lineWidth: 2)
                    .padding(.horizontal, 16)
                    .opacity(on ? 1 : 0)
                    .allowsHitTesting(false)
            }
            .onChange(of: n) {
                // With Reduce Motion the ring shows and goes, without a fade.
                withAnimation(reduceMotion ? nil : .easeOut(duration: 0.2)) { on = true }
                withAnimation(reduceMotion ? .linear(duration: 0).delay(1) : .easeIn(duration: 0.6).delay(0.8)) { on = false }
            }
    }
}

/// A thread whose task is in review, over its composer: the task, its
/// PR, and Accept or Send back… (task.accept, task.send-back), which
/// ask the coordinator as the dashboard's do.
struct ReviewCard: View {
    @Environment(\.theme) private var theme
    let project: String
    let thread: AppAPI.Thread
    let task: AppAPI.Task

    var body: some View {
        CardFrame(tag: "Review", colour: theme.good, tint: theme.cardTint, waiting: "review \(project) \(task.ref)") {
            HStack(alignment: .center, spacing: 10) {
                VStack(alignment: .leading, spacing: 3) {
                    Text("\(task.ref) is ready to review")
                        .font(theme.title(.headline, .bold))
                    Text(facts)
                        .font(theme.body(.subheadline))
                        .foregroundStyle(theme.muted)
                        .lineLimit(1)
                        .textSelection(.enabled)
                }
                Spacer(minLength: 8)
                TaskActs(project: project, task: task.ref, status: task.status)
            }
            TaskOutcome(project: project, task: task.ref)
        }
    }

    private var facts: String {
        var f = [task.title]
        if thread.pr > 0 {
            f.append("PR #\(thread.pr)" + (thread.checks.isEmpty ? "" : " · checks \(thread.checks)"))
        } else if !thread.reportPr.isEmpty {
            f.append(thread.reportPr)
        }
        return f.joined(separator: " · ")
    }
}

/// One entry of the conversation: each turn says who and when ("you ·
/// 20:45", "claude · 20:45"); tm's prompts fold to one line the user
/// opens (the thread's kickoff, the server's notes), and long prompts to
/// their first lines. Files sent or pasted show as attachments.
struct EntryView: View {
    @Environment(\.theme) private var theme
    let entry: Transcript.Entry
    let agent: String
    /// The session's id, which reads its files.
    var session: String?
    /// The question card's link, for the AskUserQuestion call it answers.
    var answer: (() -> Void)? = nil
    @State private var unfolded = false

    var body: some View {
        switch entry.kind {
        case .prompt where entry.folds && !unfolded:
            FoldedPrompt(entry: entry, when: Self.clock(entry.at)) { unfolded = true }
        case .prompt:
            let preview = unfolded ? nil : entry.preview()
            HStack {
                Spacer(minLength: 80)
                VStack(alignment: .trailing, spacing: 4) {
                    turn
                        .onTapGesture { if entry.folds || entry.preview() != nil { unfolded = false } }
                    if !entry.text.isEmpty || entry.files.isEmpty {
                        Text(preview ?? entry.text)
                            .textSelection(.enabled)
                            .foregroundStyle(theme.onPrompt)
                            .padding(.horizontal, 13)
                            .padding(.vertical, 9)
                            .background(LinearGradient(colors: theme.prompt, startPoint: .topLeading, endPoint: .bottomTrailing), in: RoundedRectangle(cornerRadius: 14))
                    }
                    if !entry.files.isEmpty {
                        Attachments(files: entry.attachments, session: session, alignment: .trailing)
                    }
                    if preview != nil {
                        Button("Show all \(entry.lineCount) lines") { unfolded = true }
                            .buttonStyle(.borderless)
                            .font(theme.body(.footnote))
                            .accessibilityHint("Shows the whole prompt")
                    }
                    if entry.truncated { cut }
                }
            }
        case .text:
            VStack(alignment: .leading, spacing: 6) {
                turn
                MarkdownView(text: entry.text)
                if entry.truncated { cut }
            }
            .padding(14)
            .frame(maxWidth: .infinity, alignment: .leading)
            .frosted(RoundedRectangle(cornerRadius: 14))
            .background(theme.agentCard, in: RoundedRectangle(cornerRadius: 14))
            .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(theme.edge, lineWidth: 1))
        case .tool:
            ToolLine(entry: entry, answer: answer)
        case .files:
            AttachmentCard(entry: entry, session: session,
                           turn: [entry.who(agent: agent), Self.clock(entry.at)].filter { !$0.isEmpty }.joined(separator: " · "))
        case .result:
            if let outcome = entry.questionOutcome {
                AnsweredQuestionCard(outcome: outcome)
            } else if let r = entry.result {
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

    /// Who wrote the turn, and when: "you · 20:45".
    private var turn: some View {
        Text([entry.who(agent: agent), Self.clock(entry.at)].filter { !$0.isEmpty }.joined(separator: " · "))
            .font(theme.mono(.caption, .medium))
            .foregroundStyle(theme.muted)
    }

    /// When a turn was: its time today, else its day and time.
    static func clock(_ at: Date?, now: Date = Date()) -> String {
        guard let at else { return "" }
        let cal = Calendar.current
        if cal.isDate(at, inSameDayAs: now) { return at.formatted(date: .omitted, time: .shortened) }
        if let days = cal.dateComponents([.day], from: at, to: now).day, days < 6 {
            return at.formatted(.dateTime.weekday(.abbreviated).hour().minute())
        }
        return at.formatted(.dateTime.day().month(.abbreviated).hour().minute())
    }

    private var cut: some View {
        Text("Cut short: the rest is in the Terminal tab.")
            .font(theme.body(.caption))
            .foregroundStyle(theme.muted)
    }
}

/// Consecutive tool calls as one box: a header that counts them by tool
/// ("Read ×5 · Bash ×3") with their state, folded until the user opens
/// it (a click, space, → and ←, VoiceOver's action) to their ToolLines.
/// Folded, a last call still running shows under the header. A group
/// with a find's match shows open; the user's unfolding is kept on the
/// session's model, by the run's first call, so streaming calls never
/// refold it.
struct ToolRunView: View {
    @Environment(\.theme) private var theme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let run: Transcript.ToolRun
    let model: SessionModel
    let found: Set<Int>
    let current: Int?

    var body: some View {
        let unfolded = model.unfoldedRuns.contains(run.id)
        let open = unfolded || run.calls.contains { found.contains($0.id) }
        VStack(alignment: .leading, spacing: 6) {
            Button { fold(open: !unfolded) } label: {
                ToolRunHeader(run: run, open: open)
            }
            .buttonStyle(.plain)
            .onKeyPress(keys: [.leftArrow, .rightArrow]) { press in
                fold(open: press.key == .rightArrow)
                return .handled
            }
            .help(open ? "Fold the tool calls" : "Show each tool call")
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(run.spoken + ", " + ToolRunHeader.stateWord(run))
            .accessibilityValue(open ? "expanded" : "collapsed")
            .accessibilityHint(open ? "Folds the tool calls" : "Shows each tool call")
            .accessibilityAddTraits(.isButton)
            .accessibilityAction(named: open ? "Collapse" : "Expand") { fold(open: !unfolded) }
            if open {
                ForEach(run.calls) { c in
                    ToolLine(entry: c)
                        .modifier(FoundRing(found: found.contains(c.id), current: c.id == current))
                }
                .padding(.leading, 14)
            } else if let live = run.live {
                ToolLine(entry: live)
                    .padding(.leading, 14)
            }
        }
    }

    private func fold(open: Bool) {
        withAnimation(reduceMotion ? nil : .snappy(duration: 0.2)) {
            if open {
                model.unfoldedRuns.insert(run.id)
            } else {
                model.unfoldedRuns.remove(run.id)
            }
        }
    }
}

extension View {
    /// Links in this view open only after the user confirmed them,
    /// shown with their full URL, and only http, https and mailto
    /// (design T734, R6).
    func guardingLinks() -> some View {
        modifier(LinkGuard())
    }
}

struct LinkGuard: ViewModifier {
    @State private var pending: URL?

    func body(content: Content) -> some View {
        content
            .environment(\.openURL, OpenURLAction { url in
                pending = url
                return .handled
            })
            .alert("Open this link?", isPresented: Binding(get: { pending != nil }, set: { if !$0 { pending = nil } }), presenting: pending) { url in
                if SafeMarkdown.allowed(url) {
                    Button("Open") { NSWorkspace.shared.open(url) }
                }
                Button("Copy Link") {
                    NSPasteboard.general.clearContents()
                    NSPasteboard.general.setString(url.absoluteString, forType: .string)
                }
                Button("Cancel", role: .cancel) {}
            } message: { url in
                Text(SafeMarkdown.allowed(url) ? url.absoluteString : "\(url.absoluteString)\n\nOnly web and mail links open from the app.")
            }
    }
}

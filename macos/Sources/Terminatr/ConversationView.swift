import AppKit
import SwiftUI
import TerminatrKit

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
/// fold into one group (ToolRunView).
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
    /// Whether the transcript's end is out of sight (macOS 15).
    @State private var scrolledUp = false
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
        let review = coordinator ? nil : app.thread(model.ref).flatMap { t in t.task?.status == "review" ? t : nil }
        let open = AppModel.waitsOnCard(session) || review != nil
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
            if coordinator {
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
                                EntryView(entry: e, agent: session?.agent ?? "agent", session: session?.id,
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
                            Text("The session ended (\(closed)).")
                                .font(theme.body(.subheadline))
                                .foregroundStyle(theme.muted)
                                .frame(maxWidth: .infinity)
                        }
                    }
                    .padding(.horizontal, Page.edge)
                    .padding(.vertical, 14)
                }
                .defaultScrollAnchor(.bottom)
                // Streamed text lands in place: no implicit animation.
                .transaction { if reduceMotion { $0.animation = nil } }
                .modifier(ScrolledUp(up: $scrolledUp))
                .onChange(of: current) { _, c in
                    guard let c, let row = Transcript.row(of: c, in: rows) else { return }
                    // A match in a folded group unfolds it, and it stays so.
                    if row != c { model.unfoldedRuns.insert(row) }
                    withAnimation(motion) { proxy.scrollTo(row, anchor: .center) }
                }
                .scrollEdges()
                // The cards and the composer at the foot: the transcript
                // ends above them and scrolls behind them under the edge.
                .footBar(
                    VStack(spacing: 10) {
                        VStack(spacing: 10) {
                            if let review, let task = review.task {
                                ReviewCard(project: model.ref.project, thread: review, task: task)
                                    .padding(.horizontal, Page.edge)
                            }
                            if !coordinator {
                                Cards(model: model, session: session, firstChoice: app.firstChoice)
                            }
                        }
                        .modifier(Flash(n: flash))
                        Composer(model: model, session: session)
                    }
                    .overlay(alignment: .top) {
                        if open && scrolledUp && !coordinator {
                            // Above the bar, over the transcript's edge.
                            jump(proxy)
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

    /// Jump to the open card: frosted, with a shadow, so it reads over
    /// whatever scrolls behind it.
    private func jump(_ proxy: ScrollViewProxy) -> some View {
        Button {
            if let last = model.transcript.entries.last { withAnimation(motion) { proxy.scrollTo(last.id, anchor: .bottom) } }
            flash += 1
        } label: {
            Label("Jump to the open card", systemImage: "arrow.down")
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

/// While the transcript's end is out of sight: on macOS 15, from its
/// scroll geometry; never on 14, which has none to read.
struct ScrolledUp: ViewModifier {
    @Binding var up: Bool

    func body(content: Content) -> some View {
        if #available(macOS 15, *) {
            content.onScrollGeometryChange(for: Bool.self) { g in
                g.contentOffset.y + g.containerSize.height < g.contentSize.height - 80
            } action: { _, new in
                up = new
            }
        } else {
            content
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
        CardFrame(tag: "Review", colour: theme.good, tint: theme.cardTint) {
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
            Button { unfolded = true } label: {
                HStack(alignment: .firstTextBaseline, spacing: 6) {
                    Image(systemName: "chevron.right")
                        .font(theme.body(.glyph, .bold))
                    Text("tm")
                        .font(theme.mono(.footnote, .medium))
                    Text([entry.headline, Self.clock(entry.at)].filter { !$0.isEmpty }.joined(separator: " · "))
                        .font(theme.body(.subheadline))
                        .lineLimit(1)
                    Spacer(minLength: 0)
                }
                .foregroundStyle(theme.muted)
                .padding(.horizontal, 12)
                .padding(.vertical, 6)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .help("Show what tm sent")
            .accessibilityHint("Shows the prompt tm sent")
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
                header(open: open)
            }
            .buttonStyle(.plain)
            .onKeyPress(keys: [.leftArrow, .rightArrow]) { press in
                fold(open: press.key == .rightArrow)
                return .handled
            }
            .help(open ? "Fold the tool calls" : "Show each tool call")
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(run.spoken + ", " + stateWord)
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

    private func header(open: Bool) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Image(systemName: "chevron.right")
                .font(theme.body(.glyph, .bold))
                .foregroundStyle(theme.muted)
                .rotationEffect(.degrees(open ? 90 : 0))
            Text(run.headline)
                .font(theme.mono(.subheadline, .medium))
                .foregroundStyle(theme.action)
                .lineLimit(1)
                .truncationMode(.tail)
            switch run.state {
            case .running:
                ProgressView().controlSize(.mini)
            case .failed:
                Text("· failed")
                    .font(theme.mono(.footnote))
                    .foregroundStyle(theme.attention)
            case .done:
                Text("· done")
                    .font(theme.mono(.footnote))
                    .foregroundStyle(theme.muted)
            }
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 7)
        .background(theme.command, in: RoundedRectangle(cornerRadius: 9))
        .overlay(RoundedRectangle(cornerRadius: 9).strokeBorder(theme.edge, lineWidth: 1))
        .contentShape(Rectangle())
    }

    private var stateWord: String {
        switch run.state {
        case .running: "running"
        case .failed: "failed"
        case .done: "done"
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

/// A tool call folded to one line: the tool, its main argument and its
/// result's first line. An AskUserQuestion call without its answer
/// waits on the user, not on a spinner: it says so, with a link that
/// shows the question card.
struct ToolLine: View {
    @Environment(\.theme) private var theme
    let entry: Transcript.Entry
    var answer: (() -> Void)? = nil

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(entry.tool.isEmpty ? "tool" : entry.tool)
                .font(theme.mono(.subheadline, .medium))
                .foregroundStyle(theme.action)
            Text(entry.text)
                .font(theme.mono(.subheadline))
                .lineLimit(1)
                .truncationMode(.middle)
                .textSelection(.enabled)
            if let r = entry.result {
                Text(summary(r))
                    .font(theme.mono(.footnote))
                    .foregroundStyle(r.error ? theme.attention : theme.muted)
                    .lineLimit(1)
            } else if entry.asksUser {
                Text("· Waiting for your answer")
                    .font(theme.mono(.footnote))
                    .foregroundStyle(theme.question)
                    .lineLimit(1)
                if let answer {
                    Button("Answer", action: answer)
                        .buttonStyle(.link)
                        .font(theme.body(.footnote, .semibold))
                        .foregroundStyle(theme.action)
                        .help("Shows the question card")
                }
            } else {
                ProgressView().controlSize(.mini)
            }
            Spacer(minLength: 0)
        }
        .padding(.horizontal, 12)
        .padding(.vertical, 7)
        .background(theme.command, in: RoundedRectangle(cornerRadius: 9))
        .accessibilityElement(children: .combine)
    }

    private func summary(_ r: Transcript.Result) -> String {
        var parts: [String] = []
        if r.error { parts.append("error") }
        if !r.text.isEmpty { parts.append(r.text) }
        if r.lines > 1 { parts.append("\(r.lines) lines") }
        return "· " + (parts.isEmpty ? "done" : parts.joined(separator: " · "))
    }
}

/// An agent's Markdown, cleaned by SafeMarkdown: no raw HTML, no
/// images, and links that open only on a click confirmed with their
/// URL (guardingLinks).
struct MarkdownView: View {
    @Environment(\.theme) private var theme
    let text: String

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            ForEach(Array(SafeMarkdown.cached(text).enumerated()), id: \.offset) { _, b in
                block(b)
            }
        }
        .textSelection(.enabled)
        .tint(theme.action)
    }

    @ViewBuilder
    private func block(_ b: SafeMarkdown.Block) -> some View {
        switch b {
        case .paragraph(let s):
            Text(s).fixedSize(horizontal: false, vertical: true)
        case .heading(let level, let s):
            Text(s)
                .font(theme.title(level <= 1 ? .title2 : level == 2 ? .title3 : .heading, .bold))
                .padding(.top, 2)
        case .item(let marker, let depth, let s):
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text(marker)
                    .foregroundStyle(theme.muted)
                    .frame(minWidth: 14, alignment: .trailing)
                Text(s).fixedSize(horizontal: false, vertical: true)
            }
            .padding(.leading, CGFloat(depth) * 16)
        case .quote(let s):
            Text(s)
                .foregroundStyle(theme.muted)
                .padding(.leading, 10)
                .overlay(alignment: .leading) { theme.line.frame(width: 3) }
        case .code(_, let code):
            ScrollView(.horizontal) {
                Text(code)
                    .font(theme.mono(.subheadline))
                    .padding(10)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(theme.command, in: RoundedRectangle(cornerRadius: 9))
        case .rule:
            theme.line.frame(height: 1)
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

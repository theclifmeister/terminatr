import SwiftUI
import TerminatrKit
import TerminatrUI

/// A session's conversation from its transcript (transcript.watch: its
/// last items when the server pages, transcript.page for the earlier
/// ones on a tap), as the Mac's conversation tab draws it: prompts, the agent's text,
/// tool calls folded into runs (ToolRunHeader, a tap unfolds one), and
/// the open question (QuestionSummary) or approval at the end. Read
/// only: answering comes with the device link's answer level.
struct ConversationScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    let ref: SessionRef
    @State private var transcript = Transcript()
    @State private var failure: String?
    @State private var unfolded: Set<Int> = []
    @State private var paging = false
    @State private var pages = false
    /// Whether the conversation follows its end, or the user scrolled
    /// up (iOS 18, from the scroll geometry; 17 always follows).
    @State private var follow = Follow()

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
                    ForEach(Transcript.rows(transcript.entries)) { row in
                        switch row {
                        case .entry(let e): EntryLine(entry: e, agent: session?.agent ?? "")
                        case .tools(let run): runView(run)
                        }
                    }
                    if let session { waiting(session) }
                    // The foot's padding is the end row's: scrolled to,
                    // it reaches the true end (T807).
                    Color.clear.frame(height: 1).padding(.bottom, 16).id("end")
                }
                .padding([.horizontal, .top], 16)
            }
            .modifier(PhoneFollowGeometry(follow: $follow, newest: transcript.entries.last?.id))
            // New output: the follower goes to the end; scrolled up, the
            // screen stays and Jump to latest counts it.
            .onChange(of: Self.tail(transcript)) {
                if follow.arrived(transcript.entries.map(\.id)) { scroll.scrollTo("end", anchor: .bottom) }
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
        .safeAreaInset(edge: .bottom) {
            if id != nil {
                VStack(spacing: 0) {
                    // While the agent has its turn, as the Mac's (T803).
                    if let busy = Busy.of(session) {
                        WorkingBar(busy: busy, activity: Activity.of(transcript, current: session?.current ?? ""))
                            .padding(.horizontal, 16)
                            .padding(.bottom, 8)
                    }
                    Label("Read only: answer and send from your Mac for now", systemImage: "eye")
                        .font(theme.body(.footnote))
                        .foregroundStyle(theme.muted)
                        .frame(maxWidth: .infinity)
                        .padding(.vertical, 8)
                        .background(.bar)
                }
            }
        }
        .refreshable { await model.refresh() }
        .navigationTitle(model.title(ref))
        .navigationBarTitleDisplayMode(.inline)
        .backdrop()
        // A redial is a new bridge: the stream opens again on it.
        .task(id: "\(id ?? "")/\(model.generation)") { await watch(id) }
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

    /// What the session waits on: its open question, or its dialog.
    @ViewBuilder
    private func waiting(_ s: AppAPI.Session) -> some View {
        if let q = s.question {
            ForEach(QuestionPlan.shown(q), id: \.self) { i in
                QuestionSummary(menu: q, index: i, waiting: "question \(s.id) \(q.since?.timeIntervalSince1970 ?? 0) \(i)")
            }
        }
        if s.state == "blocked", s.reason == "permission" {
            CardFrame(tag: s.escape ? "Approval · outside the sandbox" : "Approval", colour: theme.approval, tint: theme.approvalTint, waiting: "approval \(s.id) \(transcript.pendingTool?.call ?? "")") {
                Text(transcript.pendingTool.map { "\($0.tool): \($0.text)" } ?? "The agent asks to go on.")
                    .font(theme.mono(.subheadline))
                    .fixedSize(horizontal: false, vertical: true)
                Text("Approve it on your Mac.")
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.muted)
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
        } else {
            content.defaultScrollAnchor(.bottom)
        }
    }
}

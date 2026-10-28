import SwiftUI
import TerminatrKit
import TerminatrUI

/// A session's conversation from its transcript (transcript.watch), as
/// the Mac's conversation tab draws it: prompts, the agent's text,
/// tool calls folded into runs (ToolRunHeader, a tap unfolds one), and
/// the open question (QuestionSummary) or approval at the end. Read
/// only: answering comes with the device link's answer level.
struct ConversationScreen: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    let ref: SessionRef
    @State private var transcript = Transcript()
    @State private var failure: String?
    @State private var unfolded: Set<Int> = []

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
                    ForEach(Transcript.rows(transcript.entries)) { row in
                        switch row {
                        case .entry(let e): EntryLine(entry: e, agent: session?.agent ?? "")
                        case .tools(let run): runView(run)
                        }
                    }
                    if let session { waiting(session) }
                    Color.clear.frame(height: 1).id("end")
                }
                .padding(16)
            }
            .onChange(of: transcript.entries.count) { scroll.scrollTo("end", anchor: .bottom) }
        }
        .safeAreaInset(edge: .bottom) {
            if id != nil {
                Label("Read only: answer and send from your Mac for now", systemImage: "eye")
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.muted)
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 8)
                    .background(.bar)
            }
        }
        .refreshable { await model.refresh() }
        .navigationTitle(model.title(ref))
        .navigationBarTitleDisplayMode(.inline)
        .backdrop()
        .task(id: id) { await watch(id) }
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
                QuestionSummary(menu: q, index: i)
            }
        }
        if s.state == "blocked", s.reason == "permission" {
            CardFrame(tag: s.escape ? "Approval · outside the sandbox" : "Approval", colour: theme.approval, tint: theme.approvalTint) {
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
        do {
            let sub = try await b.subscribe(AppAPI.Methods.transcriptWatch, AppAPI.TranscriptParams(session: id))
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

    var body: some View {
        switch entry.kind {
        case .prompt:
            HStack {
                Spacer(minLength: 40)
                VStack(alignment: .trailing, spacing: 4) {
                    turn
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

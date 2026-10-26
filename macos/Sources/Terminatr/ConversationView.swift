import AppKit
import SwiftUI
import TerminatrKit

/// The Conversation tab: the transcript, the cards for what the session
/// waits on (a question, a permission dialog), and the composer.
struct ConversationTab: View {
    @Environment(\.theme) private var theme
    let model: SessionModel
    let session: AppAPI.Session?
    let state: ConversationState

    var body: some View {
        VStack(spacing: 0) {
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 12) {
                    if model.transcript.skipped > 0 {
                        Text("\(model.transcript.skipped) earlier items not shown")
                            .font(.system(size: 11.5))
                            .foregroundStyle(theme.muted)
                            .frame(maxWidth: .infinity)
                    }
                    ForEach(model.transcript.entries) { e in
                        EntryView(entry: e, agent: session?.agent ?? "agent")
                    }
                    switch state {
                    case .loading:
                        ProgressView().frame(maxWidth: .infinity)
                    case .waiting:
                        Text("Waiting for the agent to name its transcript…")
                            .font(.system(size: 12))
                            .foregroundStyle(theme.muted)
                            .frame(maxWidth: .infinity)
                    case .unavailable(let why):
                        Text(why)
                            .font(.system(size: 12))
                            .foregroundStyle(theme.muted)
                            .frame(maxWidth: .infinity)
                    case .mapped:
                        EmptyView()
                    }
                    if let closed = model.transcript.closed {
                        Text("The session ended (\(closed)).")
                            .font(.system(size: 12))
                            .foregroundStyle(theme.muted)
                            .frame(maxWidth: .infinity)
                    }
                }
                .padding(.horizontal, 22)
                .padding(.vertical, 14)
                .frame(maxWidth: 820)
                .frame(maxWidth: .infinity)
            }
            .defaultScrollAnchor(.bottom)
            Cards(model: model, session: session)
            Composer(model: model, session: session)
        }
        .guardingLinks()
    }
}

/// One entry of the conversation.
struct EntryView: View {
    @Environment(\.theme) private var theme
    let entry: Transcript.Entry
    let agent: String

    var body: some View {
        switch entry.kind {
        case .prompt:
            HStack {
                Spacer(minLength: 80)
                VStack(alignment: .trailing, spacing: 4) {
                    Text(entry.text)
                        .textSelection(.enabled)
                        .foregroundStyle(theme.onPrompt)
                        .padding(.horizontal, 13)
                        .padding(.vertical, 9)
                        .background(LinearGradient(colors: theme.prompt, startPoint: .topLeading, endPoint: .bottomTrailing), in: RoundedRectangle(cornerRadius: 14))
                    if entry.truncated { cut }
                }
            }
        case .text:
            VStack(alignment: .leading, spacing: 6) {
                Text(agent)
                    .font(theme.mono(11, .medium))
                    .foregroundStyle(theme.muted)
                MarkdownView(text: entry.text)
                if entry.truncated { cut }
            }
            .padding(14)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(.regularMaterial, in: RoundedRectangle(cornerRadius: 14))
            .background(theme.agentCard, in: RoundedRectangle(cornerRadius: 14))
            .overlay(RoundedRectangle(cornerRadius: 14).strokeBorder(theme.edge, lineWidth: 1))
        case .tool:
            ToolLine(entry: entry)
        case .result:
            if let r = entry.result {
                Text("\(r.text)\(r.lines > 1 ? " · \(r.lines) lines" : "")")
                    .font(theme.mono(11.5))
                    .foregroundStyle(r.error ? theme.attention : theme.muted)
                    .lineLimit(1)
                    .padding(.leading, 12)
            }
        case .note:
            Text(entry.text)
                .font(.system(size: 12, weight: .medium))
                .foregroundStyle(theme.muted)
                .frame(maxWidth: .infinity)
        }
    }

    private var cut: some View {
        Text("Cut short: the rest is in the Terminal tab.")
            .font(.system(size: 11))
            .foregroundStyle(theme.muted)
    }
}

/// A tool call folded to one line: the tool, its main argument and its
/// result's first line.
struct ToolLine: View {
    @Environment(\.theme) private var theme
    let entry: Transcript.Entry

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(entry.tool.isEmpty ? "tool" : entry.tool)
                .font(theme.mono(12, .medium))
                .foregroundStyle(theme.action)
            Text(entry.text)
                .font(theme.mono(12))
                .lineLimit(1)
                .truncationMode(.middle)
                .textSelection(.enabled)
            if let r = entry.result {
                Text(summary(r))
                    .font(theme.mono(11.5))
                    .foregroundStyle(r.error ? theme.attention : theme.muted)
                    .lineLimit(1)
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
            ForEach(Array(SafeMarkdown.blocks(text).enumerated()), id: \.offset) { _, b in
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
                .font(theme.title(level <= 1 ? 18 : level == 2 ? 16 : 14.5, .bold))
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
                    .font(theme.mono(12))
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

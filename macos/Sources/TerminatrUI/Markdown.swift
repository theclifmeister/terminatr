import SwiftUI
import TerminatrKit

/// An agent's Markdown, cleaned by SafeMarkdown: no raw HTML, no
/// images, and links that open only on a click confirmed with their
/// URL (the Mac app's guardingLinks).
public struct MarkdownView: View {
    @Environment(\.theme) private var theme
    let text: String
    /// A conversation's message: its headings and tables are mono like
    /// its text, which the caller sets with `.font(theme.conversation)`.
    var conversation: Bool

    public init(text: String, conversation: Bool = false) {
        self.text = text
        self.conversation = conversation
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            ForEach(Array(SafeMarkdown.cached(text).enumerated()), id: \.offset) { _, b in
                block(b)
            }
        }
        .textSelection(.enabled)
        .tint(theme.action)
    }

    private func heading(_ level: Int) -> Font {
        let style: TextStyle = level <= 1 ? .title3 : level == 2 ? .headline : .heading
        return conversation ? theme.mono(style, .semibold) : theme.title(style, .semibold)
    }

    @ViewBuilder
    private func block(_ b: SafeMarkdown.Block) -> some View {
        switch b {
        case .paragraph(let s):
            Text(theme.coded(s)).lineSpacing(3).fixedSize(horizontal: false, vertical: true)
        case .heading(let level, let s):
            Text(theme.coded(s))
                .font(heading(level))
                .padding(.top, 2)
        case .item(let marker, let depth, let s):
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text(marker)
                    .foregroundStyle(theme.muted)
                    .frame(minWidth: 14, alignment: .trailing)
                Text(theme.coded(s)).lineSpacing(3).fixedSize(horizontal: false, vertical: true)
            }
            .padding(.leading, CGFloat(depth) * 16)
        case .quote(let s):
            Text(theme.coded(s))
                .foregroundStyle(theme.muted)
                .padding(.leading, 10)
                .overlay(alignment: .leading) { theme.line.frame(width: 2) }
        case .code(_, let code):
            ScrollView(.horizontal) {
                Text(code)
                    .font(theme.mono(.subheadline))
                    .padding(.horizontal, 13)
                    .padding(.vertical, 11)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .overlay(RoundedRectangle(cornerRadius: Radius.row).strokeBorder(theme.line, lineWidth: theme.contrast ? 1.5 : 1).allowsHitTesting(false))
        case .rule:
            theme.line.frame(height: 1)
        case .table(let t):
            MarkdownTable(table: t, conversation: conversation)
        }
    }
}

extension Theme {
    /// A Markdown line with its code spans in the shared mono: SwiftUI
    /// would draw them in the system's. A span keeps its bold or italic.
    func coded(_ s: AttributedString) -> AttributedString {
        var out = s
        for run in s.runs where run.inlinePresentationIntent?.contains(.code) == true {
            out[run.range].inlinePresentationIntent?.remove(.code)
            out[run.range].font = monoSized(Flat.conversation)
        }
        return out
    }
}

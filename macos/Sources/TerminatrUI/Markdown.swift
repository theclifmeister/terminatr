import SwiftUI
import TerminatrKit

/// An agent's Markdown, cleaned by SafeMarkdown: no raw HTML, no
/// images, and links that open only on a click confirmed with their
/// URL (the Mac app's guardingLinks).
public struct MarkdownView: View {
    @Environment(\.theme) private var theme
    let text: String

    public init(text: String) {
        self.text = text
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
        case .table(let t):
            MarkdownTable(table: t)
        }
    }
}

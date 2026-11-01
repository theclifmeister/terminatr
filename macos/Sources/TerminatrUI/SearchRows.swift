import SwiftUI
import TerminatrKit

/// A search hit as both apps list it (T924): its kind's symbol, its
/// title, then its id and day in mono, and its task, thread and folded
/// journal lines. On a timeline, at marks the chunk it is around.
public struct SearchHitRow: View {
    @Environment(\.theme) private var theme
    let hit: AppAPI.SearchHit
    let at: Bool

    public init(_ hit: AppAPI.SearchHit, at: Bool = false) {
        self.hit = hit
        self.at = at
    }

    public var body: some View {
        let kind = Search.kind(hit.kind)
        HStack(alignment: .firstTextBaseline, spacing: 10) {
            Image(systemName: kind.symbol)
                .font(theme.body(.callout))
                .foregroundStyle(at ? theme.action : theme.muted)
                .frame(width: 20)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 3) {
                Text(hit.title.isEmpty ? hit.id : hit.title)
                    .font(theme.body(.body, at ? .semibold : .medium))
                    .lineLimit(2)
                HStack(spacing: 6) {
                    Text(hit.id)
                        .foregroundStyle(theme.action)
                        .lineLimit(1)
                        .truncationMode(.middle)
                    Text(Search.day(hit.time))
                        .foregroundStyle(theme.muted)
                        .fixedSize()
                }
                .font(theme.mono(.caption))
                let facts = Search.facts(hit)
                if !facts.isEmpty {
                    Text(facts)
                        .font(theme.mono(.caption2))
                        .foregroundStyle(theme.muted)
                        .lineLimit(1)
                }
            }
            Spacer(minLength: 0)
        }
        .padding(.vertical, 3)
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
        .accessibilityLabel("\(kind.label): \(hit.title), \(hit.id), \(Search.day(hit.time))\(at ? ", this hit" : "")")
    }
}

/// A hit's header over its text: kind, id and day, then its title.
public struct SearchHitHeader: View {
    @Environment(\.theme) private var theme
    let hit: AppAPI.SearchHit

    public init(_ hit: AppAPI.SearchHit) {
        self.hit = hit
    }

    public var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 8) {
                Label(Search.kind(hit.kind).label, systemImage: Search.kind(hit.kind).symbol)
                    .font(theme.mono(.caption, .medium))
                    .padding(.horizontal, 7)
                    .padding(.vertical, 2)
                    .background(theme.cardTint, in: Capsule())
                    .overlay(Capsule().strokeBorder(theme.edge, lineWidth: 1))
                Text(hit.id)
                    .font(theme.mono(.caption, .medium))
                    .foregroundStyle(theme.action)
                    .textSelection(.enabled)
                    .lineLimit(1)
                    .truncationMode(.middle)
                Text(Search.day(hit.time))
                    .font(theme.mono(.caption))
                    .foregroundStyle(theme.muted)
            }
            Text(hit.title.isEmpty ? hit.id : hit.title)
                .font(theme.title(.title3, .heavy))
                .fixedSize(horizontal: false, vertical: true)
                .textSelection(.enabled)
            let facts = Search.facts(hit)
            if !facts.isEmpty {
                Text(facts)
                    .font(theme.mono(.footnote))
                    .foregroundStyle(theme.muted)
            }
        }
    }
}

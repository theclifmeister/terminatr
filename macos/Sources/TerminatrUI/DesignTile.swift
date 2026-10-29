import SwiftUI
import TerminatrKit

/// A design's row (T864), as a SessionTile: its StateGlyph in its tone
/// (Designs.tone), its title, its facts under it (design task, open
/// questions, answers), and at the end its status as a ToneBadge over
/// its build ("1/4 built"). Rows sit in a TileGroup, a hairline over
/// each after the first (rule).
public struct DesignTile: View {
    @Environment(\.theme) private var theme
    @Environment(\.dynamicTypeSize) private var typeSize
    let design: AppAPI.Design
    let rule: Bool
    let selected: Bool

    public init(_ design: AppAPI.Design, rule: Bool = false, selected: Bool = false) {
        self.design = design
        self.rule = rule
        self.selected = selected
    }

    public var body: some View {
        let tone = Designs.tone(design)
        Group {
            if typeSize.isAccessibilitySize {
                VStack(alignment: .leading, spacing: Spacing.item) {
                    HStack(spacing: Spacing.row) {
                        StateGlyph(tone, side: 32)
                        Spacer(minLength: 0)
                        end(tone)
                    }
                    words
                }
            } else {
                HStack(spacing: Spacing.row + 2) {
                    StateGlyph(tone, side: 32)
                    words
                    Spacer(minLength: Spacing.hair)
                    end(tone)
                }
            }
        }
        .padding(.horizontal, Spacing.card)
        .padding(.vertical, Spacing.row)
        .frame(maxWidth: .infinity, minHeight: tapTarget, alignment: .leading)
        .background(selected ? theme.wash(.working) : .clear)
        .overlay(alignment: .top) {
            if rule {
                Rectangle()
                    .fill(theme.line)
                    .frame(height: theme.contrast ? 1 : 0.5)
                    .padding(.leading, Spacing.card)
            }
        }
        .contentShape(Rectangle())
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(design.title)
        .accessibilityValue(([design.status, Designs.built(design)] + Designs.facts(design)).joined(separator: ", "))
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    private var words: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(design.title)
                .font(theme.body(.body, .semibold))
                .foregroundStyle(theme.ink)
                .lines(1)
            Text(Designs.facts(design).joined(separator: " · "))
                .font(theme.body(.footnote))
                .foregroundStyle(theme.muted)
                .lines(1)
        }
    }

    private func end(_ tone: Attention.Tone) -> some View {
        VStack(alignment: .trailing, spacing: 3) {
            ToneBadge(design.status, tone: tone, compact: true)
            Text(Designs.built(design))
                .font(theme.mono(.caption, .medium))
                .monospacedDigit()
                .foregroundStyle(theme.muted)
                .lineLimit(1)
                .fixedSize()
        }
    }
}

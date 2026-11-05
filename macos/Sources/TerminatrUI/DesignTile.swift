import SwiftUI
import TerminatrKit

/// A design's row (T864), to the design page's list (T1001, T1008): its title,
/// its facts under it (design task, open questions, answers; the user's own,
/// over the page), and at the end its build as a bar and "3 of 6", or its
/// status in words. Open
/// questions: a dot in their tone before the title. Rows sit in a
/// TileGroup, a soft line over each after the first (rule).
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

    private var quiet: Color { selected ? theme.ink : theme.muted }

    public var body: some View {
        Group {
            if typeSize.isAccessibilitySize {
                VStack(alignment: .leading, spacing: Spacing.hair) {
                    words
                    end
                }
            } else {
                HStack(spacing: Spacing.row) {
                    words
                    Spacer(minLength: Spacing.hair)
                    end
                }
            }
        }
        .padding(.horizontal, 13)
        .padding(.vertical, 7)
        .frame(maxWidth: .infinity, minHeight: Flat.listRow * theme.scale, alignment: .leading)
        .background(selected ? theme.selection : .clear)
        .overlay(alignment: .top) {
            if rule { SoftLine() }
        }
        .contentShape(Rectangle())
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(design.title)
        .accessibilityValue(([design.status, Designs.built(design)] + Designs.facts(design)).joined(separator: ", "))
        .accessibilityAddTraits(selected ? .isSelected : [])
    }

    private var words: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: Spacing.row) {
                if !design.questions.isEmpty { FlatDot(.question) }
                Text(design.title)
                    .font(theme.sized(Flat.listText))
                    .foregroundStyle(theme.ink)
                    .lines(1)
            }
            let facts = Designs.facts(design).joined(separator: " · ")
            if !facts.isEmpty {
                Text(facts)
                    .font(theme.sized(12.5))
                    .foregroundStyle(quiet)
                    .lines(1)
                    .pageLeading(12.5)
            }
        }
    }

    @ViewBuilder private var end: some View {
        if Designs.Status(design.status) == .building {
            HStack(spacing: Spacing.row) {
                Meter(fraction: Designs.progress(design))
                    .frame(width: 60 * theme.scale, height: 4)
                Text("\(design.tasksDone) of \(max(design.build.count, design.tasks.count))")
                    .font(.system(size: 12.5 * theme.scale, design: .monospaced))
                    .monospacedDigit()
                    .foregroundStyle(quiet)
                    .lineLimit(1)
                    .fixedSize()
            }
        } else {
            Text(design.status)
                .font(theme.sized(12.5))
                .foregroundStyle(quiet)
                .lineLimit(1)
                .fixedSize()
        }
    }
}

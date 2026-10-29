import SwiftUI
import TerminatrKit

/// The themes as large live previews in a row that scrolls (T850, the
/// phone's Settings): each theme's own mesh with a little deck card on
/// it, its name under it, the picked one ringed in the action colour.
/// It opens scrolled to the picked theme; picking is a selection tap.
/// At accessibility sizes the previews wrap in a grid instead.
public struct ThemeCarousel: View {
    @Environment(\.theme) private var theme
    @Environment(\.dynamicTypeSize) private var typeSize
    @Binding var selection: String

    public init(selection: Binding<String>) {
        _selection = selection
    }

    public var body: some View {
        Group {
            if typeSize.isAccessibilitySize {
                LazyVGrid(columns: [GridItem(.adaptive(minimum: 104 * min(theme.scale, 1.6)), spacing: Spacing.row)], spacing: Spacing.card) {
                    tiles
                }
                .padding(.horizontal, Spacing.gutter)
            } else {
                ScrollViewReader { scroller in
                    ScrollView(.horizontal, showsIndicators: false) {
                        HStack(alignment: .top, spacing: Spacing.row) { tiles }
                            .padding(.horizontal, Spacing.gutter)
                            .padding(.vertical, 6)
                    }
                    .onAppear { scroller.scrollTo(ThemeName.of(selection), anchor: .center) }
                }
            }
        }
        .sensoryFeedback(.selection, trigger: selection)
    }

    private var tiles: some View {
        ForEach(ThemeName.allCases, id: \.self) { t in
            ThemeTile(name: t, selected: ThemeName.of(selection) == t) { selection = t.rawValue }
                .id(t)
        }
    }
}

/// One theme in the carousel: its preview and its name.
struct ThemeTile: View {
    @Environment(\.theme) private var theme
    @Environment(\.colorSchemeContrast) private var contrast
    let name: ThemeName
    let selected: Bool
    let pick: () -> Void

    var body: some View {
        let own = contrast == .increased ? Theme.named(name).increasedContrast() : Theme.named(name)
        let shape = RoundedRectangle(cornerRadius: 20, style: .continuous)
        Button(action: pick) {
            VStack(spacing: Spacing.tight) {
                ZStack(alignment: .bottom) {
                    Backdrop()
                    MiniCard().padding(Spacing.item)
                }
                .environment(\.theme, own)
                .frame(width: 104, height: 128)
                .clipShape(shape)
                .overlay(shape.strokeBorder(theme.line, lineWidth: 1))
                .padding(3)
                .overlay(RoundedRectangle(cornerRadius: 23, style: .continuous)
                    .strokeBorder(selected ? theme.action : .clear, lineWidth: 2.5))
                Text(name.label)
                    .font(theme.body(.subheadline, selected ? .bold : .semibold))
                    .foregroundStyle(theme.ink)
                    .lines(1)
            }
            .frame(minWidth: tapTarget, minHeight: tapTarget)
            .contentShape(Rectangle())
        }
        .buttonStyle(Pressable())
        .accessibilityLabel("\(name.label): \(name.mood)")
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

/// A need in little, on a theme's mesh: an action pill and two lines.
private struct MiniCard: View {
    @Environment(\.theme) private var theme

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            Capsule().fill(theme.action).frame(height: 14)
            Capsule().fill(theme.muted.opacity(0.6)).frame(height: 5)
            Capsule().fill(theme.muted.opacity(0.6)).frame(width: 50, height: 5)
        }
        .padding(7)
        .surface(.card, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        .accessibilityHidden(true)
    }
}

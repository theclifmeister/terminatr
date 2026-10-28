import SwiftUI
import TerminatrKit

/// The themes as swatches, each a small window of its own: the mesh, a
/// card with a title and a muted line, a filled pill and the status
/// dots, drawn in the appearance Settings is in. The Mac's Settings ›
/// Appearance shows four in a row, the phone's Settings three.
public struct ThemePicker: View {
    @Binding var selection: String
    var columns = 4

    public init(selection: Binding<String>, columns: Int = 4) {
        _selection = selection
        self.columns = columns
    }

    public var body: some View {
        LazyVGrid(columns: Array(repeating: GridItem(.flexible(), spacing: 10), count: columns), spacing: 12) {
            ForEach(ThemeName.allCases, id: \.self) { t in
                ThemeSwatch(name: t, selected: ThemeName.of(selection) == t) { selection = t.rawValue }
            }
        }
        .padding(.vertical, 4)
    }
}

public struct ThemeSwatch: View {
    let name: ThemeName
    let selected: Bool
    let pick: () -> Void
    @Environment(\.colorSchemeContrast) private var contrast

    public init(name: ThemeName, selected: Bool, pick: @escaping () -> Void) {
        self.name = name
        self.selected = selected
        self.pick = pick
    }

    public var body: some View {
        let theme = contrast == .increased ? Theme.named(name).increasedContrast() : Theme.named(name)
        Button(action: pick) {
            VStack(spacing: 5) {
                ThemePreview()
                    .environment(\.theme, theme)
                    .frame(height: 64)
                    .clipShape(RoundedRectangle(cornerRadius: 9))
                    .overlay(RoundedRectangle(cornerRadius: 9).strokeBorder(Color.primary.opacity(0.15), lineWidth: 1))
                    .padding(3)
                    .overlay(RoundedRectangle(cornerRadius: 12).strokeBorder(selected ? AnyShapeStyle(.tint) : AnyShapeStyle(.clear), lineWidth: 2.5))
                Text(name.label)
                    .font(.callout.weight(selected ? .semibold : .regular))
                    .lineLimit(1)
                    .minimumScaleFactor(0.6)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help(name.mood)
        .accessibilityLabel("\(name.label): \(name.mood)")
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

/// A theme in little: its mesh, a frosted card with a title and a muted
/// line, a pill and the dots.
public struct ThemePreview: View {
    @Environment(\.theme) private var theme

    public init() {}

    public var body: some View {
        ZStack {
            Backdrop()
            VStack(alignment: .leading, spacing: 3) {
                Text("Aa")
                    .font(theme.title(.headline, .heavy))
                    .foregroundStyle(theme.ink)
                theme.muted.frame(width: 40, height: 3).clipShape(Capsule())
                HStack(spacing: 4) {
                    Text("run")
                        .font(.system(size: 8, weight: .semibold))
                        .foregroundStyle(theme.onAction)
                        .padding(.horizontal, 5)
                        .padding(.vertical, 1)
                        .background(theme.action, in: Capsule())
                    ForEach(Array([theme.good, theme.attention, theme.approval].enumerated()), id: \.0) { _, c in
                        Circle().fill(c).frame(width: 6, height: 6)
                    }
                }
            }
            .padding(7)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(theme.cardTint, in: RoundedRectangle(cornerRadius: 7))
            .background(theme.glass, in: RoundedRectangle(cornerRadius: 7))
            .overlay(RoundedRectangle(cornerRadius: 7).strokeBorder(theme.edge, lineWidth: 1))
            .padding(9)
        }
        .accessibilityHidden(true)
    }
}

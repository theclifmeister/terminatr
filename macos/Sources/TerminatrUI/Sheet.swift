import SwiftUI

/// A sheet's header on the phone (T850: Settings, pairing), in place of
/// the inline navigation bar: an optional leading button, the title in
/// the display face, and an optional trailing one. On the sheet's glass.
/// At accessibility sizes the buttons go above the title, which then
/// has the row to itself.
public struct SheetHeader<Leading: View, Trailing: View>: View {
    @Environment(\.theme) private var theme
    @Environment(\.dynamicTypeSize) private var typeSize
    let title: String
    let leading: Leading
    let trailing: Trailing

    public init(_ title: String, @ViewBuilder leading: () -> Leading, @ViewBuilder trailing: () -> Trailing) {
        self.title = title
        self.leading = leading()
        self.trailing = trailing()
    }

    public var body: some View {
        Group {
            if typeSize.isAccessibilitySize {
                VStack(alignment: .leading, spacing: Spacing.item) {
                    HStack(spacing: Spacing.item) {
                        leading
                        Spacer(minLength: 0)
                        trailing
                    }
                    heading
                }
            } else {
                HStack(alignment: .center, spacing: Spacing.item) {
                    leading
                    heading
                    trailing
                }
                .frame(minHeight: tapTarget)
            }
        }
        .padding(.horizontal, Spacing.gutter)
        .padding(.top, Spacing.gutter)
        .padding(.bottom, Spacing.tight)
    }
}

extension SheetHeader {
    private var heading: some View {
        Text(title)
            .font(theme.title(.display, .heavy))
            .tracking(-0.5)
            .foregroundStyle(theme.ink)
            .lines(1)
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityAddTraits(.isHeader)
    }
}

extension SheetHeader where Leading == EmptyView {
    public init(_ title: String, @ViewBuilder trailing: () -> Trailing) {
        self.init(title, leading: { EmptyView() }, trailing: trailing)
    }
}

extension SheetHeader where Trailing == EmptyView {
    public init(_ title: String, @ViewBuilder leading: () -> Leading) {
        self.init(title, leading: leading, trailing: { EmptyView() })
    }
}

/// A group's label on a sheet: mono, upper case, muted, on the glass.
public struct GroupLabel: View {
    @Environment(\.theme) private var theme
    let text: String

    public init(_ text: String) {
        self.text = text
    }

    public var body: some View {
        Text(text.uppercased())
            .font(theme.mono(.caption, .medium))
            .tracking(0.8)
            .foregroundStyle(theme.muted)
            .padding(.horizontal, Spacing.gutter + 6)
            .padding(.top, Spacing.item)
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityAddTraits(.isHeader)
    }
}

extension View {
    /// A sheet's ground (T850): the theme's mesh under the glass, edge to
    /// edge ("the glass" in ThemeColours.checks; its cards are "a sheet
    /// card").
    public func sheetGlass() -> some View {
        background {
            Backdrop()
                .overlay { Color.clear.surface(.glass, in: Rectangle()).ignoresSafeArea() }
        }
    }
}

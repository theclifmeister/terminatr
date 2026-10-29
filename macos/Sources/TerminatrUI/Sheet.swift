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

/// A setting's switch on a sheet (T851: Settings' Haptics): its title
/// and a line of what it does, the switch in the action colour, on a
/// deck card ("a sheet card"). VoiceOver reads it as the one switch.
public struct SwitchCard: View {
    @Environment(\.theme) private var theme
    let title: String
    let detail: String
    @Binding var isOn: Bool

    public init(_ title: String, detail: String = "", isOn: Binding<Bool>) {
        self.title = title
        self.detail = detail
        _isOn = isOn
    }

    public var body: some View {
        Toggle(isOn: $isOn) {
            VStack(alignment: .leading, spacing: 2) {
                Text(title)
                    .font(theme.body(.headline, .semibold))
                    .foregroundStyle(theme.ink)
                if !detail.isEmpty {
                    Text(detail)
                        .font(theme.body(.subheadline))
                        .foregroundStyle(theme.muted)
                        .fixedSize(horizontal: false, vertical: true)
                }
            }
        }
        .tint(theme.action)
        .padding(.horizontal, Spacing.card)
        .padding(.vertical, Spacing.row)
        .frame(minHeight: tapTarget)
        .surface(.card, in: RoundedRectangle(cornerRadius: Radius.tile, style: .continuous))
        .padding(.horizontal, Spacing.gutter)
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

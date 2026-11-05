import SwiftUI
import TerminatrKit
import TerminatrUI

/// A page of flat panels (T1001), in place of a List, whose grouped
/// sections iOS rounds itself: rows in a PanelList under a SectionHeading.
struct PanelPage<Content: View>: View {
    @ViewBuilder let content: Content

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Spacing.row) {
                content
            }
            .padding(.horizontal, Spacing.gutter)
            .padding(.vertical, Spacing.row + 2)
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

/// A group's rows: 8 round with its line, a soft line between them.
struct PanelList<Content: View>: View {
    @ViewBuilder let content: Content

    var body: some View {
        HairlineList(radius: Radius.tile) { content }
    }
}

/// A PanelList's row that opens route, as a tap on it: a chevron at its
/// end, on the selection's fill while it is open beside the list.
struct RowLink<Label: View>: View {
    @Environment(\.navigate) private var navigate
    @Environment(PhoneModel.self) private var model
    @Environment(\.theme) private var theme
    let value: Route
    @ViewBuilder let label: Label

    init(value: Route, @ViewBuilder label: () -> Label) {
        self.value = value
        self.label = label()
    }

    var body: some View {
        PlainRouteLink(value: value) {
            ListRow(selected: navigate?.shows(value.on(model.key)) == true) {
                HStack(spacing: Spacing.item) {
                    label
                    Spacer(minLength: 0)
                    Image(systemName: "chevron.right")
                        .font(.system(size: 12 * theme.scale, weight: .semibold))
                        .foregroundStyle(theme.muted)
                        .accessibilityHidden(true)
                }
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}

/// A PanelList's row that folds what follows it: its title and a chevron,
/// turned while open.
struct FoldRow: View {
    @Environment(\.theme) private var theme
    @Environment(\.accessibilityReduceMotion) private var still
    let title: String
    @Binding var open: Bool

    var body: some View {
        Button {
            withAnimation(still ? nil : .snappy(duration: 0.2)) { open.toggle() }
        } label: {
            ListRow {
                HStack(spacing: Spacing.item) {
                    Text(title)
                        .foregroundStyle(theme.ink)
                    Spacer(minLength: 0)
                    Image(systemName: "chevron.right")
                        .font(.system(size: 12 * theme.scale, weight: .semibold))
                        .foregroundStyle(theme.muted)
                        .rotationEffect(.degrees(open ? 90 : 0))
                }
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(title)
        .accessibilityValue(open ? "expanded" : "collapsed")
    }
}

/// A filter's field under a page's bar (T1001): in the screen, as the
/// system's search bar needs the navigation bar, and on an open iPhone Duo
/// went to its rail (T928). On the window's colour, a line under it.
struct FilterField: View {
    @Environment(\.theme) private var theme
    let prompt: String
    @Binding var text: String

    var body: some View {
        HStack(spacing: Spacing.item) {
            Image(systemName: "magnifyingglass")
                .font(.system(size: 13 * theme.scale))
                .foregroundStyle(theme.muted)
                .accessibilityHidden(true)
            TextField(prompt, text: $text)
                .font(theme.sized(Flat.text))
                .autocorrectionDisabled()
                .textInputAutocapitalization(.never)
                .submitLabel(.search)
            if !text.isEmpty {
                Button("Clear", systemImage: "xmark.circle.fill") { text = "" }
                    .labelStyle(.iconOnly)
                    .buttonStyle(.plain)
                    .foregroundStyle(theme.muted)
            }
        }
        .padding(.horizontal, 11)
        .frame(minHeight: 38 * theme.scale)
        .background(theme.pane, in: RoundedRectangle(cornerRadius: Radius.row))
        .overlay { RoundedRectangle(cornerRadius: Radius.row).strokeBorder(theme.line, lineWidth: theme.contrast ? 1.5 : 1) }
        .padding(.horizontal, Spacing.gutter)
        .padding(.vertical, Spacing.item)
        .background(theme.chrome)
        .overlay(alignment: .bottom) { theme.line.frame(height: 1) }
    }
}

import SwiftUI
import TerminatrKit
import TerminatrUI

/// The theme for the bar, the rail and the sidebar (T1005): their text
/// grows with Dynamic Type to a point (the system's tab bar's never did), and
/// past it a long press on a bar's or rail's item shows it in the large
/// content viewer. Put outside the view, which reads the theme it's given.
private struct NavScale: ViewModifier {
    @Environment(\.theme) private var theme
    let most: CGFloat

    func body(content: Content) -> some View {
        var capped = theme
        capped.scale = min(theme.scale, most)
        return content.environment(\.theme, capped)
    }
}

extension View {
    /// The bar's and rail's limit on Dynamic Type.
    func navScale(most: CGFloat = 1.25) -> some View {
        modifier(NavScale(most: most))
    }
}

/// What waits, in the attention colour (`.cnt`).
private struct NavCount: View {
    @Environment(\.theme) private var theme
    let count: Int
    let size: CGFloat
    let font: CGFloat

    var body: some View {
        Text(count, format: .number)
            .font(theme.sized(font, .semibold))
            .monospacedDigit()
            .foregroundStyle(theme.onAttention)
            .lineLimit(1)
            .fixedSize()
            .padding(.horizontal, 5)
            .frame(minWidth: size * theme.scale, minHeight: size * theme.scale)
            .background(theme.attention, in: Ellipse())
    }
}

/// VoiceOver's reading and the large content viewer's of a bar or rail item.
private extension View {
    func navItem(_ page: TabPage, selected: Bool, count: Int) -> some View {
        accessibilityElement(children: .ignore)
            .accessibilityLabel(page.title)
            .accessibilityValue(count > 0 ? "\(count)" : "")
            .accessibilityAddTraits(selected ? [.isButton, .isSelected] : .isButton)
            .accessibilityIdentifier("nav." + page.title)
            .accessibilityShowsLargeContentViewer {
                Label(page.title, systemImage: page.symbol)
            }
    }
}

/// The iPhone's bar: flat, with a line over it (the design page's `.tabbar`).
struct FlatBar: View {
    @Environment(\.theme) private var theme
    let pages: [TabPage]
    let selected: TabPage?
    let count: Int
    let choose: (TabPage) -> Void
    /// What the bar leaves below it: the home indicator's.
    @State private var below: CGFloat = 0

    var body: some View {
        HStack(spacing: 0) {
            ForEach(pages, id: \.self) { p in
                Button { choose(p) } label: { item(p) }
                    .buttonStyle(.plain)
                    .navItem(p, selected: p == selected, count: p == .needs ? count : 0)
            }
        }
        .padding(.top, 5)
        // 78 with a 34 home indicator (`.tabbar`): the words run 10 into it.
        .padding(.bottom, below >= 10 ? -10 : 0)
        .frame(maxWidth: .infinity)
        .onGeometryChange(for: CGFloat.self) { $0.safeAreaInsets.bottom } action: { below = $0 }
        // Past the safe area both ways: a closed Duo keeps a band beside
        // the sensor, and the bar's colour and line run under it.
        .background(alignment: .top) {
            theme.chrome
                .overlay(alignment: .top) { theme.line.frame(height: 1) }
                .ignoresSafeArea(edges: [.bottom, .horizontal])
        }
    }

    private func item(_ p: TabPage) -> some View {
        let on = p == selected
        return VStack(spacing: 3) {
            FlatIcon(p.glyph, side: 20)
                .overlay(alignment: .topLeading) {
                    if p == .needs, count > 0 {
                        NavCount(count: count, size: 16, font: 10)
                            .fixedSize()
                            .offset(x: 16 * theme.scale, y: -4)
                    }
                }
            Text(p.title)
                .font(theme.sized(11))
                .lineLimit(1)
                .minimumScaleFactor(0.8)
                .pageLeading(11)
        }
        .foregroundStyle(on ? theme.ink : theme.muted)
        .padding(.vertical, 5)
        .frame(maxWidth: .infinity)
        // The selection's fill is the user's own, over the page's plain
        // item; each place keeps the page's third of the width.
        .background(on ? theme.selection : .clear, in: RoundedRectangle(cornerRadius: Radius.card))
        .padding(.horizontal, Spacing.hair)
        .contentShape(Rectangle())
    }
}

/// The rail: flat, with a line down its inner edge (the design page's
/// `.rail`). A phone's runs down the trailing edge with its items at the
/// bottom; the iPad's, the sidebar folded, down the leading edge from the
/// top, under the button that opens the sidebar.
struct FlatRail: View {
    @Environment(\.theme) private var theme
    let pages: [TabPage]
    let selected: TabPage?
    let count: Int
    /// Opens the iPad's sidebar.
    var openSidebar: (() -> Void)?
    let choose: (TabPage) -> Void
    /// What the rail leaves below it: the home indicator's.
    @State private var below: CGFloat = 0

    var body: some View {
        VStack(spacing: Spacing.tight) {
            if let openSidebar {
                Button("Show the sidebar", systemImage: "sidebar.leading", action: openSidebar)
                    .labelStyle(.iconOnly)
                    .buttonStyle(.plain)
                    .font(theme.sized(19))
                    .foregroundStyle(theme.muted)
                    .frame(width: 40, height: 40)
                    .contentShape(Rectangle())
                    .accessibilityIdentifier("nav.sidebar.open")
                    .padding(.top, Spacing.item)
                    .padding(.bottom, Spacing.item)
            } else {
                Spacer(minLength: 0)
            }
            ForEach(pages, id: \.self) { p in
                Button { choose(p) } label: { item(p) }
                    .buttonStyle(.plain)
                    .navItem(p, selected: p == selected, count: p == .needs ? count : 0)
            }
            if openSidebar != nil { Spacer(minLength: 0) }
        }
        // 26 from the screen's foot, 64 wide with its line (`.rail`).
        .padding(.bottom, openSidebar == nil ? max(26 - below, 0) : 0)
        .frame(width: NavChrome.railWidth - 1)
        .frame(maxHeight: .infinity)
        .background(theme.chrome.ignoresSafeArea())
        .onGeometryChange(for: CGFloat.self) { $0.safeAreaInsets.bottom } action: { below = $0 }
    }

    private func item(_ p: TabPage) -> some View {
        let on = p == selected
        return FlatIcon(p.glyph, side: 19)
            .foregroundStyle(on ? theme.ink : theme.muted)
            .frame(width: 40, height: 40)
            .background(on ? theme.selection : .clear, in: RoundedRectangle(cornerRadius: Radius.card))
            .overlay(alignment: .topTrailing) {
                if p == .needs, count > 0 {
                    NavCount(count: count, size: 17, font: 10.5)
                        .offset(x: 3, y: -3)
                }
            }
            .contentShape(Rectangle())
    }
}

/// The iPad's sidebar (T849, T1005): the wordmark, Needs you, Projects and
/// Search, then each section's rows (Phone.sidebar), which fold by the
/// section's chevron; a program member's threads fold by the row under it.
/// A section's fold is kept across launches, as a member's is (Navigation).
struct FlatSidebar: View {
    @Environment(Phone.self) private var phone
    @Environment(\.theme) private var theme
    @AppStorage("sidebar.closed") private var closed = ""
    let pages: [TabPage]
    let selected: RootTab
    let count: Int
    let hide: () -> Void
    let choose: (RootTab) -> Void

    private var closedSections: Set<String> {
        Set(closed.split(separator: "\n").map(String.init))
    }

    private func flip(_ id: String) {
        var s = closedSections
        if s.remove(id) == nil { s.insert(id) }
        closed = s.sorted().joined(separator: "\n")
    }

    var body: some View {
        VStack(spacing: 0) {
            HStack(alignment: .top) {
                SidebarHead()
                Button("Hide the sidebar", systemImage: "sidebar.leading", action: hide)
                    .labelStyle(.iconOnly)
                    .buttonStyle(.plain)
                    .font(theme.sized(19))
                    .foregroundStyle(theme.muted)
                    .frame(width: 40, height: 40)
                    .contentShape(Rectangle())
                    .accessibilityIdentifier("nav.sidebar.close")
            }
            .padding(.leading, Spacing.gutter)
            .padding(.trailing, Spacing.item)
            .padding(.top, Spacing.item)
            .padding(.bottom, Spacing.row)
            ScrollView {
                VStack(alignment: .leading, spacing: 2) {
                    ForEach(pages, id: \.self) { p in
                        placeRow(p)
                    }
                    ForEach(phone.sidebar) { s in
                        section(s)
                    }
                }
                .padding(.horizontal, Spacing.item)
                .padding(.bottom, Spacing.gutter)
            }
        }
        .frame(width: NavChrome.sidebarWidth(scale: theme.scale))
        .background(theme.chrome.ignoresSafeArea())
    }

    private func placeRow(_ p: TabPage) -> some View {
        let on = selected == p.tab
        return Button { choose(p.tab) } label: {
            rowBody(on: on) {
                FlatIcon(p.glyph, side: 18).foregroundStyle(on ? theme.ink : theme.muted)
            } title: {
                Text(p.title)
            } trailing: {
                if p == .needs, count > 0 { NavCount(count: count, size: 20, font: 12) }
            }
        }
        .buttonStyle(.plain)
        .hoverEffect()
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(p.title)
        .accessibilityValue(p == .needs && count > 0 ? "\(count)" : "")
        .accessibilityAddTraits(on ? [.isButton, .isSelected] : .isButton)
        .accessibilityIdentifier("nav." + p.title)
    }

    @ViewBuilder private func section(_ s: SidebarGroup) -> some View {
        let open = !closedSections.contains(s.id)
        Button {
            withAnimation(.snappy) { flip(s.id) }
        } label: {
            HStack {
                Text(s.title)
                    .font(theme.sized(Flat.label, .medium))
                    .foregroundStyle(theme.muted)
                    .lines(1)
                Spacer(minLength: 0)
                Image(systemName: "chevron.right")
                    .font(theme.sized(11, .semibold))
                    .foregroundStyle(theme.muted)
                    .rotationEffect(.degrees(open ? 90 : 0))
            }
            .padding(.horizontal, Spacing.item)
            .frame(minHeight: 36 * theme.scale)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .padding(.top, Spacing.tight)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(s.title)
        .accessibilityValue(open ? "Open" : "Folded")
        .accessibilityAddTraits([.isHeader, .isButton])
        if open {
            ForEach(s.rows) { r in
                row(r)
                if let f = r.fold { foldRow(f, of: r) }
            }
        }
    }

    private func row(_ r: SidebarRow) -> some View {
        let on = selected == .place(r.route)
        return Button { choose(.place(r.route)) } label: {
            rowBody(on: on) {
                Image(systemName: r.role?.symbol ?? r.tone.symbol)
                    .foregroundStyle(r.role != nil && r.tone == .idle ? theme.ink : theme.colour(r.tone))
            } title: {
                Text(r.title)
            } trailing: {
                if let b = r.badge {
                    if let n = Int(b) {
                        NavCount(count: n, size: 20, font: 12)
                    } else {
                        Text(b)
                            .font(theme.sized(Flat.label))
                            .foregroundStyle(on ? theme.ink : theme.muted)
                            .lines(1)
                    }
                }
            }
        }
        .buttonStyle(.plain)
        .hoverEffect()
        .contextMenu {
            if let f = r.fold {
                Button(f.open ? "Fold" : "Unfold", systemImage: f.open ? "chevron.up" : "chevron.down") { phone.model(f.server)?.toggle(f) }
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(r.title)
        .accessibilityValue(r.badge ?? "")
        .accessibilityAddTraits(on ? [.isButton, .isSelected] : .isButton)
    }

    private func foldRow(_ f: SidebarRow.Fold, of r: SidebarRow) -> some View {
        Button {
            phone.model(f.server)?.toggle(f)
        } label: {
            rowBody(on: false) {
                Image(systemName: f.open ? "chevron.down" : "chevron.right")
                    .font(theme.sized(12, .semibold))
                    .foregroundStyle(theme.muted)
            } title: {
                Text(f.open ? "Hide threads" : "Show threads").foregroundStyle(theme.muted)
            } trailing: {
                EmptyView()
            }
        }
        .buttonStyle(.plain)
        .hoverEffect()
        .accessibilityHint(r.title)
    }

    /// A row: 44 high for a finger, 6 radius and the selection fill as the
    /// Mac's; its icon in a fixed column, so the titles line up.
    private func rowBody<Icon: View, Title: View, Trailing: View>(
        on: Bool,
        @ViewBuilder icon: () -> Icon,
        @ViewBuilder title: () -> Title,
        @ViewBuilder trailing: () -> Trailing
    ) -> some View {
        HStack(spacing: Spacing.row) {
            icon()
                .font(theme.sized(15))
                .frame(width: 22 * theme.scale)
            title()
                .font(theme.sized(14.5, on ? .medium : .regular))
                .foregroundStyle(theme.ink)
                .lines(1)
            Spacer(minLength: 0)
            trailing()
        }
        .padding(.horizontal, Spacing.item)
        .frame(maxWidth: .infinity, minHeight: 44, alignment: .leading)
        .background(on ? theme.selection : .clear, in: RoundedRectangle(cornerRadius: Radius.row))
        .contentShape(RoundedRectangle(cornerRadius: Radius.row))
    }
}

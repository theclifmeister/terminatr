import SwiftUI
import TerminatrKit
import TerminatrUI

/// A tab's screens (T825): in compact width (an iPhone, a closed iPhone
/// Duo) the stack, as ever; in regular width (an open iPhone Duo's inner
/// display, an iPad) two columns, a list and what it opened (Columns).
/// Both lay out the one path, so opening or closing the phone keeps the
/// screen at hand and the row it came from; what each screen holds
/// beyond that is kept in Places.
struct TabStack<Content: View>: View {
    @Environment(PhoneModel.self) private var model
    @Environment(\.horizontalSizeClass) private var width
    @Binding var path: [Route]
    /// The tab's list, for Places.chosen.
    let key: String
    /// What the detail column says while the list has opened nothing.
    let empty: (String, String)
    @ViewBuilder let root: () -> Content

    var body: some View {
        Group {
            if width == .regular {
                Columns(path: $path, empty: empty, root: root)
            } else {
                NavigationStack(path: $path) {
                    root().destinations()
                }
            }
        }
        .onChange(of: path, initial: true) { model.places.choose(path, root: key) }
    }
}

/// The path's last two screens side by side: the list (the tab's own,
/// or the route before the last) and the route it opened. A link in the
/// list opens beside it; a link in the detail moves the pair on, the
/// detail becoming the list, and Back moves it back. On iOS 27.1 an
/// ArrangementView splits them (.split). While the screen is partly
/// folded (an iPhone Duo's division region is active) it picks the axis
/// itself and keeps both off the fold. Flat and wider than tall, they
/// stay side by side (.axes(.horizontal)). Flat and taller than wide
/// (an open iPhone Duo held in portrait, an iPad in portrait) the
/// ArrangementView stacks them anyway, axes or not, and the list takes
/// the whole height: the detail was never shown, so a tap opened
/// nothing (T841). There, and before 27.1, the list sits beside the
/// detail in an HStack (beside); on an open iPhone Duo it ends at the
/// hinge, which the region of the fold, inactive while flat, marks
/// (PhoneColumns.split, T849). A coordinator or thread open there, flat
/// and in portrait or narrow (PhoneColumns.foldsList), takes the whole
/// width instead, the list folded away (T855): the bar's list button,
/// or a swipe in from the leading edge, slides it back over the session.
struct Columns<Content: View>: View {
    @Environment(\.theme) private var theme
    @Binding var path: [Route]
    let empty: (String, String)
    let root: () -> Content
    /// The folded list slid back over the session.
    @State private var listShown = false
    /// The space's size without the keyboard: the layout is picked by
    /// it, so the keyboard coming up in an iPad's portrait (less tall
    /// than wide then) doesn't swap the layout, which built the session
    /// afresh, dropping the keyboard, again and again (T893).
    @State private var whole: CGSize?

    /// The list column's width when the layout leaves it to us.
    static var listWidth: CGFloat { PhoneColumns.listIdeal }

    var body: some View {
        GeometryReader { g in
            let size = whole ?? g.size
            if #available(iOS 27.1, *) {
                let folds = g.reservedRegions(kind: .division).contains { $0.isActive }
                let hinge = g.reservedRegions(kind: .division, options: .includeInactive).first?.frame
                let (list, gap) = PhoneColumns.split(g.size.width, hinge: hinge)
                if !folds, foldsList(size) {
                    folded(list: list)
                } else if folds || size.width > size.height {
                    ArrangementView {
                        lead.splitArrangementLayoutSize(minWidth: 300, idealWidth: Self.listWidth, maxWidth: 440)
                    } secondary: {
                        trail()
                    }
                    .arrangementViewStyle(folds ? .split : .split.axes(.horizontal))
                } else {
                    beside(list: list, gap: gap)
                        .environment(\.splitArrangementAxis, .horizontal)
                }
            } else if foldsList(size) {
                folded(list: min(Self.listWidth, g.size.width / 2))
            } else {
                beside(list: Self.listWidth)
            }
        }
        .background {
            GeometryReader { w in
                Color.clear
                    .onAppear { whole = w.size }
                    .onChange(of: w.size) { _, now in whole = now }
            }
            .ignoresSafeArea(.keyboard)
        }
        .onChange(of: path) { listShown = false }
    }

    /// Whether the detail is a coordinator or thread that takes the
    /// whole of a space this size.
    private func foldsList(_ size: CGSize) -> Bool {
        switch path.last {
        case .session, .need: PhoneColumns.foldsList(size)
        default: false
        }
    }

    private var lead: some View {
        let n = path.count
        let list = n >= 2 ? path[n - 2] : nil
        return NavigationStack {
            Group {
                if let list { RouteScreen(route: list) } else { root() }
            }
            .modifier(InnerEdge(edge: .trailing))
            .toolbar {
                if n >= 2 {
                    ToolbarItem(placement: .topBarLeading) {
                        Button("Back", systemImage: "chevron.left") { path.removeLast() }
                            .accessibilityHint("Shows the list before this one")
                    }
                }
            }
            .destinations()
        }
        .environment(\.navigate, Navigate(open: path.last) { r in path = Array(path.prefix(max(n - 1, 0))) + [r] })
        .id(list)
    }

    /// The detail column; folded, its bar leads with the list button.
    private func trail(folded: Bool = false) -> some View {
        let detail = path.last
        return NavigationStack {
            Group {
                if let detail {
                    RouteScreen(route: detail)
                } else {
                    ContentUnavailableView(empty.0, systemImage: empty.1)
                        .backdrop()
                }
            }
            .modifier(InnerEdge(edge: .leading))
            .toolbar {
                if folded {
                    ToolbarItem(placement: .topBarLeading) {
                        Button("List", systemImage: "list.bullet") { show(!listShown) }
                            .accessibilityLabel(listShown ? "Hide the list" : "Show the list")
                    }
                }
            }
            .destinations()
        }
        .environment(\.navigate, Navigate(open: nil) { path.append($0) })
        .id(detail)
    }

    /// The list column, list points wide, beside the detail, gap points
    /// apart (the hinge, else a hairline).
    private func beside(list: CGFloat, gap: CGFloat = 1) -> some View {
        HStack(spacing: 0) {
            lead.frame(width: list)
            Rectangle().fill(theme.edge).frame(width: gap).ignoresSafeArea()
            trail()
        }
    }

    /// The detail across the whole width, the list folded away; shown,
    /// it slides in over the detail, list points wide, and a tap beside
    /// it or a swipe back folds it again.
    private func folded(list: CGFloat) -> some View {
        ZStack(alignment: .leading) {
            trail(folded: true)
            if listShown {
                Color.black.opacity(0.2)
                    .ignoresSafeArea()
                    .onTapGesture { show(false) }
                    .accessibilityHidden(true)
                    .transition(.opacity)
                HStack(spacing: 0) {
                    lead.frame(width: list)
                    Rectangle().fill(theme.edge).frame(width: 1).ignoresSafeArea()
                }
                .shadow(color: .black.opacity(0.2), radius: 12)
                .transition(.move(edge: .leading))
            }
        }
        .simultaneousGesture(
            DragGesture(minimumDistance: 20).onEnded { v in
                if !listShown, v.startLocation.x < 24, v.translation.width > 60 {
                    show(true)
                } else if listShown, v.translation.width < -60 {
                    show(false)
                }
            }
        )
    }

    private func show(_ shown: Bool) {
        withAnimation(.snappy) { listShown = shown }
    }
}

/// A column's screen lays out to its inner edge, the one it shares with
/// the other column side by side (T837). A NavigationStack gives its
/// screen the window's side insets whole, wherever it sits: on a
/// landscape iPhone Duo the sensor's 84 pt, so the list column's list
/// and sections stopped that far short of the column, and turned the
/// other way the detail's started that far into it. Stacked, or before
/// iOS 27.1 (no splitArrangementAxis), every edge may be the screen's
/// and the insets hold. Beside in an HStack on 27.1, Columns sets the
/// axis itself.
private struct InnerEdge: ViewModifier {
    let edge: Edge.Set

    func body(content: Content) -> some View {
        if #available(iOS 27.1, *) {
            content.modifier(Arranged(edge: edge))
        } else {
            content
        }
    }

    @available(iOS 27.1, *)
    private struct Arranged: ViewModifier {
        @Environment(\.splitArrangementAxis) private var axis
        let edge: Edge.Set

        func body(content: Content) -> some View {
            content.ignoresSafeArea(.container, edges: axis == .horizontal ? edge : [])
        }
    }
}

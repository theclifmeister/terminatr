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
/// detail in an HStack (beside).
struct Columns<Content: View>: View {
    @Environment(\.theme) private var theme
    @Binding var path: [Route]
    let empty: (String, String)
    let root: () -> Content

    /// The list column's width when the layout leaves it to us.
    static var listWidth: CGFloat { 360 }

    var body: some View {
        let n = path.count
        let list = n >= 2 ? path[n - 2] : nil
        let detail = path.last
        let lead = NavigationStack {
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
        .environment(\.navigate, Navigate(open: detail) { r in path = Array(path.prefix(max(n - 1, 0))) + [r] })
        .id(list)
        let trail = NavigationStack {
            Group {
                if let detail {
                    RouteScreen(route: detail)
                } else {
                    ContentUnavailableView(empty.0, systemImage: empty.1)
                        .backdrop()
                }
            }
            .modifier(InnerEdge(edge: .leading))
            .destinations()
        }
        .environment(\.navigate, Navigate(open: nil) { path.append($0) })
        .id(detail)
        if #available(iOS 27.1, *) {
            GeometryReader { g in
                let folds = g.reservedRegions(kind: .division).contains { $0.isActive }
                if folds || g.size.width > g.size.height {
                    ArrangementView {
                        lead.splitArrangementLayoutSize(minWidth: 300, idealWidth: Self.listWidth, maxWidth: 440)
                    } secondary: {
                        trail
                    }
                    .arrangementViewStyle(folds ? .split : .split.axes(.horizontal))
                } else {
                    // Half the width at most: an open Duo's 585 pt leaves
                    // the detail as much room as the list.
                    beside(lead, trail, list: min(Self.listWidth, g.size.width / 2))
                        .environment(\.splitArrangementAxis, .horizontal)
                }
            }
        } else {
            beside(lead, trail, list: Self.listWidth)
        }
    }

    /// The list column, list points wide, beside the detail.
    private func beside(_ lead: some View, _ trail: some View, list: CGFloat) -> some View {
        HStack(spacing: 0) {
            lead.frame(width: list)
            Rectangle().fill(theme.edge).frame(width: 1).ignoresSafeArea()
            trail
        }
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

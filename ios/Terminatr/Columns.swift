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
/// itself and keeps both off the fold. Flat, or with no fold, they stay
/// side by side (.axes(.horizontal)): the Duo's inner display is
/// portrait, taller than wide, and would otherwise stack them. Before
/// 27.1, a fixed list beside the detail (an iPad).
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
            .destinations()
        }
        .environment(\.navigate, Navigate(open: nil) { path.append($0) })
        .id(detail)
        if #available(iOS 27.1, *) {
            GeometryReader { g in
                let folds = g.reservedRegions(kind: .division).contains { $0.isActive }
                ArrangementView {
                    lead.splitArrangementLayoutSize(minWidth: 300, idealWidth: Self.listWidth, maxWidth: 440)
                } secondary: {
                    trail
                }
                .arrangementViewStyle(folds ? .split : .split.axes(.horizontal))
            }
        } else {
            HStack(spacing: 0) {
                lead.frame(width: Self.listWidth)
                Rectangle().fill(theme.edge).frame(width: 1).ignoresSafeArea()
                trail
            }
        }
    }
}

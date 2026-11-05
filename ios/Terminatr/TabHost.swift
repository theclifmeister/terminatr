import SwiftUI
import TerminatrUI
import UIKit

/// The three places' screens, which a TabView held (T1005): each in its
/// own hosting controller, built the first time it's chosen and kept
/// after, so its navigation stack, scroll place and @State stay while
/// another shows. The one showing is the only one in the window, so a
/// screen's .onAppear, .onDisappear and .task run as they did in a tab, and
/// the status bar's tap finds one scroll view. A hosting controller is the
/// edge of SwiftUI's environment: Root.page hands each what it needs. It
/// lays out by the window's safe area where it touches its edges, and
/// avoids the keyboard itself.
struct TabHost: UIViewControllerRepresentable {
    let showing: TabPage
    /// Raised to send the page showing to its top, as a second tap of its
    /// tab did at its root.
    let toTop: Int
    let page: (TabPage) -> AnyView

    func makeUIViewController(context: Context) -> Pages { Pages() }

    func updateUIViewController(_ pages: Pages, context: Context) {
        pages.update(showing: showing, page: page)
        if pages.toTop != toTop {
            pages.toTop = toTop
            pages.scrollToTop()
        }
    }

    final class Pages: UIViewController {
        private var hosts: [TabPage: UIHostingController<AnyView>] = [:]
        private var current: UIHostingController<AnyView>?
        var toTop = 0

        func update(showing: TabPage, page: (TabPage) -> AnyView) {
            for (p, host) in hosts { host.rootView = page(p) }
            let host = hosts[showing] ?? {
                let new = UIHostingController(rootView: page(showing))
                new.view.backgroundColor = .clear
                hosts[showing] = new
                return new
            }()
            guard host !== current else { return }
            if let old = current {
                old.willMove(toParent: nil)
                old.view.removeFromSuperview()
                old.removeFromParent()
            }
            addChild(host)
            host.view.frame = view.bounds
            host.view.autoresizingMask = [.flexibleWidth, .flexibleHeight]
            view.addSubview(host.view)
            host.didMove(toParent: self)
            current = host
        }

        /// The first list down the page showing. Only ever at a page's
        /// root, so never a conversation, which SwiftUI alone may move.
        func scrollToTop() {
            guard let view = current?.view else { return }
            var next = [view]
            while !next.isEmpty {
                let v = next.removeFirst()
                if let s = v as? UIScrollView, !(s is UITextView), s.contentSize.height > 0 {
                    s.setContentOffset(CGPoint(x: s.contentOffset.x, y: -s.adjustedContentInset.top), animated: true)
                    return
                }
                next += v.subviews
            }
        }
    }
}

/// What TabHost keeps: Needs you, Projects (with the sidebar's rows) and
/// Search.
enum TabPage: Hashable, CaseIterable {
    case needs, projects, search

    var title: String {
        switch self {
        case .needs: "Needs you"
        case .projects: "Projects"
        case .search: "Search"
        }
    }

    var symbol: String {
        switch self {
        case .needs: "exclamationmark.bubble"
        case .projects: "folder"
        case .search: "magnifyingglass"
        }
    }

    /// Its icon in the bar, rail and sidebar; symbol is the large content
    /// viewer's.
    var glyph: FlatGlyph {
        switch self {
        case .needs: .alert
        case .projects: .folder
        case .search: .search
        }
    }

    var tab: RootTab {
        switch self {
        case .needs: .needs
        case .projects: .projects
        case .search: .search
        }
    }
}

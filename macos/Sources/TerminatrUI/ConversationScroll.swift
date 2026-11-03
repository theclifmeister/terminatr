import Foundation
import os
import SwiftUI
import TerminatrKit

/// How a conversation scrolls, on the iPhone, the iPad and the Mac
/// (T976): bottom-anchored, and only ever by SwiftUI.
///
/// The conversation's rows are laid out exactly (a VStack, the last
/// rows only, not a lazy stack whose estimated heights made every end
/// computed from them wrong), and nothing writes the scroll offset by
/// hand. It opens at the end (the initial offset's anchor is the
/// bottom). For size changes the anchor is the top, so what a user
/// scrolled up reads stays put; under a follower, streamed output, a
/// fold or rotation, the keyboard or a taller composer leave the end
/// below, and Follow says so: the view's scrollTo of its end row (a
/// live switch of the anchor isn't honoured: the Mac kept its first).
/// Jump to latest is that scrollTo, animated.
///
/// SwiftUI clamps the offsets it sets to the content as laid out, so
/// the view never rests past its end; the guard here holds that anyway:
/// Follow says when the scroll is at rest with the visible rect past
/// the content's end, the view scrolls to the end (toEnd) and the
/// screen's first such breach goes to the app's reports (kind scroll,
/// appreport.add), with the geometry, and to the log.
///
/// On iOS 17 and macOS 14 there's no scroll geometry to read: the view
/// keeps to the bottom always, as it did.
public struct ConversationScroll: ViewModifier {
    let follow: Follower
    let newest: Int?
    /// Which conversation, for a breach's report.
    let screen: String
    /// What the report says of it besides (rows drawn, the role).
    let detail: () -> [String: String]
    /// Told the row at the top as the user scrolls, when set (the
    /// iPhone's Places; the stack needs .scrollTargetLayout()).
    let top: ((Int?) -> Void)?
    /// SwiftUI's scroll to the end row.
    let toEnd: () -> Void

    public init(follow: Follower, newest: Int?, screen: String, detail: @escaping () -> [String: String] = { [:] },
                top: ((Int?) -> Void)? = nil, toEnd: @escaping () -> Void) {
        self.follow = follow
        self.newest = newest
        self.screen = screen
        self.detail = detail
        self.top = top
        self.toEnd = toEnd
    }

    public func body(content: Content) -> some View {
        if #available(iOS 18, macOS 15, *) {
            content
                .defaultScrollAnchor(.bottom, for: .initialOffset)
                .defaultScrollAnchor(.top, for: .sizeChanges)
                .onScrollGeometryChange(for: [Double].self) { g in
                    [g.contentOffset.y, g.visibleRect.height, g.contentSize.height, g.contentInsets.bottom, g.contentInsets.top]
                } action: { _, g in
                    if follow.scrolled(offset: g[0], visible: g[1], content: g[2], inset: g[3], top: g[4], newest: newest) { fix() }
                }
                .onScrollPhaseChange { _, p in
                    if follow.moved(by: p == .tracking || p == .interacting, resting: p == .idle) { fix() }
                }
                .modifier(TopRow(top: top))
        } else {
            content.defaultScrollAnchor(.bottom)
        }
    }

    /// Back to the end: a jump's momentum, or the guard's breach, which
    /// is reported once for the screen.
    private func fix() {
        if follow.breach, follow.firstBreach() {
            let p = ScrollGuard.report(follow, screen: screen, detail: detail())
            ScrollGuard.log.error("\(p.summary, privacy: .public): \(p.payload, privacy: .public)")
            #if canImport(MetricKit)
            CrashReporter.shared.report(p)
            #endif
        }
        // After this update, on the main run loop: a scroll asked for
        // inside the geometry's own update (a resize's layout) is
        // dropped.
        let toEnd = self.toEnd
        CFRunLoopPerformBlock(CFRunLoopGetMain(), CFRunLoopMode.commonModes.rawValue) {
            MainActor.assumeIsolated { toEnd() }
        }
        CFRunLoopWakeUp(CFRunLoopGetMain())
    }
}

/// The row at the top, as the user scrolls (iOS 18, macOS 15).
@available(iOS 18, macOS 15, *)
private struct TopRow: ViewModifier {
    let top: ((Int?) -> Void)?

    func body(content: Content) -> some View {
        if let top {
            content.onScrollTargetVisibilityChange(idType: Int.self, threshold: 0.01) { top($0.first) }
        } else {
            content
        }
    }
}

/// The scroll guard's report (T976): this app's build, OS and device.
public enum ScrollGuard {
    static let log = Logger(subsystem: Bundle.main.bundleIdentifier ?? "dev.terminatr", category: "scroll")

    @MainActor
    static func report(_ f: Follower, screen: String, detail: [String: String]) -> AppAPI.AppReportAddParams {
        let info = Bundle.main.infoDictionary ?? [:]
        let v = ProcessInfo.processInfo.operatingSystemVersion
        #if os(iOS)
        let os = "iOS \(v.majorVersion).\(v.minorVersion).\(v.patchVersion)"
        #else
        let os = "macOS \(v.majorVersion).\(v.minorVersion).\(v.patchVersion)"
        #endif
        return CrashReports.scroll(f.geometry, past: f.past, screen: screen, detail: detail,
                                   appVersion: info["CFBundleShortVersionString"] as? String ?? "",
                                   build: info["CFBundleVersion"] as? String ?? "", os: os, model: model(),
                                   ownUUID: CrashReports.ownUUID())
    }

    /// The device's model identifier (iPhone18,1, Mac16,10).
    static func model() -> String {
        #if os(iOS)
        let key = ProcessInfo.processInfo.environment["SIMULATOR_MODEL_IDENTIFIER"] == nil ? "hw.machine" : ""
        if key.isEmpty { return ProcessInfo.processInfo.environment["SIMULATOR_MODEL_IDENTIFIER"] ?? "" }
        #else
        let key = "hw.model"
        #endif
        var size = 0
        sysctlbyname(key, nil, &size, nil, 0)
        guard size > 0 else { return "" }
        var buf = [UInt8](repeating: 0, count: size)
        sysctlbyname(key, &buf, &size, nil, 0)
        return String(decoding: buf.prefix { $0 != 0 }, as: UTF8.self)
    }
}

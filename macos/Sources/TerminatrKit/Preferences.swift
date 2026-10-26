import Foundation

/// What Settings › Appearance picks (T748 milestone G): the theme's
/// light or dark, the sidebar's density and the text size. Each is a
/// user default the views read as they draw.
public enum Appearance: String, CaseIterable, Sendable {
    /// As the system: light or dark with it.
    case system, light, dark

    public static let key = "appearance"

    /// The stored choice; anything else is system.
    public static func of(_ raw: String?) -> Appearance {
        raw.flatMap(Appearance.init(rawValue:)) ?? .system
    }

    public var label: String {
        switch self {
        case .system: "System"
        case .light: "Light"
        case .dark: "Dark"
        }
    }
}

/// How close the sidebar's rows sit: View › Sidebar Density and
/// Settings › Appearance.
public enum Density: String, CaseIterable, Sendable {
    case compact, regular, roomy

    public static let key = "sidebarDensity"

    public static func of(_ raw: String?) -> Density {
        raw.flatMap(Density.init(rawValue:)) ?? .regular
    }

    public var label: String {
        switch self {
        case .compact: "Compact"
        case .regular: "Regular"
        case .roomy: "Roomy"
        }
    }

    /// Points added above and below each row's content.
    public var rowPadding: Double {
        switch self {
        case .compact: 0
        case .regular: 0
        case .roomy: 4
        }
    }

    /// The least height of a row; nil leaves the List's own.
    public var minRowHeight: Double? {
        self == .compact ? 20 : nil
    }
}

/// The text size: every font's size times a scale, in steps, as View ›
/// Make Text Bigger (⌘+), Smaller (⌘-) and Actual Size (⌘0) and the
/// Settings slider move it (T748 J1).
public enum TextSize {
    public static let key = "textSize"
    public static let steps: [Double] = [0.85, 0.9, 1, 1.1, 1.2, 1.3]
    public static let standard = 1.0

    /// A stored scale held to the steps' range; none is standard.
    public static func of(_ stored: Double?) -> Double {
        guard let stored, stored > 0 else { return standard }
        return min(max(stored, steps.first!), steps.last!)
    }

    /// The step next to scale's nearest: above it by 1, below by -1;
    /// it stays at either end.
    public static func step(_ scale: Double, by: Int) -> Double {
        let i = steps.indices.min { abs(steps[$0] - scale) < abs(steps[$1] - scale) }!
        return steps[min(max(i + by, 0), steps.count - 1)]
    }

    /// The scale as the slider's label says it ("110%").
    public static func label(_ scale: Double) -> String {
        "\(Int((scale * 100).rounded()))%"
    }
}

/// A role a text plays, which sets its size (T748 J1): views name one
/// in place of a point size, and the theme scales it by the text size
/// and, in the sidebar, the system's sidebar size. The sizes are
/// Bloom's at 100%.
public enum TextStyle: CaseIterable, Sendable {
    /// A project's name over its dashboard.
    case display
    /// A sheet's headline: the first-run sheet's.
    case sheetTitle
    case largeTitle, title, title2, title3, headline
    /// A third-level heading, the menu bar's titles.
    case heading
    case body, callout, subheadline, footnote, caption, caption2
    /// A small tag's text.
    case label
    /// A chevron or glyph beside a label.
    case glyph

    /// The size in points at 100%.
    public var points: Double {
        switch self {
        case .display: 30
        case .sheetTitle: 24
        case .largeTitle: 22
        case .title: 20
        case .title2: 18
        case .title3: 16
        case .headline: 15
        case .heading: 14
        case .body: 13
        case .callout: 12.5
        case .subheadline: 12
        case .footnote: 11.5
        case .caption: 11
        case .caption2: 10.5
        case .label: 10
        case .glyph: 9
        }
    }
}

/// System Settings › Appearance › Sidebar icon size, which the sidebar's
/// text follows as Finder's does: small, medium (the standard) or
/// large.
public enum SidebarSize: Sendable {
    case small, medium, large

    /// The sidebar's text size against medium's: the system's sidebar
    /// rows set 11, 13 and 15 pt.
    public var scale: Double {
        switch self {
        case .small: 11.0 / 13
        case .medium: 1
        case .large: 15.0 / 13
        }
    }
}

/// File › New Task…'s form: the title is one line and not empty, as tm
/// task add takes it; notes are optional.
public struct NewTaskForm: Equatable, Sendable {
    public var title = ""
    public var notes = ""

    public init(title: String = "", notes: String = "") {
        self.title = title
        self.notes = notes
    }

    public var trimmedTitle: String {
        title.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    public var canAdd: Bool {
        !trimmedTitle.isEmpty && !trimmedTitle.contains(where: \.isNewline)
    }

    /// task.add's params for project.
    public func params(project: String) -> AppAPI.TaskAddParams {
        AppAPI.TaskAddParams(project: project, title: trimmedTitle,
                             notes: notes.trimmingCharacters(in: .whitespacesAndNewlines))
    }

    /// What the sheet says once task.add answered.
    public static func done(_ r: AppAPI.TaskAdded, project: String) -> String {
        r.existing ? "\(r.task) is on \(project)'s board already." : "Added \(r.task) to \(project)'s board."
    }
}

/// Settings › Advanced's bridge log: what the app's bridge did, the
/// newest last, the oldest dropped past limit.
public struct BridgeLog: Sendable {
    public struct Entry: Equatable, Sendable, Identifiable {
        public let id: Int
        public let time: Date
        public let text: String
    }

    public private(set) var entries: [Entry] = []
    private var next = 0
    public let limit: Int

    public init(limit: Int = 200) {
        self.limit = limit
    }

    public mutating func add(_ text: String, at time: Date = Date()) {
        entries.append(Entry(id: next, time: time, text: text))
        next += 1
        if entries.count > limit {
            entries.removeFirst(entries.count - limit)
        }
    }

    /// The log as text, one entry a line ("23:45:02  connected …").
    public func text(_ format: (Date) -> String) -> String {
        entries.map { "\(format($0.time))  \($0.text)" }.joined(separator: "\n")
    }

    /// What a hello says of the bridge and its server.
    public static func connected(_ h: AppAPI.Hello, tm: String) -> String {
        var s = "Connected: \(tm) \(h.tm), app API \(h.appApi)"
        if let srv = h.server {
            s += "; server \(srv.version), protocol \(srv.protocol), pid \(srv.pid)"
            if srv.started { s += ", started by this bridge" }
        }
        return s
    }
}

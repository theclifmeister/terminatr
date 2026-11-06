import Foundation

/// When to read a project's daily numbers (project.history, app API 58)
/// again: when the board's done count changed, as the Done fold reads
/// task.done, never on a timer. The server keeps the numbers in memory,
/// but a project's journal moves every few seconds while agents work, and
/// nothing a client shows of them moves until a task finishes.
public struct HistoryReads: Equatable, Sendable {
    private var read: [String: Int] = [:]

    public init() {}

    /// Whether project's numbers need a read at this done count; it
    /// counts as read from here on.
    public mutating func needsRead(_ project: String, done: Int) -> Bool {
        if read[project] == done { return false }
        read[project] = done
        return true
    }

    /// Forgets project's read, one that failed, so the next asks again.
    public mutating func forget(_ project: String) {
        read[project] = nil
    }
}

/// A project's releases in words: each repo's count, a monorepo's by part,
/// never a total, as one repo's versions say nothing of another's. Empty
/// with no releases, where what shows them leaves them out.
public enum ReleaseCounts {
    public static func words(_ repos: [AppAPI.HistoryRepo]) -> String {
        repos.flatMap { r -> [String] in
            guard !r.parts.isEmpty else { return ["\(r.repo) \(r.count)"] }
            return r.parts.map { $0.part.isEmpty ? "\(r.repo) \($0.count)" : "\(r.repo) \($0.part) \($0.count)" }
        }.joined(separator: " · ")
    }
}

/// The Pulse as a board draws it (app API 69; design board-library, B5 and
/// B16): tm decides the bars and their words, so every surface shows the
/// same. No band before tm says it appears, or from an older tm.
public struct PulseBand: Equatable, Sendable {
    public let done: Int
    public let unit: String
    public let bars: [AppAPI.PulseBar]
    /// What stands under the count: how long the work has run, by what
    /// when it isn't days, the releases if any, and from weeks up the
    /// typical time, as the TUI's stat line says them.
    public let facts: String
    /// A row of dots, only with a release to show.
    public let hasDots: Bool

    public init?(_ history: AppAPI.History?) {
        guard let history, let p = history.pulse, !p.bars.isEmpty else { return nil }
        done = history.done
        unit = p.unit
        bars = p.bars
        hasDots = p.bars.contains(where: \.released)
        var words = [p.unit == "day" ? p.span : "\(p.span), by \(p.unit)"]
        if !p.releases.isEmpty { words.append(p.releases) }
        if p.unit != "day" && !history.typical.isEmpty { words.append("typical \(history.typical)") }
        facts = words.joined(separator: " · ")
    }

    /// The bar that starts on from, one with work: what a filter can show.
    public func bar(from: String?) -> AppAPI.PulseBar? {
        bars.first { $0.from == from && $0.done > 0 }
    }

    /// How tall bar is, of a height: the tallest fills it, one with work is
    /// never lost, and none draws nothing.
    public func height(_ bar: AppAPI.PulseBar, of full: Double) -> Double {
        guard bar.done > 0, let top = bars.map(\.done).max() else { return 0 }
        return max(full * Double(bar.done) / Double(top), 2)
    }
}

/// The History view as a page draws it (app API 76; design board-library, B6,
/// B15 and B16): tm decides the points and their words. No page before the
/// first finish, from a paired server or from an older tm, where nothing
/// offers it. Without releases there is no tick row or release fact, and
/// without designs no Chapters or flags.
public struct HistoryPage: Equatable, Sendable {
    /// A fact's value and what it is.
    public struct Fact: Equatable, Sendable {
        public let value: String
        public let word: String
    }

    /// A flag on the curve: a chapter's short name, where its first task
    /// finished, and whether it stands on the higher of two lines, so
    /// neighbours' names don't touch.
    public struct Flag: Equatable, Sendable {
        public let name: String
        public let title: String
        public let point: Int
        public let high: Bool
    }

    /// A row of ticks: the repo it stands for, "" for one with no name, and
    /// the points that hold a release.
    public struct TickRow: Equatable, Sendable {
        public let repo: String
        public let points: Set<Int>
    }

    public let curve: AppAPI.HistoryCurve
    public let chapters: [AppAPI.HistoryChapter]
    public let facts: [Fact]
    /// "the designs; 160 of 957 done tasks belong to one".
    public let chapterWords: String

    /// The most repos with a tick row each; beyond that, a menu.
    public static let tickRows = 3

    public init?(_ history: AppAPI.History?) {
        guard let history, let curve = history.curve, !curve.points.isEmpty else { return nil }
        self.curve = curve
        chapters = history.chapters
        chapterWords = "the designs; \(history.chapterDone) of \(history.done) done tasks belong to one"
        var facts = [Fact(value: "\(history.done)", word: "done")]
        for r in history.releases {
            let word = r.count == 1 ? "release" : "releases"
            facts.append(Fact(value: "\(r.count)", word: history.releases.count > 1 ? "\(r.repo) \(word)" : word))
        }
        facts.append(Fact(value: curve.span, word: "of work"))
        if !history.typical.isEmpty { facts.append(Fact(value: history.typical, word: "median, start to done")) }
        if curve.best.done > 0 { facts.append(Fact(value: "\(curve.best.done)", word: "\(curve.best.word), \(curve.best.when)")) }
        self.facts = facts
    }

    /// Whether the repos are too many for a tick row each.
    public var filtersRepos: Bool { curve.ticks.count > Self.tickRows }

    /// The tick rows: one for each repo up to three, then one, any repo's or
    /// with a filter the one it names. None without releases.
    public func tickRows(repo: String) -> [TickRow] {
        guard filtersRepos else { return curve.ticks.map { TickRow(repo: $0.repo, points: Set($0.points)) } }
        if let one = curve.ticks.first(where: { $0.repo == repo }) { return [TickRow(repo: one.repo, points: Set(one.points))] }
        return [TickRow(repo: "", points: Set(curve.ticks.flatMap(\.points)))]
    }

    /// The values the axis of the curve has lines at, round ones under top.
    public var gridLines: [Int] {
        let top = curve.points.last?.total ?? 0
        guard top > 0 else { return [0] }
        let magnitude = pow(10, floor(log10(Double(top))))
        let step = [0.1, 0.2, 0.25, 0.5, 1, 2, 2.5, 5, 10].map { $0 * magnitude }.first { Double(top) / $0 <= 4 } ?? magnitude
        let whole = max(Int(step.rounded()), 1)
        return Array(stride(from: 0, to: top, by: whole))
    }

    /// The tallest span's count, what the bars are scaled to.
    public var topDone: Int { curve.points.map(\.done).max() ?? 0 }

    /// The most points that are all named on the axis.
    public static let everyPoint = 8

    /// The points named on the axis, by index: all of a short curve's, else a
    /// week's Monday by day, a month or so by week, a half year by month, fewer
    /// where they'd touch.
    public func axis(width: Double, labelWidth: Double = 52) -> [Int] {
        let n = curve.points.count
        let base = n <= Self.everyPoint ? 1 : curve.unit == "day" ? 7 : curve.unit == "week" ? 4 : 6
        var every = base
        while n > 1, Double(every) * width / Double(n - 1) < labelWidth, every < n { every += base }
        return Array(stride(from: 0, to: n, by: every))
    }

    /// A chapter's short name for its flag: its title up to a colon, cut.
    public static func flagName(_ title: String) -> String {
        let short = title.split(separator: ":", maxSplits: 1).first.map(String.init) ?? title
        return short.count > 24 ? String(short.prefix(23)) + "…" : short
    }

    /// How far above its point a flag's name stands, on either line.
    public static func rise(high: Bool) -> Double { high ? 31 : 15 }

    /// The flags that fit a curve this wide and high, those with the most tasks
    /// done first: right-aligned on their point, on the lower line where the
    /// name touches none so far, else the higher, else not at all. Names sit at
    /// the curve's height there, so that is what they must clear. Empty without
    /// designs.
    public func flags(width: Double, height: Double = 190, charWidth: Double = 6.2, most: Int = 8) -> [Flag] {
        let n = curve.points.count
        let top = Double(max(curve.points.last?.total ?? 1, 1))
        func x(_ point: Int) -> Double { n > 1 ? Double(point) * width / Double(n - 1) : width / 2 }
        func y(_ point: Int) -> Double { height * (1 - Double(curve.points[point].total) / top) }
        var taken: [(xs: ClosedRange<Double>, y: Double)] = []
        var out: [Flag] = []
        let order = chapters.filter { $0.flag > 0 && $0.flag <= n }.sorted { $0.done != $1.done ? $0.done > $1.done : $0.to > $1.to }
        for c in order where out.count < most {
            let name = Self.flagName(c.title)
            let at = x(c.flag - 1)
            let wide = Double(name.count) * charWidth + 8
            let rightward = at - wide < 0 // near the left edge
            let start = rightward ? at : at - wide
            let xs = (start - 4)...(start + wide + 4)
            let high = [false, true].first { high in
                let ny = y(c.flag - 1) - Self.rise(high: high)
                // The curve only rises: leftward it stays below, rightward it can cross the name.
                if rightward, y(min(Int((start + wide) / max(width, 1) * Double(n - 1)) + 1, n - 1)) < ny + 7 { return false }
                return !taken.contains { $0.xs.overlaps(xs) && abs($0.y - ny) < 14 }
            }
            guard let high else { continue }
            taken.append((xs, y(c.flag - 1) - Self.rise(high: high)))
            out.append(Flag(name: name, title: c.title, point: c.flag - 1, high: high))
        }
        return out.sorted { $0.point < $1.point }
    }

    /// How far along a chapter is, from 0 to 1.
    public static func progress(_ chapter: AppAPI.HistoryChapter) -> Double {
        chapter.total > 0 ? Double(chapter.done) / Double(chapter.total) : 0
    }
}

/// The Release Tags sheet: which of a project's tags count (release.tags,
/// app API 65), as the TUI's Release tags row sets them.
public struct ReleaseTagsForm: Equatable, Sendable {
    public enum Mode: String, CaseIterable, Sendable {
        case follow, rule, patterns, none
    }

    public var mode: Mode
    /// What the person typed: patterns split on spaces and commas.
    public var text: String
    public let allProjects: String

    /// The form as the project has it now.
    public init(_ tags: AppAPI.ReleaseTags) {
        allProjects = tags.allProjects
        text = tags.patterns.joined(separator: " ")
        if !tags.own {
            mode = .follow
        } else {
            mode = Mode(rawValue: tags.mode) ?? .rule
        }
    }

    public var patterns: [String] {
        text.split(whereSeparator: { $0 == "," || $0.isWhitespace }).map(String.init)
    }

    /// Patterns need one, and "none" is a mode of its own.
    public var canSave: Bool {
        mode != .patterns || !(patterns.isEmpty || patterns == ["none"])
    }

    public func title(_ m: Mode) -> String {
        switch m {
        case .follow: "As all projects (\(allProjects))"
        case .rule: "Version tags"
        case .patterns: "These patterns"
        case .none: "None"
        }
    }

    public func params(project: String) -> AppAPI.ReleaseTagsSetParams {
        AppAPI.ReleaseTagsSetParams(project: project, mode: mode.rawValue, patterns: mode == .patterns ? patterns : [])
    }
}

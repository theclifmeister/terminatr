import Foundation

/// A project's read-only pages beside its dashboard (T794), as the TUI's
/// project popup shows them: Overview (goal, repositories, coordinator
/// and threads by agent, tasks), Memory (CONTEXT.md, MEMORY.md and the
/// notes, Markdown) and Library (the files threads attached to their
/// reports, and those linked projects sent). Nothing here edits; no path
/// but a memory file's in the project folder crosses.
public enum ProjectPage: String, CaseIterable, Identifiable, Sendable {
    case dashboard = "Dashboard"
    case overview = "Overview"
    case memory = "Memory"
    case library = "Library"
    /// The project's mods (T869): on or off for it, and their settings
    /// for it, as the TUI's project Mods tab.
    case mods = "Mods"

    public var id: String { rawValue }

    /// The pages hello's tm has: Memory and Library need project-files,
    /// Mods project-mods.
    public static func pages(_ hello: AppAPI.Hello?) -> [ProjectPage] {
        guard let hello else { return [.dashboard, .overview] }
        var out: [ProjectPage] = [.dashboard, .overview]
        if Compatibility.readsProjectFiles(hello) { out += [.memory, .library] }
        if Compatibility.listsMods(hello) { out.append(.mods) }
        return out
    }
}

/// The Memory page's files: the project's own two first, then the notes.
public enum MemoryPage {
    public static func project(_ files: [AppAPI.MemoryFile]) -> [AppAPI.MemoryFile] {
        files.filter { !isNote($0) }
    }

    public static func notes(_ files: [AppAPI.MemoryFile]) -> [AppAPI.MemoryFile] {
        files.filter(isNote)
    }

    public static func isNote(_ f: AppAPI.MemoryFile) -> Bool { f.path.hasPrefix("memory/") }

    /// The file shown first: CONTEXT.md, else the first listed.
    public static func first(_ files: [AppAPI.MemoryFile]) -> String? {
        files.first { $0.path == "CONTEXT.md" }?.path ?? files.first?.path
    }
}

/// The Library page's files grouped as the TUI's Library tab groups
/// them: by thread, or by the linked project that sent them, the seat's own
/// group first, then the group with the newest file first and each group's
/// files newest first. A program's list keeps its projects in the order they
/// came, each ordered so (sections).
public enum LibraryPage {
    /// The id of a coordinator's or director's own files, where a thread's
    /// goes (thread.SeatID, app API 59).
    public static let seatID = "seat"

    /// Whether f is a coordinator's or director's own.
    public static func isSeat(_ f: AppAPI.LibraryFile) -> Bool { f.thread == seatID }

    /// A project's groups in a program's list: the director's own have no
    /// title, each member's its name and its paired server's.
    public struct Section: Identifiable, Sendable, Equatable {
        public let id: String
        public let project: String
        public let server: String
        public let groups: [Group]

        /// The heading over a member's files, "" for the director's own or a
        /// project's own list.
        public var title: String {
            server.isEmpty ? project : "\(project) on \(server)"
        }
    }

    public struct Group: Identifiable, Sendable, Equatable {
        /// The thread's id, or "@" and the linked project's.
        public let id: String
        public let files: [AppAPI.LibraryFile]

        /// "T12 · Fix the build", or "from web" for a linked project's.
        public var title: String {
            files.first.map(LibraryPage.source) ?? id
        }

        /// "t-0003 · archived" (its thread and state), or how many a
        /// linked project sent.
        public var detail: String {
            guard let f = files.first else { return "" }
            if !f.peer.isEmpty || LibraryPage.isSeat(f) { return files.count == 1 ? "1 file" : "\(files.count) files" }
            return f.state == "archived" ? "\(f.thread) · archived" : f.thread
        }

        /// Its task's status, "" for none: no task, a linked project's.
        public var status: String {
            guard let f = files.first, f.peer.isEmpty else { return "" }
            return f.taskStatus
        }

        /// Its thread and, in a project with several, the repository; a
        /// linked project's group has neither, nor a seat's own.
        public var origin: [String] {
            guard let f = files.first, f.peer.isEmpty, !LibraryPage.isSeat(f) else { return [] }
            return f.repo.isEmpty ? [f.thread] : [f.thread, f.repo]
        }

        public var isArchived: Bool { files.first?.state == "archived" }

        /// Its kinds, in the chips' order: "26 images", "2 data files".
        public var kinds: [String] {
            var count: [Kind: Int] = [:]
            for f in files { count[LibraryPage.kind(f.name), default: 0] += 1 }
            return Kind.allCases.compactMap { k in count[k].map { "\($0) \(k.noun)\($0 == 1 ? "" : "s")" } }
        }

        public var bytes: Int { files.reduce(0) { $0 + $1.size } }

        /// The line under the title: "t-0003 · api · 6 Nov · archived ·
        /// 2 images · 3.4 MB". Of the files the filter and kind leave.
        public func facts(now: Date = Date(), calendar: Calendar = .current) -> String {
            var out = origin
            if let t = files.first?.time { out.append(LibraryPage.day(t, now: now, calendar: calendar)) }
            if isArchived { out.append("archived") }
            return (out + kinds + [Attachment.size(bytes)]).joined(separator: " · ")
        }

        /// Its files as rows: a stack's two are one (T1057).
        public var rows: [Row] { LibraryPage.rows(files) }

        /// How it lists (T1055, T1059): the rows tiles says draw as thumbnails
        /// go in its strip, the rest stay rows; a stack is one of either, a
        /// tile only when both its files draw. Folded, four of the one and six
        /// of the other, and +N for what's left of both; a group with no tiles
        /// folds as it always did. Words in the filter open it, so what they
        /// find shows.
        public func listed(open: Bool, query: String, tiles: (AppAPI.LibraryFile) -> Bool = { _ in false }) -> Listing {
            let all = self.rows
            let strip = all.filter { $0.files.allSatisfy(tiles) }, rows = all.filter { !$0.files.allSatisfy(tiles) }
            if strip.isEmpty {
                let f = LibraryPage.fold(rows.count, open: open, query: query)
                return Listing(strip: [], rows: Array(rows.prefix(f.rows)), more: f.more, fewer: f.fewer)
            }
            let more = max(0, strip.count - LibraryPage.stripAt) + max(0, rows.count - LibraryPage.foldAt)
            if !LibraryPage.words(query).isEmpty || more == 0 { return Listing(strip: strip, rows: rows, more: 0, fewer: false) }
            if open { return Listing(strip: strip, rows: rows, more: 0, fewer: true) }
            return Listing(strip: Array(strip.prefix(LibraryPage.stripAt)), rows: Array(rows.prefix(LibraryPage.foldAt)), more: more, fewer: false)
        }
    }

    /// How the page lists (T1056): by group, newest first, or flat, biggest
    /// first.
    public enum Sort: String, CaseIterable, Sendable {
        case newest = "Newest", biggest = "Biggest"
    }

    /// Whether a library has anything to sort: two files. The TUI's
    /// libShowsSort.
    public static func showsSort(_ files: [AppAPI.LibraryFile]) -> Bool { files.count >= 2 }

    /// Whether it lists biggest first: asked for, and for as long as the
    /// library has two files.
    public static func isFlat(_ files: [AppAPI.LibraryFile], _ sort: Sort) -> Bool {
        sort == .biggest && showsSort(files)
    }

    /// The flat list's rows: every file its own, so a stack comes apart, each
    /// half under its full name and its own size. The sort is there to show
    /// what takes the room, and a pair has no one size to rank by.
    public static func biggestRows(_ files: [AppAPI.LibraryFile]) -> [Row] {
        biggestFirst(files).map { Row(files: [$0], words: [], title: $0.name) }
    }

    /// The files by size, the biggest first; equal sizes keep their order,
    /// the list's, newest first. The TUI's libBiggestFirst, the offset
    /// because Swift's sort doesn't promise to be stable.
    public static func biggestFirst(_ files: [AppAPI.LibraryFile]) -> [AppAPI.LibraryFile] {
        files.enumerated()
            .sorted { $0.element.size != $1.element.size ? $0.element.size > $1.element.size : $0.offset < $1.offset }
            .map(\.element)
    }

    /// What a file came with: "T12 · Fix the build", its thread when it has
    /// neither, or "from web" for a linked project's.
    public static func source(_ f: AppAPI.LibraryFile) -> String {
        if !f.peer.isEmpty { return "from \(f.peer)" }
        if isSeat(f) { return f.title }
        let s = [f.task, f.title].filter { !$0.isEmpty }.joined(separator: " · ")
        return s.isEmpty ? f.thread : s
    }

    /// A row of a group: one file, or a stack's two, the same picture twice
    /// (T1057), which the row's switch goes between. A wrong pair costs
    /// little: the switch shows both and the heading counts both.
    public struct Row: Identifiable, Sendable, Hashable {
        /// One file, or a stack's two in the list's order.
        public let files: [AppAPI.LibraryFile]
        /// A stack's words, one per file: "dark", "light".
        public let words: [String]
        /// The name without the word: "mac-conv.png".
        public let title: String

        /// Its first file's: a row keeps it while the filter leaves it.
        public var id: String { LibraryPage.place(files[0]) + LibraryPage.id(files[0]) + "/" + files[0].name }

        public var isStack: Bool { files.count == 2 }

        /// Before and after open side by side; dark and light switch.
        public var opensSideBySide: Bool { words.contains("before") }

        /// Before on the left, whichever is newer.
        public var sideBySide: [(word: String, file: AppAPI.LibraryFile)] {
            let pairs = Array(zip(words, files))
            return pairs.first?.0 == "after" ? [pairs[1], pairs[0]] : pairs
        }
    }

    /// The words that pair, and whether each may also start a name:
    /// dark-matter.png is no variant of anything, before-action.png is.
    /// The TUI's libStackPairs: internal/tui/testdata/library-filter.json
    /// pins both.
    private static let stackPairs: [(words: [String], start: Bool)] = [(["dark", "light"], false), (["before", "after"], true)]

    /// What a file has to share with the other half of its pair. The rest
    /// of the name by its bytes: identical, as the TUI compares it.
    private struct Slot: Hashable {
        let pair: Int
        let start: Bool
        let rest: [UInt8]
        let ext: String
    }

    /// Whether these bytes are the word (lower case), in either case. Not
    /// by Unicode, where a Kelvin sign folds to a k.
    private static func isWord(_ s: ArraySlice<UInt8>, _ word: String) -> Bool {
        s.count == word.utf8.count && zip(s, word.utf8).allSatisfy { c, w in
            (0x41...0x5A).contains(c) ? c + 0x20 == w : c == w
        }
    }

    /// The ways a file's name could be half of a pair: none unless it's an
    /// image of a thread or of a seat's own files, each one folder. A linked
    /// project's files come from no thread.
    private static func slots(_ f: AppAPI.LibraryFile) -> [(slot: Slot, word: String)] {
        let name = Array(f.name.utf8)
        guard f.peer.isEmpty, kind(f.name) == .images, let dot = name.lastIndex(of: 0x2E), dot > 0 else { return [] }
        let stem = name[..<dot]
        let ext = String(decoding: name[dot...], as: UTF8.self).lowercased()
        var out: [(Slot, String)] = []
        for (p, pair) in stackPairs.enumerated() {
            for w in pair.words {
                let n = stem.count - w.utf8.count
                if n < 0 { continue }
                if n == 0 {
                    if isWord(stem, w) { out.append((Slot(pair: p, start: false, rest: [], ext: ext), w)) } // the whole name: dark.png
                    continue
                }
                if stem[n - 1] == 0x2D, isWord(stem[n...], w) {
                    out.append((Slot(pair: p, start: false, rest: Array(stem[..<(n - 1)]), ext: ext), w))
                }
                let len = w.utf8.count
                if pair.start, stem[len] == 0x2D, isWord(stem[..<len], w) {
                    out.append((Slot(pair: p, start: true, rest: Array(stem[(len + 1)...]), ext: ext), w))
                }
            }
        }
        return out
    }

    /// A group's files as rows, in their order (the TUI's libEntries). A
    /// pair takes the place of its first file. A slot with anything but one
    /// file of each word pairs nothing, and a file two pairs want stays in
    /// neither: both are more than two files of one name.
    public static func rows(_ files: [AppAPI.LibraryFile]) -> [Row] {
        var halves: [Slot: [(at: Int, word: String)]] = [:]
        for (i, f) in files.enumerated() {
            for s in slots(f) { halves[s.slot, default: []].append((i, s.word)) }
        }
        let pairs = halves.filter { $0.value.count == 2 && $0.value[0].word != $0.value[1].word }
        var wanted: [Int: Int] = [:]
        for h in pairs.values { for half in h { wanted[half.at, default: 0] += 1 } }
        var stacks: [Int: Row] = [:]
        var second: Set<Int> = []
        for (slot, h) in pairs where wanted[h[0].at] == 1 && wanted[h[1].at] == 1 {
            let first = files[h[0].at]
            let name = Array(first.name.utf8)
            let ext = name.lastIndex(of: 0x2E).map { name[$0...] } ?? []
            let title = slot.rest.isEmpty ? first.name : String(decoding: slot.rest + ext, as: UTF8.self)
            stacks[h[0].at] = Row(files: [first, files[h[1].at]], words: [h[0].word, h[1].word], title: title)
            second.insert(h[1].at)
        }
        return files.indices.filter { !second.contains($0) }.map { i in
            stacks[i] ?? Row(files: [files[i]], words: [], title: files[i].name)
        }
    }

    /// A group as it lists: its strip, its rows, and its fold if any.
    public struct Listing: Sendable, Equatable {
        public let strip: [Row]
        public let rows: [Row]
        /// How many tiles and rows its fold hides: a stack is one.
        public let more: Int
        /// Whether it offers to fold again.
        public let fewer: Bool

        /// What it shows, top to bottom.
        public var all: [Row] { strip + rows }
        public var folds: Bool { more > 0 || fewer }

        /// The row that lists this file: a stack's for either half.
        public func row(of f: AppAPI.LibraryFile) -> Row? { all.first { $0.files.contains(f) } }
    }

    /// How many thumbnails a folded group shows.
    public static let stripAt = 4

    /// How many thumbnails go across a list this wide: four, or two where
    /// four would be too small to tell apart.
    public static func stripColumns(width: Double) -> Int { width < 480 ? 2 : 4 }

    /// A list's rows of thumbnails: each its own row of a lazy list, so a
    /// long group's tiles load only when they come into view.
    public static func stripRows<T>(_ strip: [T], columns: Int) -> [[T]] {
        stride(from: 0, to: strip.count, by: max(1, columns)).map { Array(strip[$0..<min($0 + max(1, columns), strip.count)]) }
    }

    /// How many rows a folded group shows.
    public static let foldAt = 6

    /// How a group of n rows lists (T1055; a stack is one): how many show, how
    /// many its fold hides, and whether it offers to fold again. Words in the
    /// filter open every fold, so what they find shows. The TUI's libFold.
    public static func fold(_ n: Int, open: Bool, query: String) -> (rows: Int, more: Int, fewer: Bool) {
        if n <= foldAt || !words(query).isEmpty { return (n, 0, false) }
        return open ? (n, 0, true) : (foldAt, n - foldAt, false)
    }

    /// A day as a heading says it: the year only when it isn't this one.
    public static func day(_ d: Date, now: Date = Date(), calendar: Calendar = .current) -> String {
        var style = Date.FormatStyle.dateTime.month(.abbreviated).day()
        style.timeZone = calendar.timeZone
        if calendar.component(.year, from: d) != calendar.component(.year, from: now) { style = style.year() }
        return d.formatted(style)
    }

    /// A file's group: its thread, or "@" and the linked project, in its
    /// project in a program's list.
    public static func key(_ f: AppAPI.LibraryFile) -> String {
        place(f) + (f.peer.isEmpty ? f.thread : "@" + f.peer)
    }

    /// What tells a member's file from another member's in a program's list,
    /// "" for the director's own or in a project's own list.
    public static func place(_ f: AppAPI.LibraryFile) -> String {
        f.project.isEmpty ? "" : f.server + "/" + f.project + "/"
    }

    public static func groups(_ files: [AppAPI.LibraryFile]) -> [Group] {
        sections(files).flatMap(\.groups)
    }

    /// The files by project, in the order the projects came, each project's
    /// groups as groups orders them.
    public static func sections(_ files: [AppAPI.LibraryFile]) -> [Section] {
        var places: [String] = []
        var byPlace: [String: [AppAPI.LibraryFile]] = [:]
        for f in files {
            let p = place(f)
            if byPlace[p] == nil { places.append(p) }
            byPlace[p, default: []].append(f)
        }
        return places.map { p in
            let inPlace = byPlace[p] ?? []
            var order: [String] = []
            var by: [String: [AppAPI.LibraryFile]] = [:]
            for f in inPlace.sorted(by: { $0.time > $1.time }) {
                let k = key(f)
                if by[k] == nil { order.append(k) }
                by[k, default: []].append(f)
            }
            let own = order.filter { by[$0]?.first.map(isSeat) == true }
            let rest = order.filter { by[$0]?.first.map(isSeat) != true }
            let first = inPlace.first
            return Section(id: p, project: first?.project ?? "", server: first?.server ?? "",
                           groups: (own + rest).map { Group(id: $0, files: by[$0] ?? []) })
        }
    }

    /// A file's kind, from its name's extension (T1054). The TUI's
    /// libKinds and libKindOf: internal/tui/testdata/library-filter.json
    /// pins both.
    public enum Kind: String, CaseIterable, Sendable {
        case images = "Images", pages = "Pages", notes = "Notes", data = "Data"
        case archives = "Archives", video = "Video", code = "Code", other = "Other"

        /// One file of it, in a heading.
        var noun: String {
            switch self {
            case .images: "image"
            case .pages: "page"
            case .notes: "note"
            case .data: "data file"
            case .archives: "archive"
            case .video: "video"
            case .code: "code file"
            case .other: "file"
            }
        }
    }

    private static let kindOf: [String: Kind] = {
        var out: [String: Kind] = [:]
        let lists: [(Kind, String)] = [
            (.images, "png jpg jpeg gif webp heic heif svg bmp tif tiff ico avif"),
            (.pages, "html htm pdf"),
            (.notes, "md markdown txt rtf"),
            (.data, "json jsonl ndjson csv tsv xml yaml yml toml sql sqlite db log"),
            (.archives, "zip tar gz tgz bz2 xz zst 7z rar dmg"),
            (.video, "mp4 mov m4v webm mkv avi"),
            (.code, "go swift py js jsx ts tsx sh zsh bash rb rs c h cc cpp hpp java kt php lua pl css diff patch"),
        ]
        for (kind, exts) in lists {
            for e in exts.split(separator: " ") { out[String(e)] = kind }
        }
        return out
    }()

    public static func kind(_ name: String) -> Kind {
        guard let dot = name.lastIndex(of: "."), dot != name.startIndex else { return .other }
        return kindOf[name[name.index(after: dot)...].lowercased()] ?? .other
    }

    /// A chip: a kind (nil for all) and how many files the filter leaves
    /// of it.
    public struct Chip: Identifiable, Sendable, Equatable {
        public let kind: Kind?
        public let count: Int
        public var id: String { kind?.rawValue ?? "All" }
        public var word: String { kind?.rawValue ?? "All" }
    }

    private static func words(_ query: String) -> [String] {
        query.lowercased().split(whereSeparator: \.isWhitespace).map(String.init)
    }

    /// Whether every word is in the file's name, its own title, or its
    /// task, title, thread, repository or linked project, or a program's
    /// member and its server.
    private static func matches(_ f: AppAPI.LibraryFile, _ words: [String]) -> Bool {
        if words.isEmpty { return true }
        let hay = [f.name, f.docTitle, f.task, f.title, f.thread, f.repo, f.peer, f.project, f.server].joined(separator: "\n").lowercased()
        return words.allSatisfy(hay.contains)
    }

    /// The kind that holds: pick, unless the library has fewer than two
    /// kinds or none of it (say, its last file went).
    public static func picked(_ files: [AppAPI.LibraryFile], _ pick: Kind?) -> Kind? {
        guard let pick else { return nil }
        let kinds = Set(files.map { kind($0.name) })
        return kinds.contains(pick) && kinds.count >= 2 ? pick : nil
    }

    /// The files the filter and the kind leave, in their order.
    public static func shown(_ files: [AppAPI.LibraryFile], query: String, kind pick: Kind?) -> [AppAPI.LibraryFile] {
        let words = words(query)
        let pick = picked(files, pick)
        if words.isEmpty, pick == nil { return files }
        return files.filter { matches($0, words) && (pick == nil || kind($0.name) == pick) }
    }

    /// The chips: all, then the kinds the filter leaves files of, and the
    /// picked kind even with none. Under two kinds, none at all, unless
    /// one is picked, so it can be let go.
    public static func chips(_ files: [AppAPI.LibraryFile], query: String, kind pick: Kind?) -> [Chip] {
        let words = words(query)
        let pick = picked(files, pick)
        var count: [Kind: Int] = [:]
        var all = 0
        for f in files where matches(f, words) {
            count[kind(f.name), default: 0] += 1
            all += 1
        }
        let kinds = Kind.allCases.filter { (count[$0] ?? 0) > 0 || $0 == pick }
        if kinds.count < 2, pick == nil { return [] }
        return [Chip(kind: nil, count: all)] + kinds.map { Chip(kind: $0, count: count[$0] ?? 0) }
    }

    /// Whether the filter shows: from 7 files, and for as long as it has
    /// words.
    public static func showsFilter(_ files: [AppAPI.LibraryFile], query: String) -> Bool {
        files.count >= 7 || !query.isEmpty
    }

    /// The filter field's words: the repository only where a file names one.
    public static func filterPrompt(_ files: [AppAPI.LibraryFile]) -> String {
        if files.contains(where: { !$0.project.isEmpty }) { return "Filter by name, task, thread or project" }
        return files.contains { !$0.repo.isEmpty } ? "Filter by name, task, thread or repo" : "Filter by name, task or thread"
    }

    /// Where a page is published (T1061), nil for none: an https address
    /// alone, whatever the list said.
    public static func published(_ f: AppAPI.LibraryFile) -> URL? {
        guard let url = URL(string: f.published), url.scheme?.lowercased() == "https", url.host?.isEmpty == false else { return nil }
        return url
    }

    /// What library.read names a file by: its thread, else the message a
    /// linked project sent it with.
    public static func id(_ f: AppAPI.LibraryFile) -> String {
        f.thread.isEmpty ? f.message : f.thread
    }

    /// Whether the server makes a thumbnail of a file of this name: PNG,
    /// JPEG and GIF (internal/thumb Supported). Anything else stays a row
    /// and asks nothing, so a library with no images makes no call.
    public static func hasThumbnail(_ f: AppAPI.LibraryFile) -> Bool {
        let name = f.name.lowercased()
        return [".png", ".jpg", ".jpeg", ".gif"].contains { name.hasSuffix($0) }
    }

    /// A thumbnail decoded to draw: within FileImage's bounds, as what a
    /// server sends is data. Nil for a plain row.
    public static func drawn(_ t: AppAPI.LibraryThumbnail) async -> FileImage.Drawn? {
        guard case .drawn(let d)? = await FileImageDecoder.shared.decode(t.data) else { return nil }
        return d
    }

    /// The file's thumbnail, or nil for a plain row: a file that isn't an
    /// image, without asking; one that makes none, a server too old, a
    /// link down, whatever the answer was.
    public static func thumbnail(_ fetch: @Sendable (AppAPI.LibraryThumbnailParams) async throws -> AppAPI.LibraryThumbnail,
                                 project: String, file f: AppAPI.LibraryFile) async -> AppAPI.LibraryThumbnail? {
        guard hasThumbnail(f) else { return nil }
        return try? await fetch(AppAPI.LibraryThumbnailParams(project: project, id: id(f), name: f.name))
    }

    /// library.read, as the bridge answers it.
    public typealias Fetch = @Sendable (AppAPI.LibraryReadParams) async throws -> AppAPI.LibraryChunk

    /// The file's bytes, a chunk at a time; again from the start, once,
    /// when it changed between chunks.
    /// With program, a director's own file of that program.
    public static func read(_ fetch: Fetch, project: String, program: String? = nil, file f: AppAPI.LibraryFile) async -> FileOutcome {
        let ref = program.map { AppAPI.LibraryProgram(slug: $0) }
        let params = { (off: Int) in AppAPI.LibraryReadParams(project: project, program: ref, id: id(f), name: f.name, offset: off) }
        for _ in 0..<2 {
            var out = Data()
            var size = -1
            var mtime: Date?
            var changed = false
            do {
                repeat {
                    try Task.checkCancellation()
                    let c = try await fetch(params(out.count))
                    if size >= 0, c.size != size || c.mtime != mtime {
                        changed = true
                        break
                    }
                    (size, mtime) = (c.size, c.mtime)
                    if c.data.isEmpty { break }
                    out.append(c.data)
                } while out.count < size
            } catch {
                return FileProblem(error).outcome
            }
            if !changed {
                return out.count == size ? .data(out, mtime) : .gone
            }
        }
        return .unavailable("the file kept changing while it was read")
    }
}

/// The thumbnails a Library holds on to, the latest used while they fit: a
/// long scroll would otherwise keep every one it passed. Enough for what
/// shows on any screen; one that went is asked for again, from the server's
/// cache.
public struct LibraryThumbs: Sendable {
    /// About 400 KB each, drawn.
    public static let budget = 120

    private var images: [String: FileImage.Drawn] = [:]
    private var order: [String] = []
    private let budget: Int

    public init(budget: Int = budget) {
        self.budget = budget
    }

    /// The image of key.
    public subscript(key: String) -> FileImage.Drawn? { images[key] }

    /// Notes key was just shown, so it stays longest.
    public mutating func touch(_ key: String) {
        guard images[key] != nil else { return }
        order.removeAll { $0 == key }
        order.append(key)
    }

    public mutating func add(_ key: String, _ image: FileImage.Drawn) {
        order.removeAll { $0 == key }
        order.append(key)
        images[key] = image
        while order.count > budget { images[order.removeFirst()] = nil }
    }

    public mutating func removeAll() {
        images = [:]
        order = []
    }
}

/// The Overview page's lines, as the TUI's A › overview: the goal, the
/// repositories (with a checkout's note), the coordinator's agent and
/// state, the threads by agent and the tasks' counts. Settings stay the
/// TUI's.
public struct ProjectOverview: Equatable, Sendable {
    public struct Repo: Equatable, Sendable {
        public let path: String
        /// A checkout's warning ("behind origin by 3"), empty for none.
        public let note: String
    }

    public struct Agent: Equatable, Sendable {
        public let agent: String
        public let threads: [String]
    }

    public let goal: String
    public let repos: [Repo]
    /// A paired server's project: how many repositories it has, whose
    /// paths stay there (repos is empty then); nil for this server's.
    public let repoCount: Int?
    /// PROJECT.md's body, its standing instructions: a paired server's
    /// project's, "" for this server's (the TUI shows those).
    public let instructions: String
    /// The coordinator's agent and state ("claude · working"); empty
    /// when none runs.
    public let coordinator: String
    public let agents: [Agent]
    /// "2 need you · 3 in motion · 5 on deck · 9 done".
    public let tasks: String

    public init(_ p: AppAPI.Project, coordinator: AppAPI.Session?, needs: Int) {
        goal = p.goal.trimmingCharacters(in: .whitespacesAndNewlines)
        repos = p.repos.map { Repo(path: $0, note: p.checkouts[$0] ?? "") }
        repoCount = nil
        instructions = ""
        if let c = coordinator {
            self.coordinator = [c.agent.isEmpty ? "claude" : c.agent, c.state].filter { !$0.isEmpty }.joined(separator: " · ")
        } else {
            self.coordinator = ""
        }
        var by: [String: [String]] = [:]
        for t in p.threads {
            by[t.record.agent.isEmpty ? "claude" : t.record.agent, default: []].append(t.record.id)
        }
        agents = by.keys.sorted().map { Agent(agent: $0, threads: by[$0] ?? []) }
        let needWords = needs == 1 ? "1 needs you" : "\(needs) need you"
        tasks = "\(needWords) · \(p.counts["in_motion"] ?? 0) in motion · \(p.counts["on_deck"] ?? 0) on deck · \(p.counts["done"] ?? 0) done"
    }

    /// A paired server's project (T886): its pages, and from peer.list
    /// its coordinator and threads (project) and what waits on that
    /// server's user.
    public init(_ pages: AppAPI.PeerPages, project p: AppAPI.PeerProject) {
        goal = pages.goal.trimmingCharacters(in: .whitespacesAndNewlines)
        repos = []
        repoCount = pages.repos
        instructions = pages.body.trimmingCharacters(in: .whitespacesAndNewlines)
        if let c = p.sessions.first(where: { $0.role == "coordinator" }) {
            coordinator = [c.agent.isEmpty ? "claude" : c.agent, c.state].filter { !$0.isEmpty }.joined(separator: " · ")
        } else {
            coordinator = ""
        }
        var by: [String: [String]] = [:]
        for t in p.sessions where t.role == "thread" {
            by[t.agent.isEmpty ? "claude" : t.agent, default: []].append(t.thread)
        }
        agents = by.keys.sorted().map { Agent(agent: $0, threads: by[$0] ?? []) }
        let needs = p.needsYou
        let needWords = needs == 1 ? "1 needs you" : "\(needs) need you"
        let motion = pages.tasks.filter { PeerPages.inMotion.contains($0.status) }.count
        tasks = "\(needWords) · \(motion) in motion · \(PeerPages.onDeck(pages).count) on deck · \(PeerPages.done(pages).count) done"
    }
}

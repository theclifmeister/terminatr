import Foundation
import Testing
@testable import TerminatrKit

private func dir(_ path: String) -> AppAPI.FileEntry {
    AppAPI.FileEntry(name: (path as NSString).lastPathComponent, path: path, kind: "dir")
}

private func file(_ path: String) -> AppAPI.FileEntry {
    AppAPI.FileEntry(name: (path as NSString).lastPathComponent, path: path, kind: "file")
}

private let tree: [String: [AppAPI.FileEntry]] = [
    "": [dir("macos"), dir("docs"), file("README.md")],
    "macos": [dir("macos/Sources"), file("macos/Makefile")],
    "macos/Sources": [file("macos/Sources/a.swift")],
    "docs": [file("docs/SPEC.md")],
]

@Test func thePressedButtonSwitchesThePanelAndTheLitOneHidesIt() {
    #expect(RightPanel.pressed(.files, now: nil) == .files)
    #expect(RightPanel.pressed(.files, now: .inspector) == .files)
    #expect(RightPanel.pressed(.files, now: .files) == nil)
    #expect(RightPanel.pressed(.inspector, now: .files) == .inspector)
    #expect(RightPanel.pressed(.inspector, now: .inspector) == nil)
}

@Test func filesGivesWayToTheInspectorWhereThereAreNone() {
    #expect(RightPanel.shown(.files, hasFiles: false) == .inspector)
    #expect(RightPanel.shown(.files, hasFiles: true) == .files)
    #expect(RightPanel.shown(nil, hasFiles: false) == nil)
}

@Test func thePanelsChoiceReadsTheOldInspectorKey() {
    #expect(RightPanel.saved(nil, inspector: nil) == .inspector)
    #expect(RightPanel.saved(nil, inspector: false) == nil)
    #expect(RightPanel.saved("files", inspector: false) == .files)
    #expect(RightPanel.saved("", inspector: true) == nil)
}

@Test func aRunningThreadBrowsesItsWorktree() throws {
    let p = try #require(FilesPlace.of(.session(SessionRef(project: "web", thread: "t-1")), sessionID: "s-1", threadDone: false, task: "T9", readsWorktree: true, hasLibrary: true))
    #expect(p.session == "s-1")
    #expect(p.why == nil)
    #expect(p.thread == "t-1")
    #expect(p.label == "T9 t-1")
    #expect(p.relativeMention)
}

@Test func aStoppedOrDoneThreadSaysWhyItHasNoWorktree() throws {
    let ref = SessionRef(project: "web", thread: "t-1")
    let stopped = try #require(FilesPlace.of(.session(ref), sessionID: nil, threadDone: false, readsWorktree: true, hasLibrary: true))
    #expect(stopped.session == nil)
    #expect(stopped.why?.contains("isn't running") == true)
    let done = try #require(FilesPlace.of(.session(ref), sessionID: nil, threadDone: true, readsWorktree: true, hasLibrary: true))
    #expect(done.why?.contains("worktree is gone") == true)
}

@Test func aProjectOrCoordinatorBrowsesTheRepoAndAllAttached() throws {
    let p = try #require(FilesPlace.of(.project("web"), sessionID: "s-c", threadDone: false, readsWorktree: true, hasLibrary: true))
    #expect(p.session == "s-c")
    #expect(p.thread == nil)
    #expect(!p.relativeMention)
    #expect(p.mentions == SessionRef(project: "web"))
    let c = try #require(FilesPlace.of(.session(SessionRef(project: "web")), sessionID: nil, threadDone: false, readsWorktree: true, hasLibrary: true))
    #expect(c.thread == nil)
    #expect(c.why?.contains("coordinator") == true)
}

@Test func aPairedServersFilesAreAttachedOnly() throws {
    let p = try #require(FilesPlace.of(.session(SessionRef(project: "web", thread: "t-1", server: "mini")), sessionID: "mini/s-1", threadDone: false, readsWorktree: true, hasLibrary: true))
    #expect(p.session == nil)
    #expect(p.why?.contains("mini's worktrees stay on mini") == true)
    #expect(p.mentions == nil)
}

@Test func aDirectorAndAProgramHaveNoFiles() {
    var director = SessionRef(project: "launch")
    director.director = true
    #expect(FilesPlace.of(.session(director), sessionID: "s", threadDone: false, readsWorktree: true, hasLibrary: true) == nil)
    #expect(FilesPlace.of(.program("launch"), sessionID: nil, threadDone: false, readsWorktree: true, hasLibrary: true) == nil)
    #expect(FilesPlace.of(nil, sessionID: nil, threadDone: false, readsWorktree: true, hasLibrary: true) == nil)
    #expect(FilesPlace.of(.project("web"), sessionID: nil, threadDone: false, readsWorktree: true, hasLibrary: false) == nil)
}

@Test func theTreeWalksTheUnfoldedFolders() {
    let shut = FileTree.rows(dirs: tree, open: [])
    #expect(shut.map(\.id) == ["macos", "docs", "README.md"])
    let open = FileTree.rows(dirs: tree, open: ["macos", "macos/Sources"])
    #expect(open.map(\.id) == ["macos", "macos/Sources", "macos/Sources/a.swift", "macos/Makefile", "docs", "README.md"])
    #expect(open.map(\.depth) == [0, 1, 2, 1, 0, 0])
    // A folder inside a folded one doesn't list, though left unfolded.
    #expect(FileTree.rows(dirs: tree, open: ["macos/Sources"]).count == 3)
}

@Test func theFoldersToListAreTheTopThenTheUnfolded() {
    #expect(FileTree.folders(dirs: tree, open: []) == [""])
    #expect(FileTree.folders(dirs: tree, open: ["macos", "macos/Sources"]) == ["", "macos", "macos/Sources"])
    #expect(FileTree.folders(dirs: tree, open: ["docs", "new"]) == ["", "docs", "new"])
    let many = Set((0..<400).map { "d\($0)" })
    #expect(FileTree.folders(dirs: [:], open: many).count == FileTree.maxStamped)
}

@Test func leftAndRightFoldAndStep() {
    let open: Set<String> = ["macos"]
    let rows = FileTree.rows(dirs: tree, open: open)
    #expect(FileTree.fold(rows, at: "docs", right: true, open: open) == .open("docs"))
    #expect(FileTree.fold(rows, at: "macos", right: true, open: open) == .pick("macos/Sources"))
    #expect(FileTree.fold(rows, at: "README.md", right: true, open: open) == .nothing)
    #expect(FileTree.fold(rows, at: "macos", right: false, open: open) == .close("macos"))
    #expect(FileTree.fold(rows, at: "macos/Makefile", right: false, open: open) == .pick("macos"))
    #expect(FileTree.fold(rows, at: "README.md", right: false, open: open) == .nothing)
    #expect(FileTree.fold(rows, at: nil, right: true, open: open) == .nothing)
    // An unfolded empty folder has nowhere to step.
    let empty: [String: [AppAPI.FileEntry]] = ["": [dir("void"), file("z")], "void": []]
    let e = FileTree.rows(dirs: empty, open: ["void"])
    #expect(FileTree.fold(e, at: "void", right: true, open: ["void"]) == .nothing)
}

@Test func aFilesIDKeepsTabsApart() {
    let w = OpenedFile(.worktree(file("a/b.txt"), ignored: false))
    let a = OpenedFile(.attached(AppAPI.LibraryFile(thread: "t-1", title: "x", state: "open", name: "b.txt", size: 1, time: Date())))
    #expect(w.id == "w:a/b.txt")
    #expect(a.id == "a:t-1/b.txt")
    #expect(w.name == "b.txt")
    #expect(w.path == "a/b.txt")
}

@Test func paneTabsOpenOnceCloseToTheNextAndDropTheOldest() {
    var p = PaneFiles()
    #expect(p.current == nil)
    let fs = (0..<8).map { OpenedFile(.worktree(file("f\($0)"), ignored: false)) }
    p.open(fs[0])
    p.open(fs[1])
    p.open(fs[0])
    #expect(p.tabs.map(\.id) == ["w:f0", "w:f1"])
    #expect(p.shown == "w:f0")
    p.close("w:f0")
    #expect(p.shown == "w:f1")
    p.close("w:f1")
    #expect(p.shown == nil)
    #expect(p.tabs.isEmpty)
    for f in fs { p.open(f) }
    #expect(p.tabs.count == PaneFiles.limit)
    #expect(p.tabs.first?.id == "w:f2")
    p.shown = nil
    p.close("w:f7")
    #expect(p.shown == nil)
}

/// The cases of TestFileCaution in internal/tui/filespanel_test.go.
@Test func theCautionMatchesTheTUIsViewer() {
    #expect(FileCaution.of(name: "out.log", ignored: true) == "Git-ignored: may hold secrets")
    #expect(FileCaution.of(name: ".env", ignored: true) == "Git-ignored: may hold secrets")
    for name in [".env", ".env.production", "server.PEM", "id_ed25519", "secrets.yaml", "credentials.json", ".git-credentials", "secret.txt"] {
        #expect(FileCaution.of(name: name, ignored: false) == "Its name says it may hold secrets", "\(name)")
    }
    for name in [".env.example", "id_ed25519.pub", "keyboard.go", "README.md"] {
        #expect(FileCaution.of(name: name, ignored: false) == nil, "\(name)")
    }
}

@Test func whatWouldRunDoesntOpen() {
    for name in ["run.command", "A.APP", "x.sh", "evil.terminal", "go.webloc", "tool.pkg"] {
        #expect(FileCaution.launches(name), "\(name)")
    }
    // The Library tab and the Attached card ask the same rule as the Files panel.
    for name in ["dir/run.COMMAND", "a.tar.sh", "Mail.webloc"] {
        #expect(FileCaution.launches(name), "\(name)")
    }
    for name in ["a.png", "b.pdf", "c.md", "Makefile", ".env", "shell", "notes.sh.txt"] {
        #expect(!FileCaution.launches(name), "\(name)")
    }
}

@Test func binaryIsANulOrNotUTF8ButForACutEnd() {
    #expect(FileText.isBinary(Data([0x89, 0x50, 0x00])))
    #expect(FileText.isBinary(Data("caf".utf8) + Data([0xE9, 0x20, 0x61])))
    #expect(!FileText.isBinary(Data("plain".utf8)))
    #expect(!FileText.isBinary(Data()))
    // "é" is C3 A9: its first byte alone at the end is a cut, not binary.
    #expect(!FileText.isBinary(Data("caf".utf8) + Data([0xC3])))
    let euro = Array("€".utf8)
    #expect(!FileText.isBinary(Data("x".utf8) + Data(euro.prefix(2))))
    #expect(!FileText.isBinary(Data("x€".utf8)))
}

@Test func theTextDropsWhatCouldMisleadAndACutEnd() {
    #expect(FileText.text(Data("a\r\nb\tc\n".utf8)) == "a\nb\tc\n")
    #expect(FileText.text(Data("a\u{1B}[31mb".utf8)) == "a[31mb")
    #expect(FileText.text(Data("safe\u{202E}txt.exe\u{2066}".utf8)) == "safetxt.exe")
    #expect(FileText.text(Data("x".utf8) + Data(Array("€".utf8).prefix(2))) == "x")
    #expect(FileText.text(Data("émoji 🙂".utf8)) == "émoji 🙂")
}

@Test func aCutFileSaysSo() {
    #expect(FileText.cutNote(size: 10, shown: 10) == nil)
    #expect(FileText.cutNote(size: 3 << 20, shown: 1 << 20)?.hasPrefix("Shows the first") == true)
}

@Test func lineNumbersFindTheirLine() {
    let starts = FileText.lineStarts("ab\ncd\n\nef")
    #expect(starts == [0, 3, 6, 7])
    #expect(FileText.line(at: 0, in: starts) == 1)
    #expect(FileText.line(at: 2, in: starts) == 1)
    #expect(FileText.line(at: 3, in: starts) == 2)
    #expect(FileText.line(at: 6, in: starts) == 3)
    #expect(FileText.line(at: 8, in: starts) == 4)
}

private final class Chunks: @unchecked Sendable {
    var data: Data
    var binary = false
    var mtimes: [Date] = []
    var asked: [AppAPI.FilesReadParams] = []
    var error: (any Error)?
    init(_ data: Data) { self.data = data }

    func fetch(_ p: AppAPI.FilesReadParams) async throws -> AppAPI.FilesChunk {
        asked.append(p)
        if let error { throw error }
        let mtime = mtimes.isEmpty ? nil : mtimes.removeFirst()
        if binary && !p.binary {
            return AppAPI.FilesChunk(name: "f", path: p.path, size: data.count, mtime: mtime, offset: p.offset, binary: true)
        }
        let start = min(p.offset, data.count)
        let part = data[start..<min(start + 512 << 10, data.count)]
        return AppAPI.FilesChunk(name: "f", path: p.path, size: data.count, mtime: mtime, offset: p.offset, binary: binary, data: Data(part))
    }
}

@Test func aBodyReadsUpToTheCapAndSaysHowMuch() async {
    let big = Chunks(Data(repeating: 0x61, count: FileText.cap + 900))
    let body = await WorktreeRead.body(big.fetch, session: "s", path: "f", ignored: true)
    guard case .text(let text, let size, let shown, _) = body else { Issue.record("\(body)"); return }
    #expect(text.utf8.count == FileText.cap)
    #expect(size == FileText.cap + 900)
    #expect(shown == FileText.cap)
    #expect(big.asked.allSatisfy { $0.ignored && !$0.binary })
    let empty = Chunks(Data())
    #expect(await WorktreeRead.body(empty.fetch, session: "s", path: "f", ignored: false) == .text("", size: 0, shown: 0, mtime: nil))
}

@Test func aBinaryBodyHasNameAndSizeAndItsBytesWhenAsked() async {
    let pic = Chunks(Data([1, 0, 2]))
    pic.binary = true
    #expect(await WorktreeRead.body(pic.fetch, session: "s", path: "f", ignored: false) == .binary(size: 3, mtime: nil))
    #expect(await WorktreeRead.bytes(pic.fetch, session: "s", path: "f", ignored: false) == .data(Data([1, 0, 2]), nil))
    #expect(pic.asked.last?.binary == true)
}

@Test func anOlderServerThatSendsNoBytesSaysSo() async {
    let f: WorktreeRead.Fetch = { p in AppAPI.FilesChunk(name: "f", path: p.path, size: 3, offset: 0, binary: true) }
    guard case .unavailable(let why) = await WorktreeRead.bytes(f, session: "s", path: "f", ignored: false) else { Issue.record("no refusal"); return }
    #expect(why.contains("update"))
}

@Test func bytesOverWhatAReadGivesAreRefusedNotCut() async {
    let f: WorktreeRead.Fetch = { p in AppAPI.FilesChunk(name: "f", path: p.path, size: 30 << 20, offset: 0, truncated: true, data: Data([1])) }
    guard case .unavailable(let why) = await WorktreeRead.bytes(f, session: "s", path: "f", ignored: false) else { Issue.record("no refusal"); return }
    #expect(why.contains("Reveal in Finder"))
}

@Test func bytesReadAgainWhenTheFileChangesBetweenChunks() async {
    let f = Chunks(Data(repeating: 1, count: 600 << 10))
    let a = Date(timeIntervalSince1970: 1), b = Date(timeIntervalSince1970: 2)
    f.mtimes = [a, b, b, b, b]
    let out = await WorktreeRead.bytes(f.fetch, session: "s", path: "f", ignored: false)
    #expect(out == .data(f.data, b))
    #expect(f.asked.count == 4)
}

@Test func aGoneFileReadsAsGone() async {
    let f = Chunks(Data())
    f.error = AppAPI.Error(code: "gone", message: "gone")
    #expect(await WorktreeRead.body(f.fetch, session: "s", path: "f", ignored: false) == .gone)
    #expect(await WorktreeRead.stat(f.fetch, session: "s", path: "f", ignored: false) == .gone)
    #expect(await WorktreeRead.bytes(f.fetch, session: "s", path: "f", ignored: false) == .gone)
}

@Test func aStatAsksPastTheEnd() async {
    let f = Chunks(Data("abc".utf8))
    #expect(await WorktreeRead.stat(f.fetch, session: "s", path: "f", ignored: false) == .binary(size: 3, mtime: nil))
    #expect(f.asked.first?.offset == WorktreeRead.statOffset)
}

@Test func anAttachedFilesBytesBecomeABody() {
    #expect(WorktreeRead.body(attached: .gone) == .gone)
    #expect(WorktreeRead.body(attached: .data(Data([0]), nil)) == .binary(size: 1, mtime: nil))
    #expect(WorktreeRead.body(attached: .data(Data("hi".utf8), nil)) == .text("hi", size: 2, shown: 2, mtime: nil))
}

@Test func aMentionIsRelativeForAThreadAndWholeForACoordinator() {
    let e = file("macos/a.swift")
    #expect(FileMention.text(.worktree(e, ignored: false), root: "/w", relative: true) == "macos/a.swift")
    #expect(FileMention.text(.worktree(e, ignored: false), root: "/w", relative: false) == "/w/macos/a.swift")
    let f = AppAPI.LibraryFile(thread: "t-1", title: "x", state: "open", name: "n.md", size: 1, time: Date())
    #expect(FileMention.text(.attached(f), root: "", relative: false) == "n.md (attached by t-1)")
}

/// A coordinator's file names its repo in every read, and is its own tab:
/// two repos can hold the same path.
@Test func anOpenedFileStaysOnItsRepo() async throws {
    let e = AppAPI.FileEntry(name: "a.txt", path: "d/a.txt", kind: "file")
    let first = OpenedFile(.worktree(e, ignored: false))
    let second = OpenedFile(.worktree(e, ignored: false), repo: AppAPI.FilesRepoRef(path: "/r/two"))
    #expect(first.id == "w:d/a.txt")
    #expect(second.id != first.id)
    var pane = PaneFiles()
    pane.open(first)
    pane.open(second)
    #expect(pane.tabs.count == 2)
}

/// No repo chosen leaves the field out, so a bridge before app API 51,
/// which decodes strictly, still answers; one chosen goes as its path.
@Test func noRepoChosenIsLeftOutOfTheParams() throws {
    let plain = try AppAPI.encoder().encode(AppAPI.FilesListParams(session: "s-1"))
    #expect(!String(decoding: plain, as: UTF8.self).contains("repo"))
    let read = try AppAPI.encoder().encode(AppAPI.FilesReadParams(session: "s-1", path: "a"))
    #expect(!String(decoding: read, as: UTF8.self).contains("repo"))
    let stamp = try AppAPI.encoder().encode(AppAPI.FilesStampParams(session: "s-1"))
    #expect(!String(decoding: stamp, as: UTF8.self).contains("repo"))
    let chosen = try AppAPI.encoder().encode(AppAPI.FilesListParams(session: "s-1", repo: AppAPI.FilesRepoRef(path: "/r/two")))
    #expect(String(decoding: chosen, as: UTF8.self).contains(#""repo":{"path":"/r/two"}"#))
}

/// Every read of a chosen repo's file names that repo: the text, the bytes
/// and the refresh's look at it.
@Test func aReadNamesItsRepo() async {
    let repo = AppAPI.FilesRepoRef(path: "/r/two")
    let c = Chunks(Data("text".utf8))
    _ = await WorktreeRead.body(c.fetch, session: "s", repo: repo, path: "f", ignored: false)
    _ = await WorktreeRead.bytes(c.fetch, session: "s", repo: repo, path: "f", ignored: false)
    _ = await WorktreeRead.stat(c.fetch, session: "s", repo: repo, path: "f", ignored: false)
    #expect(!c.asked.isEmpty)
    #expect(c.asked.allSatisfy { $0.repo == repo })
    let plain = Chunks(Data("text".utf8))
    _ = await WorktreeRead.body(plain.fetch, session: "s", path: "f", ignored: false)
    #expect(plain.asked.allSatisfy { $0.repo == nil })
}

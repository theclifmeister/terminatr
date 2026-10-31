import Foundation

/// Whether this app and the tm its bridge runs fit (docs/SPEC.md §3.8),
/// from the bridge's hello: the app API tm speaks against the one the
/// app was generated for, then their releases. An older app works with
/// a newer tm, so only a tm below `minimumAPI` stops the app; the rest
/// is a line saying which one is behind. `tm update` updates both.
public enum Compatibility: Equatable, Sendable {
    case ok
    /// tm's app API is older than the oldest this app works with.
    case tmTooOld(tm: String, api: Int)
    /// tm is older than the app: what the app has that tm doesn't is off.
    case tmBehind(tm: String, app: String)
    /// The app is older than tm: what tm has that the app doesn't is unused.
    case appBehind(tm: String, app: String)

    /// The oldest app API this app works with: 2 has the writes and
    /// peer.list the windows and the menu bar use.
    public static let minimumAPI = 2

    /// The app API that has task.add: File › New Task… needs it.
    public static let taskAddAPI = 7

    /// The app API that has task.get and the threads' check and
    /// pr_state: the task view's notes for a board task need it.
    public static let taskGetAPI = 14

    /// The app API that has task.done, task.templates and task.add's
    /// template: the board's Done fold and New Task's templates need it.
    public static let boardAPI = 20

    /// The app API that has design.list: the project's Designs need it.
    public static let designsAPI = 31

    /// The app API that has sessions.watch: the session list pushed as
    /// it changes, so a question's card shows and goes at once.
    public static let sessionsWatchAPI = 32

    /// The app API that has session.file: the attachment cards need it.
    public static let sessionFileAPI = 11

    /// The app API that has coordinator.clear, project.active and
    /// session.list's context_hint: Save & Clear, Activate and Deactivate
    /// need it.
    public static let projectControlsAPI = 13

    /// Whether hello's tm saves and clears coordinators and activates
    /// projects from the app.
    public static func controlsProjects(_ hello: AppAPI.Hello) -> Bool {
        hello.appApi >= projectControlsAPI && hello.methods.contains(AppAPI.Methods.coordinatorClear.name)
            && hello.methods.contains(AppAPI.Methods.projectActive.name)
    }

    /// The app API that has session.upload: the composer's files need it.
    public static let uploadAPI = 15

    /// Whether hello's tm takes files for a prompt (session.upload).
    public static func takesFiles(_ hello: AppAPI.Hello) -> Bool {
        hello.appApi >= uploadAPI && hello.methods.contains(AppAPI.Methods.sessionUpload.name)
    }

    /// The app API that has guard.refusals and guard.decide: the
    /// refusals' cards under Needs you need it.
    public static let guardAPI = 18

    /// The app API that has project.pin, project.new,
    /// session.adopt.request, session.remote and link.send: the sidebar's
    /// and the sessions' acts (the TUI's P, +, O, prefix+r and M) need it.
    public static let sideActsAPI = 22

    /// Whether hello's tm pins, makes and adopts into projects and
    /// writes on their links from the app; its server too (side-acts).
    public static func actsOnSidebar(_ hello: AppAPI.Hello) -> Bool {
        hello.appApi >= sideActsAPI && hello.methods.contains(AppAPI.Methods.projectPin.name)
            && (hello.server?.caps ?? []).contains("side-acts")
    }

    /// Whether hello's tm turns a coordinator's remote control on or off
    /// from the app (session.remote).
    public static func controlsRemote(_ hello: AppAPI.Hello) -> Bool {
        hello.appApi >= sideActsAPI && hello.methods.contains(AppAPI.Methods.sessionRemote.name)
    }

    /// Whether hello's tm sends the conversation's files (session.file),
    /// its server too.
    public static func sendsFiles(_ hello: AppAPI.Hello) -> Bool {
        hello.appApi >= sessionFileAPI && hello.methods.contains(AppAPI.Methods.sessionFile.name)
            && (hello.server?.caps ?? []).contains("session-file")
    }

    /// Whether hello's tm lists the guard's refusals and answers them
    /// from the app (guard.refusals, guard.decide).
    public static func answersRefusals(_ hello: AppAPI.Hello) -> Bool {
        hello.appApi >= guardAPI && hello.methods.contains(AppAPI.Methods.guardDecide.name)
    }

    /// The app API that has the device methods: Settings › Devices
    /// needs it.
    public static let devicesAPI = 26

    /// Whether hello's tm lists, pairs and changes the paired devices
    /// from the app (device.list, device.pair …).
    public static func managesDevices(_ hello: AppAPI.Hello) -> Bool {
        hello.appApi >= devicesAPI && hello.methods.contains(AppAPI.Methods.devicePair.name)
            && hello.methods.contains(AppAPI.Methods.deviceList.name)
    }

    /// The app API that has memory.list, memory.read, library.list and
    /// library.read: a project's Memory and Library pages need it.
    public static let projectFilesAPI = 21

    /// Whether hello's tm reads a project's memory and library for the
    /// app, its server too.
    public static func readsProjectFiles(_ hello: AppAPI.Hello) -> Bool {
        hello.appApi >= projectFilesAPI && hello.methods.contains(AppAPI.Methods.memoryRead.name)
            && hello.methods.contains(AppAPI.Methods.libraryRead.name) && (hello.server?.caps ?? []).contains("project-files")
    }

    /// Whether hello's tm adds tasks from the app (task.add).
    public static func addsTasks(_ hello: AppAPI.Hello) -> Bool {
        hello.appApi >= taskAddAPI && hello.methods.contains(AppAPI.Methods.taskAdd.name)
    }

    /// Whether hello's tm reads a task whole (task.get).
    public static func getsTasks(_ hello: AppAPI.Hello) -> Bool {
        hello.appApi >= taskGetAPI && hello.methods.contains(AppAPI.Methods.taskGet.name)
    }

    /// The app API that has peer.pages-read, peer.library-list and
    /// peer.library-read: a paired server's project pages need it.
    public static let peerPagesAPI = 35

    /// Whether hello's tm reads a paired server's project pages, its
    /// server too (caps peer-pages): else every page of a paired
    /// server's project is "not shared".
    public static func readsPeerPages(_ hello: AppAPI.Hello) -> Bool {
        hello.appApi >= peerPagesAPI && hello.methods.contains(AppAPI.Methods.peerPagesRead.name)
            && hello.methods.contains(AppAPI.Methods.peerLibraryRead.name) && (hello.server?.caps ?? []).contains("peer-pages")
    }

    /// The app API that has peer.design-list: a paired server's
    /// project's designs need it (T888).
    public static let peerDesignsAPI = 36

    /// Whether hello's tm reads a paired server's project's designs, its
    /// server too (caps peer-designs): else its Designs are "not
    /// shared".
    public static func readsPeerDesigns(_ hello: AppAPI.Hello) -> Bool {
        hello.appApi >= peerDesignsAPI && hello.methods.contains(AppAPI.Methods.peerDesignList.name)
            && (hello.server?.caps ?? []).contains("peer-designs")
    }

    /// Whether hello's tm lists a project's designs, its server too
    /// (design.list, caps designs).
    public static func listsDesigns(_ hello: AppAPI.Hello) -> Bool {
        hello.appApi >= designsAPI && hello.methods.contains(AppAPI.Methods.designList.name)
            && (hello.server?.caps ?? []).contains("designs")
    }

    /// Whether hello's tm pushes the session list as it changes, its
    /// server too (sessions.watch, caps sessions-watch); else the app
    /// reads session.list on a timer.
    public static func watchesSessions(_ hello: AppAPI.Hello) -> Bool {
        hello.appApi >= sessionsWatchAPI && hello.methods.contains(AppAPI.Methods.sessionsWatch.name)
            && (hello.server?.caps ?? []).contains("sessions-watch")
    }

    /// Whether hello's tm lists the board's done tasks (task.done).
    public static func listsDone(_ hello: AppAPI.Hello) -> Bool {
        hello.appApi >= boardAPI && hello.methods.contains(AppAPI.Methods.taskDone.name)
    }

    /// Whether hello's tm lists task templates and adds a task with one
    /// (task.templates, task.add's template).
    public static func offersTemplates(_ hello: AppAPI.Hello) -> Bool {
        hello.appApi >= boardAPI && hello.methods.contains(AppAPI.Methods.taskTemplates.name)
            && hello.methods.contains(AppAPI.Methods.taskAdd.name)
    }

    /// Compares hello with this app, of release appVersion (its
    /// CFBundleShortVersionString, "0.54.0"; nil or 0.0.0 for a build
    /// from source, which compares by the app API only).
    public static func check(_ hello: AppAPI.Hello, appVersion: String?, appAPI: Int = AppAPI.version) -> Compatibility {
        let app = appVersion ?? ""
        if hello.appApi < minimumAPI {
            return .tmTooOld(tm: hello.tm, api: hello.appApi)
        }
        if hello.appApi < appAPI {
            return .tmBehind(tm: hello.tm, app: app)
        }
        if hello.appApi > appAPI {
            return .appBehind(tm: hello.tm, app: app)
        }
        guard let a = Release(app), a != Release(major: 0, minor: 0, patch: 0), let t = Release(hello.tm) else {
            return .ok
        }
        if t > a { return .appBehind(tm: hello.tm, app: app) }
        if a > t { return .tmBehind(tm: hello.tm, app: app) }
        return .ok
    }

    /// The app can't work with this tm.
    public var blocks: Bool {
        if case .tmTooOld = self { return true }
        return false
    }

    /// What the app says, nil when they fit.
    public var message: String? {
        switch self {
        case .ok:
            return nil
        case .tmTooOld(let tm, let api):
            return "tm \(tm) is too old for this app (app API \(api); it needs \(Self.minimumAPI) or later). Update it with tm update."
        case .tmBehind(let tm, let app):
            return "tm \(tm) is older than this app\(Self.version(app)): some of the app's features stay off until you update it with tm update."
        case .appBehind(let tm, let app):
            return "This app\(Self.version(app)) is older than tm \(tm): update it with tm update."
        }
    }

    /// The toolbar's few words, nil when they fit: message says the rest.
    public var headline: String? {
        switch self {
        case .ok: return nil
        case .tmTooOld(let tm, _): return "tm \(tm) is too old"
        case .tmBehind(let tm, _): return "tm \(tm) is behind"
        case .appBehind: return "This app is behind tm"
        }
    }

    private static func version(_ app: String) -> String {
        app.isEmpty || app == "0.0.0" ? "" : " (\(app))"
    }
}

/// A release's three numbers: "v0.54.0", "0.54.0", "v0.54.1-rc1" (the
/// suffix ignored). tm's source builds ("dev", "52421c6-dirty") have none.
struct Release: Comparable, Sendable {
    var major, minor, patch: Int

    init(major: Int, minor: Int, patch: Int) {
        (self.major, self.minor, self.patch) = (major, minor, patch)
    }

    init?(_ s: String) {
        var v = Substring(s)
        if v.hasPrefix("v") { v = v.dropFirst() }
        if let dash = v.firstIndex(of: "-") { v = v[..<dash] }
        let n = v.split(separator: ".", omittingEmptySubsequences: false).map { Int($0) }
        guard n.count == 3, let a = n[0], let b = n[1], let c = n[2], a >= 0, b >= 0, c >= 0 else { return nil }
        self.init(major: a, minor: b, patch: c)
    }

    static func < (l: Release, r: Release) -> Bool {
        (l.major, l.minor, l.patch) < (r.major, r.minor, r.patch)
    }
}

extension Compatibility {
    /// The app API that has mods.list, mods.set and mods.setting: a
    /// project's Mods page needs it (T869).
    public static let projectModsAPI = 38

    /// Whether hello's tm lists a project's mods, its server too (caps
    /// project-mods): the Mods page shows.
    public static func listsMods(_ hello: AppAPI.Hello) -> Bool {
        hello.appApi >= projectModsAPI && hello.methods.contains(AppAPI.Methods.modsList.name)
            && (hello.server?.caps ?? []).contains("project-mods")
    }

    /// Whether hello's tm turns a project's mods on or off and sets
    /// their settings here: the Mac app's, a device's at the write
    /// level.
    public static func changesMods(_ hello: AppAPI.Hello) -> Bool {
        listsMods(hello) && hello.methods.contains(AppAPI.Methods.modsSet.name)
            && hello.methods.contains(AppAPI.Methods.modsSetting.name)
    }
}

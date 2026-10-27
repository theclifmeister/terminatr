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

    /// The app API that has session.file: the attachment cards need it.
    public static let sessionFileAPI = 11

    /// The app API that has session.upload: the composer's files need it.
    public static let uploadAPI = 15

    /// Whether hello's tm takes files for a prompt (session.upload).
    public static func takesFiles(_ hello: AppAPI.Hello) -> Bool {
        hello.appApi >= uploadAPI && hello.methods.contains(AppAPI.Methods.sessionUpload.name)
    }

    /// Whether hello's tm sends the conversation's files (session.file),
    /// its server too.
    public static func sendsFiles(_ hello: AppAPI.Hello) -> Bool {
        hello.appApi >= sessionFileAPI && hello.methods.contains(AppAPI.Methods.sessionFile.name)
            && (hello.server?.caps ?? []).contains("session-file")
    }

    /// Whether hello's tm adds tasks from the app (task.add).
    public static func addsTasks(_ hello: AppAPI.Hello) -> Bool {
        hello.appApi >= taskAddAPI && hello.methods.contains(AppAPI.Methods.taskAdd.name)
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

import Foundation
#if canImport(MachO)
import MachO
#endif

/// The app's crash and hang reports (T957, app API 43): MetricKit's
/// diagnostics, which the system hands the app on a launch after the
/// crash, hang or exception. Each waits in a folder of the app's own
/// until the app is linked to its tm server, then goes there
/// (appreport.add) and nowhere else: the server keeps it for tm device
/// reports. A report the server refuses as such is dropped; one that
/// didn't get through waits for the next link.
public enum CrashReports {
    /// The kinds of report, MetricKit's diagnostics.
    public enum Kind: String, Sendable, CaseIterable {
        case crash
        case hang
        case cpuException = "cpu-exception"
        case diskWriteException = "disk-write-exception"
        case appLaunch = "app-launch"
    }

    /// The server's bound on a diagnostic's JSON (appreport.MaxPayload).
    public static let maxPayload = 512 << 10

    /// The params for one diagnostic: what MetricKit's JSON for it
    /// (jsonRepresentation) says of the app, the OS and the device (its
    /// diagnosticMetaData), a summary line, and the app binary's UUID
    /// from its call stacks (the frames of `executable`); failing that,
    /// `ownUUID` when the report is of this very build. A diagnostic over
    /// maxPayload keeps its metadata and loses its call stacks, saying
    /// so. `at` is when it happened (the payload's end).
    public static func params(kind: Kind, json: Data, at: Date?, executable: String, build: String, ownUUID: String?) -> AppAPI.AppReportAddParams {
        let obj = (try? JSONSerialization.jsonObject(with: json)) as? [String: Any] ?? [:]
        let meta = obj["diagnosticMetaData"] as? [String: Any] ?? [:]
        let appBuild = string(meta["appBuildVersion"])
        var uuid = binaryUUID(in: obj["callStackTree"], executable: executable) ?? ""
        if uuid.isEmpty, !appBuild.isEmpty, appBuild == build, let own = ownUUID {
            uuid = own
        }
        var payload = String(decoding: json, as: UTF8.self)
        if json.count > maxPayload {
            let cut: [String: Any] = ["diagnosticMetaData": meta, "terminatrCut": "MetricKit's diagnostic was \(json.count) bytes, over the server's \(maxPayload): its call stacks were left out"]
            payload = (try? JSONSerialization.data(withJSONObject: cut, options: [.sortedKeys])).map { String(decoding: $0, as: UTF8.self) } ?? "{}"
        }
        return AppAPI.AppReportAddParams(kind: kind.rawValue, time: at, appVersion: string(meta["appVersion"]), build: appBuild,
                                         binaryUuid: uuid, os: string(meta["osVersion"]), model: string(meta["deviceType"]),
                                         summary: summary(kind, meta), payload: payload)
    }

    /// One line on what happened, from the diagnostic's metadata.
    public static func summary(_ kind: Kind, _ meta: [String: Any]) -> String {
        var parts: [String] = []
        func add(_ label: String, _ key: String) {
            let v = string(meta[key])
            if !v.isEmpty { parts.append(label.isEmpty ? v : "\(label) \(v)") }
        }
        switch kind {
        case .crash:
            if let t = meta["exceptionType"].flatMap(int) {
                parts.append(exceptionName(t))
            }
            add("code", "exceptionCode")
            if let s = meta["signal"].flatMap(int) {
                parts.append(signalName(s))
            }
            if let r = meta["exceptionReason"] as? [String: Any] {
                let m = string(r["composedMessage"]).isEmpty ? string(r["className"]) : string(r["composedMessage"])
                if !m.isEmpty { parts.append(m) }
            }
            add("", "terminationReason")
        case .hang:
            add("hang", "hangDuration")
        case .cpuException:
            add("CPU", "totalCPUTime")
            add("over", "totalSampledTime")
        case .diskWriteException:
            add("wrote", "writesCaused")
        case .appLaunch:
            add("launch took", "launchDuration")
        }
        return String(parts.joined(separator: ", ").prefix(200))
    }

    /// The UUID of the first frame of `executable` in a call stack tree.
    static func binaryUUID(in tree: Any?, executable: String) -> String? {
        var stack: [Any] = [tree as Any]
        while let node = stack.popLast() {
            if let d = node as? [String: Any] {
                if string(d["binaryName"]) == executable, case let u = string(d["binaryUUID"]), !u.isEmpty {
                    return u
                }
                stack.append(contentsOf: d.values.reversed())
            } else if let a = node as? [Any] {
                stack.append(contentsOf: a.reversed())
            }
        }
        return nil
    }

    static func string(_ v: Any?) -> String {
        switch v {
        case let s as String: return s
        case let n as NSNumber: return n.stringValue
        default: return ""
        }
    }

    static func int(_ v: Any) -> Int? {
        (v as? NSNumber)?.intValue ?? (v as? String).flatMap { Int($0) }
    }

    /// Mach exception types, as crash reports name them.
    static func exceptionName(_ t: Int) -> String {
        let names = [1: "EXC_BAD_ACCESS", 2: "EXC_BAD_INSTRUCTION", 3: "EXC_ARITHMETIC", 4: "EXC_EMULATION", 5: "EXC_SOFTWARE",
                     6: "EXC_BREAKPOINT", 7: "EXC_SYSCALL", 8: "EXC_MACH_SYSCALL", 9: "EXC_RPC_ALERT", 10: "EXC_CRASH",
                     11: "EXC_RESOURCE", 12: "EXC_GUARD", 13: "EXC_CORPSE_NOTIFY"]
        return names[t] ?? "exception \(t)"
    }

    static func signalName(_ s: Int) -> String {
        let names = [4: "SIGILL", 5: "SIGTRAP", 6: "SIGABRT", 7: "SIGEMT", 8: "SIGFPE", 9: "SIGKILL", 10: "SIGBUS", 11: "SIGSEGV", 12: "SIGSYS", 13: "SIGPIPE"]
        return names[s] ?? "signal \(s)"
    }

    /// The main executable's LC_UUID, what its dSYM carries.
    public static func ownUUID() -> String? {
        #if canImport(MachO)
        guard let h = _dyld_get_image_header(0) else { return nil }
        var p = UnsafeRawPointer(h).advanced(by: MemoryLayout<mach_header_64>.size)
        for _ in 0..<h.pointee.ncmds {
            let lc = p.loadUnaligned(as: load_command.self)
            if lc.cmd == LC_UUID {
                let u = p.loadUnaligned(as: uuid_command.self)
                return UUID(uuid: u.uuid).uuidString
            }
            p = p.advanced(by: Int(lc.cmdsize))
        }
        #endif
        return nil
    }
}

/// The reports waiting for the server, one JSON file each in a folder of
/// the app's own, at most `limit` (the oldest go first).
public actor CrashReportQueue {
    public let folder: URL
    public let limit: Int
    private var sending = false

    public init(folder: URL, limit: Int = 20) {
        self.folder = folder
        self.limit = limit
    }

    /// Keeps p until it is sent.
    public func add(_ p: AppAPI.AppReportAddParams) {
        let fm = FileManager.default
        try? fm.createDirectory(at: folder, withIntermediateDirectories: true)
        guard let data = try? AppAPI.encoder().encode(p) else { return }
        let name = "\(Int(Date().timeIntervalSince1970 * 1000))-\(UUID().uuidString.prefix(8)).json"
        try? data.write(to: folder.appendingPathComponent(name), options: .atomic)
        let all = files()
        for f in all.dropLast(limit) {
            try? fm.removeItem(at: f)
        }
    }

    /// The waiting reports' files, oldest first.
    public func files() -> [URL] {
        let names = (try? FileManager.default.contentsOfDirectory(atPath: folder.path)) ?? []
        return names.filter { $0.hasSuffix(".json") }.sorted().map { folder.appendingPathComponent($0) }
    }

    /// Sends what waits, oldest first, through send, and forgets each the
    /// server took or refused as a report (bad-report); it stops at the
    /// first other failure, the rest waiting for the next link. It
    /// answers how many went. One flush at a time.
    @discardableResult
    public func flush(_ send: @Sendable (AppAPI.AppReportAddParams) async throws -> AppAPI.AppReportAdded) async -> Int {
        guard !sending else { return 0 }
        sending = true
        defer { sending = false }
        var sent = 0
        for f in files() {
            guard let data = try? Data(contentsOf: f), let p = try? AppAPI.decoder().decode(AppAPI.AppReportAddParams.self, from: data) else {
                try? FileManager.default.removeItem(at: f)
                continue
            }
            do {
                _ = try await send(p)
                sent += 1
            } catch let e as AppAPI.Error where e.code == "bad-report" {
                // the server won't ever take it
            } catch {
                return sent
            }
            try? FileManager.default.removeItem(at: f)
        }
        return sent
    }
}

#if canImport(MetricKit)
import MetricKit

/// MetricKit's subscriber: each diagnostic it hands the app joins the
/// queue, and the queue goes to the server whenever a sender is set (the
/// app is linked to a server that answers appreport.add).
public final class CrashReporter: NSObject, MXMetricManagerSubscriber, @unchecked Sendable {
    public typealias Send = @Sendable (AppAPI.AppReportAddParams) async throws -> AppAPI.AppReportAdded

    public let queue: CrashReportQueue
    private let lock = NSLock()
    /// The senders by link (a phone's paired servers, the Mac's one
    /// bridge): a report goes once, through the first that is set.
    private var senders: [String: Send] = [:]

    /// The app's reporter, its queue in its Application Support folder.
    public static let shared = CrashReporter(queue: CrashReportQueue(folder: defaultFolder()))

    public init(queue: CrashReportQueue) {
        self.queue = queue
    }

    /// Where the waiting reports live: the app's Application Support,
    /// under its bundle id on the Mac, whose folder is the user's.
    public static func defaultFolder() -> URL {
        let base = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first ?? FileManager.default.temporaryDirectory
        #if os(macOS)
        return base.appendingPathComponent(Bundle.main.bundleIdentifier ?? "dev.terminatr.mac").appendingPathComponent("CrashReports")
        #else
        return base.appendingPathComponent("CrashReports")
        #endif
    }

    /// Subscribes to MetricKit, early at launch: the diagnostics of an
    /// earlier run come soon after.
    public func start() {
        MXMetricManager.shared.add(self)
    }

    /// Sets how reports reach the server of link `key` (nil while it
    /// isn't linked) and, when set, sends what waits.
    public func setSender(_ s: Send?, for key: String) {
        lock.withLock { senders[key] = s }
        if s != nil {
            Task { await flush() }
        }
    }

    /// Sends what waits, if a sender is set.
    public func flush() async {
        guard let s = lock.withLock({ senders.sorted { $0.key < $1.key }.first?.value }) else { return }
        await queue.flush(s)
    }

    public func didReceive(_ payloads: [MXDiagnosticPayload]) {
        let ps = payloads.flatMap(Self.params)
        guard !ps.isEmpty else { return }
        Task {
            for p in ps {
                await queue.add(p)
            }
            await flush()
        }
    }

    /// One payload's diagnostics as reports.
    static func params(_ payload: MXDiagnosticPayload) -> [AppAPI.AppReportAddParams] {
        let exe = Bundle.main.executableURL?.lastPathComponent ?? ""
        let build = Bundle.main.infoDictionary?["CFBundleVersion"] as? String ?? ""
        let own = CrashReports.ownUUID()
        var out: [AppAPI.AppReportAddParams] = []
        func add(_ kind: CrashReports.Kind, _ ds: [MXDiagnostic]?) {
            for d in ds ?? [] {
                var p = CrashReports.params(kind: kind, json: d.jsonRepresentation(), at: payload.timeStampEnd, executable: exe, build: build, ownUUID: own)
                if p.appVersion.isEmpty { p.appVersion = d.applicationVersion }
                if p.build.isEmpty {
                    p.build = d.metaData.applicationBuildVersion
                    if p.binaryUuid.isEmpty, p.build == build, let own { p.binaryUuid = own }
                }
                if p.os.isEmpty { p.os = d.metaData.osVersion }
                if p.model.isEmpty { p.model = d.metaData.deviceType }
                out.append(p)
            }
        }
        add(.crash, payload.crashDiagnostics)
        add(.hang, payload.hangDiagnostics)
        add(.cpuException, payload.cpuExceptionDiagnostics)
        add(.diskWriteException, payload.diskWriteExceptionDiagnostics)
        #if os(iOS)
        // the Mac has no launch diagnostics
        add(.appLaunch, payload.appLaunchDiagnostics)
        #endif
        return out
    }
}
#endif

import Foundation

// The Mac app's only: iOS runs no child processes (the phone's
// transport is a device link, docs/SPEC.md §3.8).
#if os(macOS)

/// tm app bridge as the app's child: its stdin and stdout are the lines,
/// its stderr the reason when it ends unasked. It opens no listener:
/// the bridge dials the human socket, and starts the server through
/// tm's launchd job when none answers, never as the app's child.
public final class ProcessTransport: BridgeTransport, @unchecked Sendable {
    public let lines: AsyncThrowingStream<Data, any Error>

    private let process = Process()
    private let stdin = Pipe()
    private let lock = NSLock()
    private var buffer = Data()
    private var stderr = Data()
    private var closing = false

    /// Keeps this much of the bridge's stderr, its last words.
    private static let stderrCap = 4096

    /// What a tm session puts in its processes' environment (SPEC §3.4)
    /// to make tm one of its agents: on the agent socket, as that
    /// session. An app opened from a session's pane inherits them.
    public static let sessionVariables: Set<String> = [
        "TERMINATR", "TERMINATR_SESSION", "TERMINATR_SOCKET", "TERMINATR_BIN",
        "TERMINATR_ROLE", "TERMINATR_PROJECT", "TERMINATR_THREAD",
    ]

    /// The bridge's environment: base without the session variables, so
    /// the bridge dials the human socket as the human however the app
    /// was opened. TERMINATR_HOME stays: it picks the server (a test
    /// one, say), and a session's is its own server's home anyway.
    public static func bridgeEnvironment(_ base: [String: String]) -> [String: String] {
        base.filter { !sessionVariables.contains($0.key) }
    }

    /// environment nil: the app's own.
    public init(tm: URL, environment: [String: String]? = nil) throws {
        let stdout = Pipe()
        let errPipe = Pipe()
        process.executableURL = tm
        process.arguments = ["app", "bridge"]
        process.environment = Self.bridgeEnvironment(environment ?? ProcessInfo.processInfo.environment)
        process.standardInput = stdin
        process.standardOutput = stdout
        process.standardError = errPipe
        // A write after the bridge died fails with EPIPE instead of
        // killing the app with SIGPIPE.
        _ = fcntl(stdin.fileHandleForWriting.fileDescriptor, F_SETNOSIGPIPE, 1)

        let (lines, sink) = AsyncThrowingStream<Data, any Error>.makeStream()
        self.lines = lines
        errPipe.fileHandleForReading.readabilityHandler = { [weak self] h in
            let d = h.availableData
            guard let self, !d.isEmpty else {
                h.readabilityHandler = nil
                return
            }
            self.lock.withLock {
                self.stderr.append(d)
                if self.stderr.count > Self.stderrCap {
                    self.stderr = self.stderr.suffix(Self.stderrCap)
                }
            }
        }
        stdout.fileHandleForReading.readabilityHandler = { [weak self] h in
            let d = h.availableData
            guard let self else { return }
            if d.isEmpty {
                h.readabilityHandler = nil
                self.process.waitUntilExit()
                sink.finish(throwing: self.ended())
                return
            }
            for line in self.split(d) {
                sink.yield(line)
            }
        }
        do {
            try process.run()
        } catch {
            stdout.fileHandleForReading.readabilityHandler = nil
            errPipe.fileHandleForReading.readabilityHandler = nil
            throw BridgeError.closed("Couldn't run \(tm.path): \(error.localizedDescription)")
        }
    }

    public func send(_ line: Data) throws {
        var l = line
        l.append(0x0A)
        try stdin.fileHandleForWriting.write(contentsOf: l)
    }

    public func close() {
        lock.withLock { closing = true }
        try? stdin.fileHandleForWriting.close()
    }

    /// The complete lines in buffer plus d; the rest waits for more.
    private func split(_ d: Data) -> [Data] {
        lock.withLock {
            buffer.append(d)
            var out: [Data] = []
            while let nl = buffer.firstIndex(of: 0x0A) {
                out.append(buffer[buffer.startIndex..<nl])
                buffer.removeSubrange(buffer.startIndex...nl)
            }
            buffer = Data(buffer) // reindex from 0
            return out
        }
    }

    /// Why the stream ends: nothing when the app closed it, else the
    /// bridge's exit and the last of its stderr.
    private func ended() -> (any Error)? {
        lock.withLock {
            if closing && process.terminationStatus == 0 { return nil }
            let words = String(decoding: stderr, as: UTF8.self).trimmingCharacters(in: .whitespacesAndNewlines)
            if words.contains(#"unknown command "app""#) {
                return BridgeError.closed("This tm has no app bridge (tm app bridge): update it with tm update or brew upgrade terminatr.")
            }
            let status = "tm app bridge exited (\(process.terminationStatus))"
            return BridgeError.closed(words.isEmpty ? status + "." : status + ": " + words)
        }
    }
}
#endif

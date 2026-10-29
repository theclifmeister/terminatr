import Foundation
import Observation

/// The files a composer's prompt carries (docs/SPEC.md §3.8), as both
/// apps show them: each a chip, sent to tm as it comes in (session.upload
/// through call), its progress, its ref once tm has it, or why it
/// failed. A failed upload keeps its bytes: Retry sends it again, from
/// where it got to while tm keeps it; one a dropped link cut (dropped)
/// waits for resumeDropped, which the phone calls as its link comes
/// back. The phone keeps a session's tray across reconnects and screens
/// (PhoneModel); the Mac's composer holds its own.
@MainActor @Observable
public final class ComposerTray {
    /// session.upload, the session filled in by the owner.
    public typealias Call = @MainActor (AppAPI.UploadParams) async throws -> AppAPI.Upload

    public private(set) var files: [ComposerFile] = []
    /// Why the last file wasn't added (the prompt is full), until the
    /// next one is.
    public private(set) var note: String?
    @ObservationIgnored public var call: Call?
    /// Whether an error is the link dropping: such a file waits for
    /// resumeDropped instead of saying Retry alone.
    @ObservationIgnored public var dropped: @MainActor (any Error) -> Bool = { _ in false }
    @ObservationIgnored private let chunk: Int
    @ObservationIgnored private var payloads: [UUID: Payload] = [:]
    @ObservationIgnored private var tasks: [UUID: Task<Void, Never>] = [:]

    private struct Payload {
        let data: Data
        var resume = ComposerFiles.Resume()
        /// The link dropped under it: resumeDropped sends it again.
        var interrupted = false
    }

    public init(chunk: Int = ComposerFiles.chunk, call: Call? = nil) {
        self.chunk = chunk
        self.call = call
    }

    /// The refs of the files tm has, for session.prompt's files.
    public var refs: [String] { files.compactMap(\.ref) }

    public var uploading: Bool { files.contains(where: \.uploading) }

    /// Whether another file fits the prompt.
    public var full: Bool { files.count >= ComposerFiles.maxFiles }

    /// Adds a file of name and its bytes, and sends it to tm; one tm
    /// won't take shows as a chip saying why. False when the prompt is
    /// full (note says so).
    @discardableResult
    public func add(name: String, data: Data) -> Bool {
        guard !full else { return refuseFull() }
        note = nil
        if let why = ComposerFiles.refusal(name: name, size: data.count) {
            files.append(ComposerFile(name: name, size: data.count, state: .failed(why)))
            return true
        }
        let f = ComposerFile(name: name, size: data.count)
        files.append(f)
        payloads[f.id] = Payload(data: data)
        start(f.id)
        return true
    }

    /// Adds a chip for a file that can't go (a folder, one that couldn't
    /// be read), saying why.
    @discardableResult
    public func refuse(name: String, size: Int = 0, why: String) -> Bool {
        guard !full else { return refuseFull() }
        note = nil
        files.append(ComposerFile(name: name, size: size, state: .failed(why)))
        return true
    }

    private func refuseFull() -> Bool {
        note = "A prompt takes at most \(ComposerFiles.maxFiles) files."
        return false
    }

    /// Takes the file off the prompt, stopping its upload.
    public func remove(_ id: UUID) {
        tasks.removeValue(forKey: id)?.cancel()
        payloads[id] = nil
        files.removeAll { $0.id == id }
        note = nil
    }

    /// Sends a failed file again, from where tm has it to.
    public func retry(_ id: UUID) {
        guard let i = files.firstIndex(where: { $0.id == id }), files[i].failed, payloads[id] != nil else { return }
        payloads[id]?.interrupted = false
        start(id)
    }

    /// Sends again the files the link's drop cut: the link is back.
    public func resumeDropped() {
        for (id, p) in payloads where p.interrupted { retry(id) }
    }

    /// Empties the tray: the prompt went with its files.
    public func clear() {
        for t in tasks.values { t.cancel() }
        tasks = [:]
        payloads = [:]
        files = []
        note = nil
    }

    private func start(_ id: UUID) {
        guard let p = payloads[id], let name = files.first(where: { $0.id == id })?.name else { return }
        set(id, .uploading(p.resume.offset), canRetry: false)
        tasks[id] = Task { [weak self] in
            await self?.send(id, name: name, payload: p)
        }
    }

    private func send(_ id: UUID, name: String, payload p: Payload) async {
        guard let call else {
            finish(id, .failed("Not connected to terminatr"), retry: true)
            return
        }
        var resume = p.resume
        do {
            let r: AppAPI.Upload
            do {
                r = try await ComposerFiles.upload(p.data, name: name, session: "", chunk: chunk, from: resume, call: call) { at in
                    resume = at
                    self.payloads[id]?.resume = at
                    self.set(id, .uploading(at.offset))
                }
            } catch let e as AppAPI.Error where !resume.upload.isEmpty && e.code == "bad-params" {
                // tm no longer has it there (it waited too long, or a
                // chunk landed as the link dropped): from the start.
                payloads[id]?.resume = ComposerFiles.Resume()
                set(id, .uploading(0))
                r = try await ComposerFiles.upload(p.data, name: name, session: "", chunk: chunk, call: call) { at in
                    self.payloads[id]?.resume = at
                    self.set(id, .uploading(at.offset))
                }
            }
            payloads[id] = nil
            finish(id, .ready(ref: r.ref), retry: false)
        } catch is CancellationError {
            return
        } catch {
            guard !Task.isCancelled else { return }
            let drop = dropped(error)
            payloads[id]?.interrupted = drop
            finish(id, .failed(drop ? "The link dropped: it goes on once it is back" : error.localizedDescription), retry: true)
        }
    }

    private func finish(_ id: UUID, _ state: ComposerFile.State, retry: Bool) {
        tasks[id] = nil
        set(id, state, canRetry: retry && payloads[id] != nil)
    }

    private func set(_ id: UUID, _ state: ComposerFile.State, canRetry: Bool? = nil) {
        guard let i = files.firstIndex(where: { $0.id == id }) else { return }
        files[i].state = state
        if let canRetry { files[i].canRetry = canRetry }
    }
}

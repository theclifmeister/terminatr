import AppKit
import CoreImage
import CoreImage.CIFilterBuiltins
import SwiftUI
import TerminatrKit

/// Settings › Devices (T790, app API 26): the phones paired with this
/// server, each with its link and level, Rename…, Remove… (asked first)
/// and Pair iPhone…, as tm device does. The bridge answers these itself,
/// for the user only: never a phone's, at any level.
struct DeviceSettings: View {
    @Environment(AppModel.self) private var model
    @State private var list: AppAPI.Devices?
    @State private var failure: String?
    @State private var pairing = false
    @State private var renaming: AppAPI.Device?
    @State private var newName = ""
    @State private var removing: AppAPI.Device?
    /// Bumped to read the list again at once.
    @State private var reload = 0

    /// How often the list is read again: the links change on their own.
    private static let every: Duration = .seconds(3)

    private var manages: Bool {
        if case .connected(let h) = model.phase { return Compatibility.managesDevices(h) }
        return false
    }

    var body: some View {
        Form {
            if manages {
                DeviceList(list: list, failure: failure, setLevel: setLevel, rename: { d in
                    newName = d.name
                    renaming = d
                }, remove: { removing = $0 })
                Section {
                    HStack {
                        Spacer()
                        Button("Pair iPhone…") { pairing = true }
                    }
                } footer: {
                    Text("A paired iPhone reads what this Mac's terminatr shows, through the relay, and answers what you allow it to. Only you pair devices, here or with tm device pair.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            } else {
                Section {
                    Text(model.isConnected
                        ? "This tm can't pair devices from the app: update it with tm update, or use tm device pair in a terminal."
                        : "Not connected to terminatr.")
                        .foregroundStyle(.secondary)
                }
            }
        }
        .formStyle(.grouped)
        .fixedSize(horizontal: false, vertical: true)
        .task(id: "\(manages) \(reload)") {
            guard manages else { return }
            while !Task.isCancelled {
                await read()
                try? await Task.sleep(for: Self.every)
            }
        }
        .sheet(isPresented: $pairing, onDismiss: { reload += 1 }) {
            PairPhoneSheet()
        }
        .alert("Rename Device", isPresented: Binding(get: { renaming != nil }, set: { if !$0 { renaming = nil } }), presenting: renaming) { d in
            TextField("Name", text: $newName)
            Button("Rename") { rename(d, to: newName) }
                .disabled(newName.trimmingCharacters(in: .whitespaces).isEmpty)
            Button("Cancel", role: .cancel) {}
        } message: { _ in
            Text("Its link connects again under the new name; the journal names it so.")
        }
        .confirmationDialog(removing.map { "Remove “\($0.name)”?" } ?? "",
                            isPresented: Binding(get: { removing != nil }, set: { if !$0 { removing = nil } }), presenting: removing) { d in
            Button("Remove", role: .destructive) { remove(d) }
            Button("Cancel", role: .cancel) {}
        } message: { _ in
            Text("Its link closes now and its key is no longer taken; to use it again, pair it again. On the phone, Forget this server deletes its keys.")
        }
    }

    private func read() async {
        do {
            list = try await model.connected().call(AppAPI.Methods.deviceList)
            failure = nil
        } catch {
            failure = DeviceWords.error(error)
        }
    }

    private func act(_ f: @escaping () async throws -> Void) {
        Task {
            do {
                try await f()
                failure = nil
            } catch {
                failure = DeviceWords.error(error)
            }
            await read()
        }
    }

    private func setLevel(_ d: AppAPI.Device, _ level: String) {
        act { _ = try await model.connected().call(AppAPI.Methods.deviceAccess, AppAPI.DeviceAccessParams(device: d.id, level: level)) }
    }

    private func rename(_ d: AppAPI.Device, to name: String) {
        let name = name.trimmingCharacters(in: .whitespaces)
        act { _ = try await model.connected().call(AppAPI.Methods.deviceName, AppAPI.DeviceNameParams(device: d.id, name: name)) }
    }

    private func remove(_ d: AppAPI.Device) {
        act { _ = try await model.connected().call(AppAPI.Methods.deviceRemove, AppAPI.DeviceParams(device: d.id)) }
    }
}

/// The devices' section: each device's name, link, id and relay, its
/// level (View or Answer, and why it can't answer), and its menu.
struct DeviceList: View {
    let list: AppAPI.Devices?
    let failure: String?
    let setLevel: (AppAPI.Device, String) -> Void
    let rename: (AppAPI.Device) -> Void
    let remove: (AppAPI.Device) -> Void

    var body: some View {
        Section {
            if let list, !list.devices.isEmpty {
                ForEach(list.devices) { d in
                    row(d, running: list.running, levels: list.levels)
                }
            } else if list != nil {
                Text("No devices yet: Pair iPhone… shows a code to scan with the terminatr app.")
                    .foregroundStyle(.secondary)
            } else if failure == nil {
                Text("Reading the devices…")
                    .foregroundStyle(.secondary)
            }
            if let failure {
                Label(failure, systemImage: "exclamationmark.triangle")
                    .foregroundStyle(.red)
                    .font(.callout)
            }
        } header: {
            Text("Devices")
        }
    }

    private func row(_ d: AppAPI.Device, running: Bool, levels: [String]) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(alignment: .center, spacing: 10) {
                Image(systemName: "iphone")
                    .font(.title2)
                    .foregroundStyle(.secondary)
                    .frame(width: 22)
                VStack(alignment: .leading, spacing: 2) {
                    Text(d.name)
                        .font(.headline)
                    HStack(spacing: 5) {
                        Circle()
                            .fill(DeviceWords.connected(d) ? Color.green : Color.secondary.opacity(0.5))
                            .frame(width: 7, height: 7)
                        Text(DeviceWords.link(d, running: running))
                    }
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    Text("Device id \(d.short) · \(d.relay)")
                        .font(.caption)
                        .foregroundStyle(.tertiary)
                        .textSelection(.enabled)
                }
                Spacer(minLength: 8)
                Picker("Access", selection: Binding(get: { levels.contains(d.level) ? d.level : d.effective },
                                                     set: { if $0 != d.level { setLevel(d, $0) } })) {
                    ForEach(levels, id: \.self) { l in
                        Text(l == DeviceWords.view ? "View" : DeviceWords.level(l)).tag(l)
                    }
                }
                .pickerStyle(.segmented)
                .labelsHidden()
                .fixedSize()
                .disabled(!d.writeKey)
                .help(DeviceWords.levelHelp(d.effective))
                Menu {
                    Button("Rename…") { rename(d) }
                    Divider()
                    Button("Remove…", role: .destructive) { remove(d) }
                } label: {
                    Image(systemName: "ellipsis.circle")
                }
                .menuStyle(.borderlessButton)
                .menuIndicator(.hidden)
                .fixedSize()
                .accessibilityLabel("More for \(d.name)")
            }
            Text(DeviceWords.limit(d) ?? DeviceWords.levelHelp(d.effective))
                .font(.caption)
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
                .padding(.leading, 32)
        }
        .padding(.vertical, 2)
    }
}

/// Where a pairing is, as the Pair iPhone… sheet shows it.
enum PairStep {
    /// Asking the bridge for a code.
    case starting
    /// No relay is known: the user gives one.
    case needsRelay
    /// The code works: its QR code, until the phone joins.
    case waiting(AppAPI.DevicePairing)
    /// The phone joined: tm device pair's confirmation.
    case offer(AppAPI.DevicePairing, AppAPI.DeviceOffer)
    /// The user said yes; the pairing finishes.
    case adding
    case paired(AppAPI.Device)
    case failed(String)
}

/// Pair iPhone…: a code from device.pair shown as a QR code with its
/// countdown and Cancel, then, once the phone joined, the confirmation
/// tm device pair asks, under a name the user may change.
struct PairPhoneSheet: View {
    @Environment(AppModel.self) private var model
    @Environment(\.dismiss) private var dismiss
    @State private var step = PairStep.starting
    @State private var relay = ""
    @State private var name = ""
    @State private var nameError: String?
    @State private var run: Task<Void, Never>?

    var body: some View {
        PairContent(step: step, relay: $relay, name: $name, nameError: nameError,
                    start: start, cancel: cancel, add: add, done: { dismiss() })
            .onAppear(perform: start)
            .onDisappear {
                run?.cancel()
                answerNo()
            }
    }

    /// The open pairing's id, while one waits on the phone or the user.
    private var open: String? {
        switch step {
        case .waiting(let p), .offer(let p, _): p.pairing
        default: nil
        }
    }

    private func start() {
        run?.cancel()
        step = .starting
        nameError = nil
        let relay = relay.trimmingCharacters(in: .whitespaces)
        run = Task {
            do {
                let b = try model.connected()
                let p = try await b.call(AppAPI.Methods.devicePair, AppAPI.DevicePairParams(relay: relay))
                guard !Task.isCancelled else { return }
                step = .waiting(p)
                let o = try await b.call(AppAPI.Methods.devicePairWait, AppAPI.DevicePairingParams(pairing: p.pairing))
                guard !Task.isCancelled else { return }
                name = o.name
                step = .offer(p, o)
            } catch let e as AppAPI.Error where e.code == "relay-needed" {
                step = .needsRelay
            } catch {
                if !Task.isCancelled { step = .failed(DeviceWords.error(error)) }
            }
        }
    }

    /// No to the open pairing: it ends, whether the phone joined or not.
    private func answerNo() {
        guard let id = open, let b = try? model.connected() else { return }
        Task { _ = try? await b.call(AppAPI.Methods.devicePairAnswer, AppAPI.DevicePairAnswerParams(pairing: id, accept: false)) }
    }

    private func cancel() {
        run?.cancel()
        answerNo()
        step = .starting
        dismiss()
    }

    private func add() {
        guard case .offer(let p, let o) = step else { return }
        let name = name.trimmingCharacters(in: .whitespaces)
        nameError = nil
        step = .adding
        Task {
            do {
                let r = try await model.connected().call(AppAPI.Methods.devicePairAnswer,
                                                         AppAPI.DevicePairAnswerParams(pairing: p.pairing, accept: true, name: name == o.name ? "" : name))
                step = r.device.map { .paired($0) } ?? .failed("Not paired.")
            } catch let e as AppAPI.Error where e.code == "name-taken" || e.code == "bad-params" {
                // The offer still waits: another name.
                nameError = DeviceWords.error(e)
                step = .offer(p, o)
            } catch {
                step = .failed(DeviceWords.error(error))
            }
        }
    }
}

/// The sheet's content for a step, apart from the bridge.
struct PairContent: View {
    let step: PairStep
    @Binding var relay: String
    @Binding var name: String
    let nameError: String?
    let start: () -> Void
    let cancel: () -> Void
    let add: () -> Void
    let done: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 16) {
            Text("Pair iPhone")
                .font(.title2.weight(.semibold))
            content
        }
        .padding(24)
        .frame(width: 440)
    }

    @ViewBuilder private var content: some View {
        switch step {
        case .starting:
            HStack(spacing: 8) {
                ProgressView().controlSize(.small)
                Text("Making a code…").foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity, minHeight: 120)
            buttons { Button("Cancel", role: .cancel, action: cancel).keyboardShortcut(.cancelAction) }
        case .needsRelay:
            Text("The phone and this Mac meet on a relay, and none is set up yet. Give its address, as tm device pair --relay takes it.")
                .fixedSize(horizontal: false, vertical: true)
            TextField("Relay", text: $relay, prompt: Text("wss://relay.example.com"))
                .textFieldStyle(.roundedBorder)
                .onSubmit(start)
            buttons {
                Button("Cancel", role: .cancel, action: cancel).keyboardShortcut(.cancelAction)
                Button("Make Code", action: start)
                    .keyboardShortcut(.defaultAction)
                    .disabled(relay.trimmingCharacters(in: .whitespaces).isEmpty)
            }
        case .waiting(let p):
            waiting(p)
        case .offer(_, let o):
            offer(o)
        case .adding:
            HStack(spacing: 8) {
                ProgressView().controlSize(.small)
                Text("Adding it…").foregroundStyle(.secondary)
            }
            .frame(maxWidth: .infinity, minHeight: 120)
        case .paired(let d):
            Label("Paired “\(d.name)”, device id \(d.short).", systemImage: "checkmark.circle.fill")
                .symbolRenderingMode(.multicolor)
                .font(.headline)
            Text(d.writeKey
                ? "It can view now. To let it answer questions and approvals, choose Answer in the list."
                : "It can view. Its app has no write key, so it can't answer: pair it again from an app that has one.")
                .foregroundStyle(.secondary)
                .fixedSize(horizontal: false, vertical: true)
            buttons { Button("Done", action: done).keyboardShortcut(.defaultAction) }
        case .failed(let why):
            Label(why, systemImage: "exclamationmark.triangle")
                .foregroundStyle(.red)
                .fixedSize(horizontal: false, vertical: true)
            buttons {
                Button("Close", role: .cancel, action: cancel).keyboardShortcut(.cancelAction)
                Button("New Code", action: start).keyboardShortcut(.defaultAction)
            }
        }
    }

    private func waiting(_ p: AppAPI.DevicePairing) -> some View {
        TimelineView(.periodic(from: .now, by: 1)) { ctx in
            let expired = ctx.date >= p.expires
            VStack(alignment: .leading, spacing: 14) {
                HStack(alignment: .top, spacing: 18) {
                    QRCodeView(text: p.invite)
                        .frame(width: 200, height: 200)
                        .opacity(expired ? 0.15 : 1)
                        .accessibilityLabel("Pairing code")
                    VStack(alignment: .leading, spacing: 10) {
                        Text("Scan this with the terminatr app on the iPhone.")
                            .font(.headline)
                            .fixedSize(horizontal: false, vertical: true)
                        Text("It works once. You confirm the phone here before it is added.")
                            .foregroundStyle(.secondary)
                            .fixedSize(horizontal: false, vertical: true)
                        Spacer(minLength: 0)
                        if expired {
                            Text("The code's time ran out.")
                                .foregroundStyle(.secondary)
                        } else {
                            HStack(spacing: 6) {
                                ProgressView().controlSize(.small)
                                Text("Waiting for the phone…")
                            }
                            Text("Works for \(DeviceWords.countdown(to: p.expires, now: ctx.date))")
                                .font(.callout.monospacedDigit())
                                .foregroundStyle(.secondary)
                        }
                    }
                    .frame(maxHeight: .infinity, alignment: .top)
                }
                .frame(height: 200)
                Text("This Mac: \(p.name), server id \(p.server), through \(p.relay).")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .textSelection(.enabled)
                if !p.note.isEmpty {
                    Label(p.note, systemImage: "info.circle")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .fixedSize(horizontal: false, vertical: true)
                }
                buttons {
                    Button("Cancel", role: .cancel, action: cancel).keyboardShortcut(.cancelAction)
                    if expired {
                        Button("New Code", action: start).keyboardShortcut(.defaultAction)
                    }
                }
            }
        }
    }

    private func offer(_ o: AppAPI.DeviceOffer) -> some View {
        TimelineView(.periodic(from: .now, by: 1)) { ctx in
            VStack(alignment: .leading, spacing: 12) {
                Label("A phone joined", systemImage: "iphone.radiowaves.left.and.right")
                    .font(.headline)
                ForEach(DeviceWords.offer(o), id: \.self) { line in
                    Text(line)
                        .fixedSize(horizontal: false, vertical: true)
                }
                TextField("Add it as", text: $name)
                    .textFieldStyle(.roundedBorder)
                    .onSubmit(add)
                if let nameError {
                    Text(nameError)
                        .font(.caption)
                        .foregroundStyle(.red)
                }
                Text(DeviceWords.question(name: name.trimmingCharacters(in: .whitespaces), level: o.level))
                    .font(.headline)
                    .fixedSize(horizontal: false, vertical: true)
                Text("Only a device you just scanned the code with. Answer within \(DeviceWords.countdown(to: o.answerBy, now: ctx.date)).")
                    .font(.caption.monospacedDigit())
                    .foregroundStyle(.secondary)
                buttons {
                    Button("Don't Add", role: .cancel, action: cancel).keyboardShortcut(.cancelAction)
                    Button("Add", action: add)
                        .keyboardShortcut(.defaultAction)
                        .disabled(name.trimmingCharacters(in: .whitespaces).isEmpty)
                }
            }
        }
    }

    private func buttons(@ViewBuilder _ b: () -> some View) -> some View {
        HStack {
            Spacer()
            b()
        }
    }
}

/// A QR code of text, as Core Image draws it (level M, the error
/// correction tm device pair's code has): dark on light whatever the
/// appearance, with its quiet zone, as a camera reads it best.
struct QRCodeView: View {
    let text: String

    var body: some View {
        if let image = Self.image(text) {
            Image(nsImage: image)
                .interpolation(.none)
                .resizable()
                .scaledToFit()
                .padding(10)
                .background(.white, in: RoundedRectangle(cornerRadius: 8))
        } else {
            Text("The code can't be drawn.")
                .foregroundStyle(.secondary)
        }
    }

    static func image(_ text: String) -> NSImage? {
        let f = CIFilter.qrCodeGenerator()
        f.message = Data(text.utf8)
        f.correctionLevel = "M"
        guard let out = f.outputImage else { return nil }
        let scaled = out.transformed(by: CGAffineTransform(scaleX: 8, y: 8))
        guard let cg = CIContext().createCGImage(scaled, from: scaled.extent) else { return nil }
        return NSImage(cgImage: cg, size: scaled.extent.size)
    }
}

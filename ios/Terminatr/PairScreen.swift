import AVFoundation
import SwiftUI
import TerminatrLink
import TerminatrUI

/// Pairing with a server (docs/SPEC.md §16.15; redesigned in T850): scan
/// the QR code tm device pair shows on the computer (PairHero, with the
/// steps under it) or paste its terminatr://pair link; then confirm: the
/// server's id as chips to check against the computer's, this phone's
/// name, and Pair, then say yes on the computer. The code names the
/// server's key; the pairing takes no other. It adds the server beside
/// those paired already (T852); one paired already is paired again in
/// its place.
struct PairScreen: View {
    @Environment(Phone.self) private var phone
    @Environment(\.theme) private var theme
    @Environment(\.dismiss) private var dismiss
    /// A code that came as a link (the Camera app's scan).
    let initial: PairingCode?

    @State private var code: PairingCode?
    @State private var pasted = ""
    @State private var name = UIDevice.current.name
    @State private var pairing = false
    @State private var failure: String?
    @State private var camera = AVCaptureDevice.authorizationStatus(for: .video)

    init(code: PairingCode? = nil) {
        initial = code
    }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: Spacing.card) {
                if let code {
                    SheetHeader("Is this your server?", leading: {
                        Button {
                            self.code = nil
                            failure = nil
                        } label: {
                            Label("Back", systemImage: "chevron.backward")
                        }
                        .buttonStyle(.circle)
                        .disabled(pairing)
                    })
                    chosen(code)
                } else {
                    SheetHeader("Pair with a server", trailing: {
                        Button("Cancel") { dismiss() }
                            .buttonStyle(.ghost)
                    })
                    scan
                }
                if let failure {
                    Failure(text: failure)
                        .padding(.horizontal, Spacing.gutter)
                }
            }
            .frame(maxWidth: 560)
            .frame(maxWidth: .infinity)
            .padding(.bottom, 32)
        }
        .scrollDismissesKeyboard(.interactively)
        .sheetGlass()
        .onAppear { if code == nil { code = initial } }
    }

    @ViewBuilder
    private var scan: some View {
        PairHero(scanning: camera == .authorized) {
            switch camera {
            case .authorized:
                QRScanner { take($0) }
                    .accessibilityLabel("Camera, looking for the pairing code")
            case .notDetermined:
                Button("Use the camera") {
                    Task {
                        _ = await AVCaptureDevice.requestAccess(for: .video)
                        camera = AVCaptureDevice.authorizationStatus(for: .video)
                    }
                }
                .buttonStyle(.primary())
                .fixedSize()
            default:
                Text("The camera is off for terminatr: turn it on in Settings, or paste the code below.")
                    .font(theme.body(.subheadline, .medium))
                    .multilineTextAlignment(.center)
                    .padding(.horizontal, 28)
            }
        }
        .padding(.horizontal, Spacing.gutter)
        NumberedSteps([
            Text("On your computer, run \(Text("tm device pair").font(theme.mono(.subheadline, .medium)))"),
            Text("Point the camera at the code it shows"),
            Text("Say yes on the computer to add this phone"),
        ])
        .padding(.horizontal, Spacing.gutter)
        GroupLabel("Or paste the code")
        VStack(alignment: .leading, spacing: Spacing.row) {
            TextField("terminatr://pair?…", text: $pasted, axis: .vertical)
                .font(theme.mono(.subheadline))
                .foregroundStyle(theme.ink)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
                .padding(.horizontal, Spacing.card)
                .frame(minHeight: tapTarget)
                .surface(.card, in: RoundedRectangle(cornerRadius: Radius.row, style: .continuous))
            if !pasted.isEmpty {
                Button("Use this code") { take(pasted) }
                    .buttonStyle(.secondary)
            }
        }
        .padding(.horizontal, Spacing.gutter)
    }

    @ViewBuilder
    private func chosen(_ c: PairingCode) -> some View {
        VStack(alignment: .leading, spacing: Spacing.row) {
            Text("Server id")
                .font(theme.mono(.caption, .medium))
                .tracking(0.8)
                .textCase(.uppercase)
                .foregroundStyle(theme.muted)
            FingerprintChips(c.server.prefix(4))
            Text("\(c.relay.host() ?? c.relay.absoluteString) · tm device pair on your computer shows the same id. Only that server can answer.")
                .font(theme.body(.subheadline))
                .foregroundStyle(theme.muted)
                .fixedSize(horizontal: false, vertical: true)
            if let m = phone.model(DeviceStore.id(of: c)) {
                Text("This phone is paired with \(m.serverName) already: pairing again takes its place, the other servers stay.")
                    .font(theme.body(.subheadline, .medium))
                    .foregroundStyle(theme.ink)
                    .fixedSize(horizontal: false, vertical: true)
            } else if !phone.servers.isEmpty {
                Text("It joins the \(phone.servers.count == 1 ? "server" : "\(phone.servers.count) servers") this phone is paired with.")
                    .font(theme.body(.subheadline, .medium))
                    .foregroundStyle(theme.ink)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
        .padding(Spacing.card)
        .frame(maxWidth: .infinity, alignment: .leading)
        .surface(.card, in: RoundedRectangle(cornerRadius: Radius.card, style: .continuous))
        .padding(.horizontal, Spacing.gutter)
        GroupLabel("This phone's name")
        VStack(alignment: .leading, spacing: Spacing.tight) {
            TextField("This phone's name", text: $name)
                .font(theme.body(.headline))
                .foregroundStyle(theme.ink)
                .padding(.horizontal, Spacing.card)
                .frame(minHeight: 50)
                .surface(.card, in: RoundedRectangle(cornerRadius: Radius.row, style: .continuous))
            Text("The server shows this name and asks you to add the phone.")
                .font(theme.body(.footnote))
                .foregroundStyle(theme.muted)
                .padding(.horizontal, 6)
        }
        .padding(.horizontal, Spacing.gutter)
        VStack(spacing: Spacing.row) {
            Button {
                pair(c)
            } label: {
                HStack(spacing: Spacing.item) {
                    if pairing { ProgressView().tint(theme.onAction) }
                    Text(pairing ? "Say yes on your computer…" : "Pair")
                }
            }
            .buttonStyle(.primary())
            .disabled(pairing || name.trimmingCharacters(in: .whitespaces).isEmpty)
            Button("Scan another code") {
                code = nil
                failure = nil
            }
            .buttonStyle(.ghost)
            .disabled(pairing)
        }
        .padding(.horizontal, Spacing.gutter)
        .padding(.top, Spacing.item)
    }

    private func take(_ s: String) {
        do {
            code = try PairingCode(s)
            failure = nil
        } catch {
            failure = error.localizedDescription
        }
    }

    private func pair(_ c: PairingCode) {
        pairing = true
        failure = nil
        Task {
            do {
                try await phone.pair(c, name: name.trimmingCharacters(in: .whitespaces))
                dismiss()
            } catch {
                failure = error.localizedDescription
            }
            pairing = false
        }
    }
}

/// A capture session, started and stopped off the main thread, as
/// AVFoundation asks; nothing else touches it there.
final class Camera: @unchecked Sendable {
    let session = AVCaptureSession()
    private let queue = DispatchQueue(label: "dev.terminatr.ios.camera")

    func run(_ on: Bool) {
        queue.async { on ? self.session.startRunning() : self.session.stopRunning() }
    }
}

/// The camera, reading QR codes: found gets each code's text once.
struct QRScanner: UIViewRepresentable {
    let found: (String) -> Void

    func makeUIView(context: Context) -> Preview {
        let v = Preview()
        v.found = found
        v.start()
        return v
    }

    func updateUIView(_ v: Preview, context: Context) {
        v.found = found
    }

    static func dismantleUIView(_ v: Preview, coordinator: ()) {
        v.stop()
    }

    final class Preview: UIView, AVCaptureMetadataOutputObjectsDelegate {
        override class var layerClass: AnyClass { AVCaptureVideoPreviewLayer.self }
        var found: (String) -> Void = { _ in }
        private let camera = Camera()
        private var session: AVCaptureSession { camera.session }
        private var last: String?

        func start() {
            guard let cam = AVCaptureDevice.default(for: .video), let input = try? AVCaptureDeviceInput(device: cam),
                  session.canAddInput(input) else { return }
            session.addInput(input)
            let out = AVCaptureMetadataOutput()
            guard session.canAddOutput(out) else { return }
            session.addOutput(out)
            out.setMetadataObjectsDelegate(self, queue: .main)
            out.metadataObjectTypes = [.qr]
            let layer = self.layer as! AVCaptureVideoPreviewLayer
            layer.session = session
            layer.videoGravity = .resizeAspectFill
            camera.run(true)
        }

        func stop() { camera.run(false) }

        nonisolated func metadataOutput(_ output: AVCaptureMetadataOutput, didOutput objects: [AVMetadataObject], from connection: AVCaptureConnection) {
            let texts = objects.compactMap { ($0 as? AVMetadataMachineReadableCodeObject)?.stringValue }
            MainActor.assumeIsolated {
                for t in texts where t != last {
                    last = t
                    found(t)
                }
            }
        }
    }
}

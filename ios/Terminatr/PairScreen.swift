import AVFoundation
import SwiftUI
import TerminatrLink
import TerminatrUI

/// Pairing with a server (docs/SPEC.md §16.15): scan the QR code tm
/// device pair shows on the computer (or paste its terminatr://pair
/// link), name this phone, and say yes on the computer. The code names
/// the server's key; the pairing takes no other.
struct PairScreen: View {
    @Environment(PhoneModel.self) private var model
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
        NavigationStack {
            Form {
                if let code {
                    chosen(code)
                } else {
                    scan
                }
                if let failure {
                    Section { Failure(text: failure) }
                }
            }
            .scrollContentBackground(.hidden)
            .backdrop()
            .navigationTitle("Pair with a server")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) {
                    Button("Cancel") { dismiss() }
                }
            }
            .onAppear { if code == nil { code = initial } }
        }
    }

    @ViewBuilder
    private var scan: some View {
        Section {
            switch camera {
            case .authorized:
                QRScanner { take($0) }
                    .frame(height: 280)
                    .clipShape(RoundedRectangle(cornerRadius: 12))
                    .listRowInsets(EdgeInsets())
                    .accessibilityLabel("Camera, looking for the pairing code")
            case .notDetermined:
                Button("Use the camera") {
                    Task {
                        _ = await AVCaptureDevice.requestAccess(for: .video)
                        camera = AVCaptureDevice.authorizationStatus(for: .video)
                    }
                }
            default:
                Text("The camera is off for terminatr: turn it on in Settings, or paste the code below.")
                    .font(theme.body(.footnote))
                    .foregroundStyle(theme.muted)
            }
        } footer: {
            Text("On your computer, run tm device pair, then point the camera at the code it shows.")
        }
        Section("Or paste the code") {
            TextField("terminatr://pair?…", text: $pasted, axis: .vertical)
                .font(theme.mono(.footnote))
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
            Button("Use this code") { take(pasted) }
                .disabled(pasted.isEmpty)
        }
    }

    @ViewBuilder
    private func chosen(_ c: PairingCode) -> some View {
        Section {
            LabeledContent("Relay", value: c.relay.host() ?? c.relay.absoluteString)
            LabeledContent("Server key") {
                Text(c.server.prefix(6).map { String(format: "%02x", $0) }.joined() + "…")
                    .font(theme.mono(.footnote))
            }
            TextField("This phone's name", text: $name)
        } footer: {
            Text("The server shows this name and asks you to add the phone. Only the server whose key the code names can answer.")
        }
        Section {
            Button {
                pair(c)
            } label: {
                HStack {
                    Text(pairing ? "Say yes on your computer…" : "Pair")
                    if pairing {
                        Spacer()
                        ProgressView()
                    }
                }
            }
            .disabled(pairing || name.trimmingCharacters(in: .whitespaces).isEmpty)
            Button("Scan another code", role: .cancel) {
                code = nil
                failure = nil
            }
            .disabled(pairing)
        }
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
                try await model.pair(c, name: name.trimmingCharacters(in: .whitespaces))
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

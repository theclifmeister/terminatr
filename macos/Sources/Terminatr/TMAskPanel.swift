import AppKit
import SwiftUI
import TerminatrKit
import TerminatrUI

/// The question before the app runs a tm that fails its signature check
/// or changed path (TMAsk). A panel of its own, not a sheet: the app may
/// have no window open, at login or in the menu bar only. Leaving it any
/// way but Run It runs nothing.
@MainActor
final class TMAskPanel: NSPanel {
    private var answer: ((Bool) -> Void)?

    /// Asks and waits. A cancelled task (Reconnect while it shows) closes
    /// the panel and answers no; the new connection asks for itself.
    static func ask(_ ask: TMAsk) async -> Bool {
        let panel = TMAskPanel(ask)
        return await withTaskCancellationHandler {
            await withCheckedContinuation { (k: CheckedContinuation<Bool, Never>) in
                if Task.isCancelled {
                    k.resume(returning: false)
                    return
                }
                panel.show { k.resume(returning: $0) }
            }
        } onCancel: {
            Task { @MainActor in panel.finish(false) }
        }
    }

    private init(_ ask: TMAsk) {
        super.init(contentRect: .zero, styleMask: [.titled, .fullSizeContentView], backing: .buffered, defer: true)
        title = TMAsk.title
        titlebarAppearsTransparent = true
        titleVisibility = .hidden
        isMovableByWindowBackground = true
        isReleasedWhenClosed = false
        level = .floating
        hidesOnDeactivate = false
        let view = TMAskView(ask: ask) { [weak self] in self?.finish($0) }
            .themed()
        let host = NSHostingController(rootView: view)
        // The hidden title bar keeps no room for itself.
        host.safeAreaRegions = []
        contentViewController = host
    }

    private func show(_ answer: @escaping (Bool) -> Void) {
        self.answer = answer
        center()
        NSApp.activate()
        makeKeyAndOrderFront(nil)
    }

    private func finish(_ yes: Bool) {
        close()
        answer?(yes)
        answer = nil
    }

    override var canBecomeKey: Bool { true }

    /// Esc is Don't Run. Here, not cancelOperation: SwiftUI's hosting view
    /// takes the key before that.
    override func sendEvent(_ event: NSEvent) {
        if event.type == .keyDown, event.keyCode == 53 {
            finish(false)
        } else {
            super.sendEvent(event)
        }
    }
}

private struct TMAskView: View {
    @Environment(\.theme) private var theme
    let ask: TMAsk
    let answer: (Bool) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(TMAsk.title)
                .font(theme.title(.sheetTitle, .heavy))
                .accessibilityAddTraits(.isHeader)
            Text(ask.path)
                .font(theme.mono(.subheadline))
                .fixedSize(horizontal: false, vertical: true)
                .textSelection(.enabled)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(10)
                .surface(in: RoundedRectangle(cornerRadius: Radius.tile, style: .continuous))
            Text(ask.why)
                .font(theme.body(.body))
                .fixedSize(horizontal: false, vertical: true)
            Text(TMAsk.warning)
                .font(theme.body(.body))
                .foregroundStyle(theme.muted)
                .fixedSize(horizontal: false, vertical: true)
            HStack {
                Spacer()
                Button("Run It") { answer(true) }
                    .buttonStyle(.flat)
                Button("Don't Run") { answer(false) }
                    .buttonStyle(FlatButtonStyle(primary: true))
                    .keyboardShortcut(.defaultAction)
            }
            .padding(.top, 4)
        }
        .padding(18)
        .frame(width: 460)
        .foregroundStyle(theme.ink)
        .background(theme.chrome)
    }
}

import SwiftUI
import TerminatrKit

/// Pairing's camera (T850): what the camera sees (or, without one, what
/// to do instead) on the terminal's dark, under a viewfinder whose four
/// corners are the theme's mesh colours and a scan line in the mesh's
/// centre colour that sweeps up and down, still with Reduce Motion.
/// Words inside it are the terminal's, which stays dark in every theme.
public struct PairHero<Content: View>: View {
    @Environment(\.theme) private var theme
    @Environment(\.accessibilityReduceMotion) private var still
    @State private var sweep = false
    let scanning: Bool
    let content: Content

    /// scanning: the camera is on, so the line sweeps.
    public init(scanning: Bool, @ViewBuilder content: () -> Content) {
        self.scanning = scanning
        self.content = content()
    }

    public var body: some View {
        let shape = RoundedRectangle(cornerRadius: 28, style: .continuous)
        ZStack {
            theme.terminalBackground
            content
                .foregroundStyle(theme.terminalForeground)
            Viewfinder()
                .frame(width: 170, height: 170)
                .allowsHitTesting(false)
                .accessibilityHidden(true)
            if scanning {
                LinearGradient(colors: [theme.mesh[4].opacity(0), theme.mesh[4], theme.mesh[4].opacity(0)], startPoint: .leading, endPoint: .trailing)
                    .frame(height: 2)
                    .shadow(color: theme.mesh[4], radius: 6)
                    .padding(.horizontal, 20)
                    .offset(y: still ? 0 : (sweep ? 70 : -70))
                    .allowsHitTesting(false)
                    .accessibilityHidden(true)
                    .onAppear {
                        guard !still else { return }
                        withAnimation(.easeInOut(duration: 2.2).repeatForever(autoreverses: true)) { sweep = true }
                    }
            }
        }
        .frame(maxWidth: .infinity)
        .frame(height: 260)
        .clipShape(shape)
        .overlay(shape.strokeBorder(theme.edge, lineWidth: 1))
    }
}

/// The viewfinder's four corners, each in one of the mesh's colours.
private struct Viewfinder: View {
    @Environment(\.theme) private var theme

    var body: some View {
        ZStack {
            corner(0, .topLeading, 0)
            corner(1, .topTrailing, 90)
            corner(3, .bottomTrailing, 180)
            corner(2, .bottomLeading, 270)
        }
    }

    private func corner(_ i: Int, _ at: Alignment, _ turn: Double) -> some View {
        Corner()
            .stroke(theme.mesh[i], style: StrokeStyle(lineWidth: 5, lineCap: .round))
            .frame(width: 40, height: 40)
            .rotationEffect(.degrees(turn))
            .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: at)
    }
}

/// A top-left corner with a rounded bend.
private struct Corner: Shape {
    func path(in r: CGRect) -> Path {
        var p = Path()
        p.move(to: CGPoint(x: r.minX, y: r.maxY))
        p.addLine(to: CGPoint(x: r.minX, y: r.minY + 16))
        p.addQuadCurve(to: CGPoint(x: r.minX + 16, y: r.minY), control: CGPoint(x: r.minX, y: r.minY))
        p.addLine(to: CGPoint(x: r.maxX, y: r.minY))
        return p
    }
}

/// Steps in order, each after its number in an action-coloured circle,
/// on a deck card.
public struct NumberedSteps: View {
    @Environment(\.theme) private var theme
    let steps: [Text]

    public init(_ steps: [Text]) {
        self.steps = steps
    }

    public var body: some View {
        let lift = theme.size(.callout) * 0.35
        VStack(alignment: .leading, spacing: Spacing.row) {
            ForEach(Array(steps.enumerated()), id: \.offset) { i, step in
                HStack(alignment: .firstTextBaseline, spacing: Spacing.row) {
                    Text("\(i + 1)")
                        .font(theme.mono(.callout, .semibold))
                        .foregroundStyle(theme.onAction)
                        .frame(width: 26 * theme.scale, height: 26 * theme.scale)
                        .background(theme.action, in: Circle())
                        .alignmentGuide(.firstTextBaseline) { $0[VerticalAlignment.center] + lift }
                    step
                        .font(theme.body(.headline))
                        .foregroundStyle(theme.ink)
                        .fixedSize(horizontal: false, vertical: true)
                }
                .accessibilityElement(children: .combine)
            }
        }
        .padding(Spacing.card)
        .frame(maxWidth: .infinity, alignment: .leading)
        .surface(.card, in: RoundedRectangle(cornerRadius: 22, style: .continuous))
    }
}

/// A key's fingerprint as chips to read off against another screen: the
/// hex digits upper case, in groups of four, in mono on an ink wash
/// ("a sheet chip"). VoiceOver reads it a character at a time.
public struct FingerprintChips: View {
    @Environment(\.theme) private var theme
    let groups: [String]

    /// bytes: the start of the key's hash, as many as the other screen
    /// shows (tm device pair: four, as 4F2A-91C3).
    public init(_ bytes: Data) {
        let hex = bytes.map { String(format: "%02X", $0) }.joined()
        groups = stride(from: 0, to: hex.count, by: 4).map {
            let a = hex.index(hex.startIndex, offsetBy: $0)
            return String(hex[a..<(hex.index(a, offsetBy: 4, limitedBy: hex.endIndex) ?? hex.endIndex)])
        }
    }

    public var body: some View {
        Flow(spacing: Spacing.tight) {
            ForEach(Array(groups.enumerated()), id: \.offset) { _, g in
                Text(g)
                    .font(theme.mono(.title3, .medium))
                    .foregroundStyle(theme.ink)
                    .padding(.horizontal, 10)
                    .padding(.vertical, 7)
                    .background(theme.ink.opacity(ThemeColours.inkWash), in: RoundedRectangle(cornerRadius: 8, style: .continuous))
            }
        }
        .accessibilityElement()
        .accessibilityLabel(groups.map { $0.map(String.init).joined(separator: " ") }.joined(separator: ", "))
    }
}

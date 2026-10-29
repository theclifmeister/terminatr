import SwiftUI
import TerminatrKit

/// The redesign's four button roles (T845), in place of .bordered and
/// .borderedProminent. Each is at least 44 pt tall; pressed, it dims to
/// 0.9 and (without Reduce Motion) springs to 0.97.
public struct RoleButtonStyle: ButtonStyle {
    public enum Role: Sendable {
        /// The answer: a tone's fill (the action's when nil), its ink.
        case primary(Attention.Tone?)
        /// The other answer: a 5% ink wash with a hairline, in ink.
        case secondary
        /// A quiet action: the action colour's text alone.
        case ghost
        /// A header's button: a 44 pt glass circle round its symbol.
        case circle
    }

    let role: Role

    public func makeBody(configuration: Configuration) -> some View {
        RoleButton(role: role, configuration: configuration)
    }
}

extension ButtonStyle where Self == RoleButtonStyle {
    public static func primary(_ tone: Attention.Tone? = nil) -> RoleButtonStyle { RoleButtonStyle(role: .primary(tone)) }
    public static var secondary: RoleButtonStyle { RoleButtonStyle(role: .secondary) }
    public static var ghost: RoleButtonStyle { RoleButtonStyle(role: .ghost) }
    public static var circle: RoleButtonStyle { RoleButtonStyle(role: .circle) }
}

private struct RoleButton: View {
    @Environment(\.theme) private var theme
    @Environment(\.isEnabled) private var enabled
    @Environment(\.accessibilityReduceMotion) private var still
    let role: RoleButtonStyle.Role
    let configuration: ButtonStyleConfiguration

    var body: some View {
        let pressed = configuration.isPressed
        styled
            .contentShape(shape)
            .opacity(enabled ? (pressed ? 0.9 : 1) : 0.45)
            .scaleEffect(pressed && !still ? 0.97 : 1)
            .animation(still ? nil : .spring(response: 0.25, dampingFraction: 0.7), value: pressed)
    }

    private var shape: some Shape { Capsule() }

    @ViewBuilder private var styled: some View {
        let label = configuration.label
            .font(theme.body(.headline, .semibold))
            .lines(1)
            .multilineTextAlignment(.center)
        switch role {
        case .primary(let tone):
            let fill = tone.map(theme.colour) ?? theme.action
            label
                .foregroundStyle(tone.map(theme.on) ?? theme.onAction)
                .padding(.horizontal, 18)
                .frame(maxWidth: .infinity, minHeight: tapTarget)
                .background(fill, in: Capsule())
                .shadow(color: fill.opacity(0.35), radius: 8, y: 4)
        case .secondary:
            label
                .foregroundStyle(theme.ink)
                .padding(.horizontal, 18)
                .frame(minWidth: 92, minHeight: tapTarget)
                .background(theme.ink.opacity(0.05), in: Capsule())
                .overlay(Capsule().strokeBorder(theme.line, lineWidth: 1))
        case .ghost:
            label
                .foregroundStyle(theme.action)
                .padding(.horizontal, 8)
                .frame(minHeight: tapTarget)
        case .circle:
            configuration.label
                .labelStyle(.iconOnly)
                .font(.system(size: 18 * theme.scale, weight: .medium))
                .foregroundStyle(theme.ink)
                .frame(width: max(tapTarget, 44 * theme.scale), height: max(tapTarget, 44 * theme.scale))
                .surface(.glass, in: Circle())
        }
    }
}

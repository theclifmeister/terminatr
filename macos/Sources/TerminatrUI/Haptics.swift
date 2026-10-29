import SwiftUI

/// The app's haptics (T847): a selection tap on picking a chip or an
/// option, success or warning when the server answers a write, and
/// nothing when a need arrives. Settings turns them all off (key); the
/// system's own setting still wins.
public enum Haptics {
    /// The UserDefaults key of the switch, on unless turned off.
    public static let key = "haptics"
}

extension EnvironmentValues {
    /// The haptics switch, as Settings has it.
    @Entry public var haptics = true
    /// A hardware keyboard's keys answer here: 1 to 9 an option, Y and N
    /// an approval (T847). Only one card on screen has them (the lead
    /// card of the deck), so a key never answers two needs; off by
    /// default, so a screen with a text field never sends a key as an
    /// answer.
    @Entry public var answerKeys = false
}

extension View {
    /// A haptic when trigger changes, unless the switch is off.
    public func feedback<T: Equatable>(_ feedback: SensoryFeedback, trigger: T) -> some View {
        modifier(Feedback(feedback: feedback, trigger: trigger))
    }

    /// The key that answers, while the environment's answerKeys says so.
    public func answerKey(_ key: KeyEquivalent?) -> some View {
        modifier(AnswerKey(key: key))
    }
}

private struct Feedback<T: Equatable>: ViewModifier {
    @Environment(\.haptics) private var on
    let feedback: SensoryFeedback
    let trigger: T

    func body(content: Content) -> some View {
        content.sensoryFeedback(trigger: trigger) { _, _ in on ? feedback : nil }
    }
}

private struct AnswerKey: ViewModifier {
    @Environment(\.answerKeys) private var keys
    let key: KeyEquivalent?

    func body(content: Content) -> some View {
        content.keyboardShortcut(keys ? key.map { KeyboardShortcut($0, modifiers: []) } : nil)
    }
}

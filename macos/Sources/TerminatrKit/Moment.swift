import Foundation

extension Transcript {
    /// The entry a need was asked at, which the iPhone's conversation
    /// beside the need scrolls to and marks (T826): an approval's call
    /// still waiting for its result; a question's last AskUserQuestion
    /// call from before it was asked (since), else the last entry from
    /// before then. Nil for other needs, and while the transcript
    /// doesn't hold it.
    public func moment(_ kind: Need.Kind, since: Date? = nil) -> Int? {
        switch kind {
        case .approval:
            return pendingTool?.id
        case .question:
            // A few seconds' slack: the menu's since is when tm saw it on
            // the screen, the call's time when the agent wrote it.
            let by = since?.addingTimeInterval(5)
            func before(_ e: Entry) -> Bool {
                guard let by else { return true }
                return (e.at ?? .distantPast) <= by
            }
            if let e = entries.last(where: { $0.asksUser && before($0) }) { return e.id }
            guard by != nil else { return nil }
            return entries.last(where: { $0.at != nil && before($0) })?.id
        default:
            return nil
        }
    }
}

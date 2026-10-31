import Foundation

// session.answer's and peer.need-act's picks (app API 39, T900) go only
// when there are some: a bridge before 39 decodes params strictly and
// refuses a field it doesn't know, so an answer without picks must read
// as it did before.

extension AppAPI.AnswerParams {
    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(id, forKey: .id)
        try c.encode(index, forKey: .index)
        try c.encode(answer, forKey: .answer)
        try c.encode(question, forKey: .question)
        if !picks.isEmpty { try c.encode(picks, forKey: .picks) }
    }
}

extension AppAPI.PeerNeedActParams {
    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(peer, forKey: .peer)
        try c.encode(project, forKey: .project)
        try c.encode(need, forKey: .need)
        try c.encode(act, forKey: .act)
        try c.encode(index, forKey: .index)
        try c.encode(answer, forKey: .answer)
        try c.encode(question, forKey: .question)
        if !picks.isEmpty { try c.encode(picks, forKey: .picks) }
        try c.encode(choice, forKey: .choice)
        try c.encode(option, forKey: .option)
        try c.encode(status, forKey: .status)
        try c.encode(note, forKey: .note)
    }
}

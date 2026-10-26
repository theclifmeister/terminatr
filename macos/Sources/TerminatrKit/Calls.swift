import Foundation

extension AppAPI {
    /// The params of a method that takes none: the request carries no
    /// params at all.
    public struct NoParams: Encodable, Sendable, Hashable {
        public init() {}
    }

    /// A method answered by one result (AppAPI.Methods names each).
    /// Write methods act as the human and are journaled "human (app)";
    /// target names the stream an input method's id refers to.
    public struct Call<Params: Encodable & Sendable, Result: Decodable & Sendable>: Sendable {
        public let name: String
        public let write: Bool
        public let target: String?

        public init(_ name: String, write: Bool, target: String? = nil) {
            self.name = name
            self.write = write
            self.target = target
        }
    }

    /// A stream method: its result, then events named in events, each
    /// carrying Data, until it ends or is cancelled.
    public struct Subscription<Params: Encodable & Sendable, Result: Decodable & Sendable, Data: Decodable & Sendable>: Sendable {
        public let name: String
        public let events: [String]

        public init(_ name: String, events: [String]) {
            self.name = name
            self.events = events
        }
    }
}

extension AppAPI.Error: Swift.Error, LocalizedError {
    public var errorDescription: String? { message.isEmpty ? code : message }
}

extension AppAPI {
    /// The coders for bridge lines: tm writes times as RFC 3339, with
    /// fractional seconds or without.
    public static func decoder() -> JSONDecoder {
        let d = JSONDecoder()
        d.dateDecodingStrategy = .custom { decoder in
            let c = try decoder.singleValueContainer()
            let s = try c.decode(String.self)
            if let t = try? Date(s, strategy: Date.ISO8601FormatStyle(includingFractionalSeconds: true)) {
                return t
            }
            if let t = try? Date(s, strategy: .iso8601) {
                return t
            }
            throw DecodingError.dataCorruptedError(in: c, debugDescription: "not an RFC 3339 time: \(s)")
        }
        return d
    }

    public static func encoder() -> JSONEncoder {
        let e = JSONEncoder()
        e.dateEncodingStrategy = .iso8601
        e.outputFormatting = [.sortedKeys, .withoutEscapingSlashes]
        return e
    }
}

extension AppAPI {
    /// projects.watch's params as a bridge before app API 7 takes them:
    /// detail alone, since it refuses lean and shown as unknown.
    public struct DetailParams: Encodable, Sendable, Hashable {
        public var detail: Bool

        public init(detail: Bool) {
            self.detail = detail
        }
    }

    /// projects.watch for a bridge before app API 7 (DetailParams).
    public static let projectsWatchDetail = Subscription<DetailParams, Projects, Projects>("projects.watch", events: ["projects.changed"])

    /// The app API that has projects.watch {lean, shown}.
    public static let leanAPI = 7
}

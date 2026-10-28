import CryptoKit
import Foundation

/// Signs a device's writes (app API 23, docs/SPEC.md §16.15): when the
/// bridge's hello says sign, every write carries a signature by the
/// device's write key over the method, a nonce the bridge gave out
/// (device.nonce) and the params exactly as the request line has them.
/// On the phone the key is in the Secure Enclave and signing asks for
/// Face ID; the Mac app's bridge never asks for a signature.
public protocol WriteSigner: Sendable {
    /// An ECDSA P-256 SHA-256 signature of message, ASN.1 DER. why says
    /// what the user is asked to allow (the Face ID prompt's reason).
    func sign(_ message: Data, why: String) async throws -> Data
}

public enum SignedWrite {
    /// appapi.SignedMessage: "tm-device-write v1\n" method "\n"
    /// SHA-256(nonce) SHA-256(params).
    public static func message(method: String, nonce: Data, params: Data) -> Data {
        var out = Data("tm-device-write v1\n\(method)\n".utf8)
        out.append(contentsOf: SHA256.hash(data: nonce))
        out.append(contentsOf: SHA256.hash(data: params))
        return out
    }

    /// A request line with params as their bytes, so the signature is
    /// over what the bridge reads, and auth.
    static func line(id: Int, method: String, params: Data?, auth: AppAPI.Auth, encoder: JSONEncoder) throws -> Data {
        var out = Data(#"{"auth":"#.utf8)
        out.append(try encoder.encode(auth))
        out.append(contentsOf: Data(#","id":\#(id),"method":"#.utf8))
        out.append(try encoder.encode(method))
        if let params {
            out.append(contentsOf: Data(#","params":"#.utf8))
            out.append(params)
        }
        out.append(contentsOf: Data("}".utf8))
        return out
    }

    /// Whether a write's params only read, so the bridge wants no
    /// signature (appapi.readsOnly): session.approve {check}.
    public static func readsOnly<P>(_ params: P) -> Bool {
        (params as? AppAPI.ApproveParams)?.check == true
    }

    /// What the Face ID prompt says for a write of method.
    public static func why(_ method: String) -> String {
        switch method {
        case AppAPI.Methods.sessionApprove.name: return "Allow the agent's request"
        case AppAPI.Methods.sessionAnswer.name: return "Answer the question"
        case AppAPI.Methods.taskAccept.name: return "Accept the task"
        case AppAPI.Methods.taskSendBack.name: return "Send the task back"
        case AppAPI.Methods.questionsOpen.name: return "Open the coordinator's questions"
        default: return "Act on your server"
        }
    }
}

/// A software write key (tests, and a Simulator, which has no Secure
/// Enclave): it signs without asking.
public struct SoftwareSigner: WriteSigner {
    public let key: P256.Signing.PrivateKey

    public init(key: P256.Signing.PrivateKey = .init()) {
        self.key = key
    }

    public func sign(_ message: Data, why: String) async throws -> Data {
        try key.signature(for: message).derRepresentation
    }
}

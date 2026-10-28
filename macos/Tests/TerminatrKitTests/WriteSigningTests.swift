import CryptoKit
import Foundation
import Testing
@testable import TerminatrKit

/// appapi's TestSignedMessageVector: the same message, the same digest.
@Test func signedMessageVector() {
    let nonce = Data((0..<32).map { UInt8($0) })
    let m = SignedWrite.message(method: "session.approve", nonce: nonce, params: Data(#"{"id":"s-1"}"#.utf8))
    let hex = SHA256.hash(data: m).map { String(format: "%02x", $0) }.joined()
    #expect(hex == "a36611fd6e712e5976eb7e8d334133866a9f9b1ae21f59ad6dffe2c5e639e142")
}

let signHelloLine = #"{"hello":{"app_api":23,"tm":"0.58.0","build":"abc","level":"answer","sign":true,"capabilities":["read","streams"],"methods":["device.nonce","session.approve","session.list"]}}"#

/// A write to a bridge whose hello says sign asks for a nonce, then
/// goes with the signer's signature over the method, the nonce and the
/// params' bytes exactly as the line carries them; a read goes as it is.
@Test func signedWrite() async throws {
    let t = ScriptTransport()
    let nonce = Data(repeating: 7, count: 32)
    t.reply = { req in
        let id = req["id"] as! Int
        switch req["method"] as! String {
        case "device.nonce":
            return [#"{"id":\#(id),"result":{"nonce":"\#(nonce.base64EncodedString())"}}"#]
        case "session.list":
            return [#"{"id":\#(id),"result":{"sessions":[],"alerts":0,"notices":[]}}"#]
        default:
            return [#"{"id":\#(id),"result":{"question":"Run it?","option":"Yes","options":["Yes","No"],"sent":true}}"#]
        }
    }
    let b = Bridge(transport: t)
    t.feed(signHelloLine)
    _ = try await b.start()
    let signer = SoftwareSigner()
    await b.setSigner(signer)
    _ = try await b.call(AppAPI.Methods.sessionList)
    _ = try await b.call(AppAPI.Methods.sessionApprove, AppAPI.ApproveParams(id: "s-1", check: true))
    let res = try await b.call(AppAPI.Methods.sessionApprove, AppAPI.ApproveParams(id: "s-1"))
    #expect(res.sent)
    let sent = t.sent
    #expect(sent.count == 4)
    #expect(sent[0] == #"{"id":1,"method":"session.list"}"#)
    #expect(sent[1] == #"{"id":2,"method":"session.approve","params":{"check":true,"choice":0,"escape":false,"id":"s-1","session":false}}"#)
    #expect(sent[2] == #"{"id":3,"method":"device.nonce"}"#)
    let line = sent[3]
    let params = try AppAPI.encoder().encode(AppAPI.ApproveParams(id: "s-1"))
    #expect(line.hasSuffix(#","id":4,"method":"session.approve","params":"# + String(decoding: params, as: UTF8.self) + "}"))
    let obj = try JSONSerialization.jsonObject(with: Data(line.utf8)) as! [String: Any]
    let auth = obj["auth"] as! [String: String]
    #expect(Data(base64Encoded: auth["nonce"]!) == nonce)
    let sig = try P256.Signing.ECDSASignature(derRepresentation: Data(base64Encoded: auth["sig"]!)!)
    #expect(signer.key.publicKey.isValidSignature(sig, for: SignedWrite.message(method: "session.approve", nonce: nonce, params: params)))
}

/// The Mac app's bridge never says sign: its writes go as they are.
@Test func unsignedWithoutSign() async throws {
    let t = ScriptTransport()
    t.reply = { req in [#"{"id":\#(req["id"] as! Int),"result":{"question":"","option":"","options":[],"sent":true}}"#] }
    let b = try await started(t)
    await b.setSigner(SoftwareSigner())
    _ = try await b.call(AppAPI.Methods.sessionApprove, AppAPI.ApproveParams(id: "s-1"))
    #expect(t.sent.count == 1)
    #expect(!t.sent[0].contains("auth"))
}

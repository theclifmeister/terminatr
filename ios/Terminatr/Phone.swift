import Foundation
import Observation
import TerminatrKit
import TerminatrLink

/// Every server the phone reads (T852): one PhoneModel per pairing, each
/// with its own device key, write key, level and link (DeviceStore), in
/// the order they were paired; or the demo's one. Needs you merges their
/// needs (needs), Projects and the iPad's sidebar group by server, and
/// Settings lists them, each with its own Forget. An act goes to the
/// server whose need or session it is, from that server's PhoneModel,
/// signed with that server's key, so each server's trust is as with one.
/// A server the phone pairs with directly that another one also links
/// to shows once, as its own (PhoneModel.shownPeers).
@MainActor @Observable
final class Phone {
    /// The paired servers, each linked while the app is in front.
    private(set) var servers: [PhoneModel] = []
    /// The demo, while it is open: the only one then.
    private(set) var demo: PhoneModel?
    /// Why the last pairing or the demo failed, for the welcome screen.
    private(set) var failure: String?
    /// Where the user is on each screen (T825), for every server's.
    @ObservationIgnored let places = Places()
    /// What the screens read while nothing is open.
    @ObservationIgnored private lazy var nothing = PhoneModel(places: places)

    /// What the screens show: the demo, else the paired servers.
    var models: [PhoneModel] { demo.map { [$0] } ?? servers }

    /// Nothing open: the welcome screen.
    var isEmpty: Bool { models.isEmpty }

    /// The first server, which the screens that read one server read
    /// when their route names none (a launch argument's).
    var primary: PhoneModel { models.first ?? nothing }

    /// Whether more than one server shows: the screens then name each.
    var several: Bool { models.count > 1 }

    func model(_ key: String) -> PhoneModel? {
        models.first { $0.key == key }
    }

    /// Every server's needs in one list (Need.merge), each by its server.
    var needs: [ServerNeed] {
        Need.merge(models.map { ($0, $0.needs) }).map { ServerNeed(model: $0.server, need: $0.need) }
    }

    /// The servers the phone pairs with directly, as peer.list names them.
    var direct: [Pairings.Direct] {
        servers.compactMap { m in
            guard case .server(let s) = m.source else { return nil }
            return Pairings.Direct(id: m.key, name: s.name)
        }
    }

    // MARK: Pairings

    /// Opens every server the Keychain keeps that isn't open yet.
    func openPaired() {
        guard demo == nil else { return }
        for pid in DeviceStore.pairings() where model(pid) == nil {
            guard let s = DeviceStore.server(pid) else { continue }
            add(pid).open(s)
        }
    }

    private func add(_ pid: String) -> PhoneModel {
        let m = PhoneModel(key: pid, places: places)
        m.direct = { [weak self] in self?.direct ?? [] }
        servers.append(m)
        return m
    }

    /// Pairs with the server code names and opens it, beside the others:
    /// a new server gets a device key and a write key of its own; one
    /// paired already is paired again, in its place, with the device key
    /// it pinned. The demo closes. It throws why not, for the pairing
    /// screen to say.
    func pair(_ code: PairingCode, name: String) async throws {
        let pid = DeviceStore.id(of: code)
        let version = Bundle.main.object(forInfoDictionaryKey: "CFBundleShortVersionString") as? String ?? "0"
        let s: DevicePairing.Server
        do {
            let id = try DeviceStore.identity(pid)
            let key = try WriteKey.forPairing(pid)
            s = try await DevicePairing.join(code, as: id, name: name, version: version, writeKey: key.signer.publicKey)
            if key.new { try WriteKey.keep(key.signer, pid) }
            try DeviceStore.save(s)
        } catch {
            DeviceStore.dropUnpaired(pid)
            throw error
        }
        failure = nil
        if demo != nil { await leaveDemo() }
        if let m = model(pid) {
            // Its link redials with the key the server took now.
            await m.disconnect()
            m.open(s)
        } else {
            add(pid).open(s)
        }
        openPaired()
    }

    /// Forgets m's server and the keys the phone made for it; the other
    /// servers stay. With none left, back to the welcome screen.
    func forget(_ m: PhoneModel) async {
        await m.disconnect()
        servers.removeAll { $0 === m }
        DeviceStore.forget(m.key)
        if servers.isEmpty { places.forget() }
    }

    func failed(_ error: any Error) {
        failure = error.localizedDescription
    }

    // MARK: The demo

    /// Opens the demo in place of the servers (their links close until
    /// it is left).
    func startDemo() async {
        let m = PhoneModel(key: "demo", places: places)
        await m.startDemo()
        guard m.source == .demo else {
            failure = m.failure
            return
        }
        for s in servers { await s.suspend() }
        failure = nil
        places.forget()
        demo = m
    }

    /// Back from the demo: to the paired servers, else the welcome screen.
    func leaveDemo() async {
        await demo?.disconnect()
        demo = nil
        places.forget()
        for s in servers { s.resume() }
        openPaired()
    }

    // MARK: The app in front or behind

    func resume() {
        for m in servers where demo == nil { m.resume() }
    }

    func toBack() async {
        await withTaskGroup(of: Void.self) { g in
            for m in servers { g.addTask { await m.toBack() } }
        }
    }

    func suspend() async {
        for m in servers { await m.suspend() }
    }

    /// Every server read again (pull to refresh on a merged screen).
    func refresh() async {
        await withTaskGroup(of: Void.self) { g in
            for m in models { g.addTask { await m.refresh() } }
        }
    }
}

/// A need with the server it is of, whose PhoneModel answers it.
struct ServerNeed: Identifiable {
    let model: PhoneModel
    let need: Need

    var id: String { model.key + " " + need.id }
}

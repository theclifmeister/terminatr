import Foundation
import Testing
@testable import TerminatrKit

private func phone(level: String = "view", effective: String? = nil, writeKey: Bool = true, link: AppAPI.DeviceLink? = nil) -> AppAPI.Device {
    AppAPI.Device(name: "Clif's iPhone", id: "4f2a91c3", short: "4F2A-91C3", level: level, effective: effective ?? level,
                  relay: "relay.example", writeKey: writeKey, link: link)
}

/// Settings › Devices needs app API 26 and its methods.
@Test func devicesNeedAPI26() {
    var h = AppAPI.Hello(appApi: 26, tm: "v0.59.0", build: "x")
    #expect(!Compatibility.managesDevices(h))
    h.methods = [AppAPI.Methods.deviceList.name, AppAPI.Methods.devicePair.name]
    #expect(Compatibility.managesDevices(h))
    h.appApi = 25
    #expect(!Compatibility.managesDevices(h))
}

/// A device's link reads as tm device list says it: a phone that isn't
/// open is waited for.
@Test func deviceLinkWords() {
    let now = Date(timeIntervalSince1970: 10_000)
    #expect(DeviceWords.link(phone(), running: false) == "The server isn't running")
    #expect(DeviceWords.link(phone(), running: true) == "No link yet")
    #expect(DeviceWords.link(phone(link: .init(state: "up", since: now.addingTimeInterval(-30))), running: true, now: now) == "Connected just now")
    #expect(DeviceWords.link(phone(link: .init(state: "up", since: now.addingTimeInterval(-600))), running: true, now: now) == "Connected for 10 min")
    #expect(DeviceWords.link(phone(link: .init(state: "offline", reason: "peer", since: now)), running: true, now: now) == "Waiting for it")
    #expect(DeviceWords.link(phone(link: .init(state: "offline", reason: "relay", since: now, error: "dial: refused")), running: true, now: now) == "Offline: dial: refused")
    #expect(DeviceWords.link(phone(link: .init(state: "connecting", since: now)), running: true, now: now) == "Connecting")
}

/// Answer needs a write key; the list says why a device can't, or runs
/// below its level.
@Test func deviceLevelLimits() {
    #expect(DeviceWords.limit(phone()) == nil)
    #expect(DeviceWords.limit(phone(writeKey: false))?.contains("Pair it again") == true)
    #expect(DeviceWords.limit(phone(level: "write", effective: "view"))?.contains("isn't in this release") == true)
    #expect(DeviceWords.level("answer") == "Answer" && DeviceWords.level("view") == "View only")
}

/// The pairing's countdown and the confirmation tm device pair asks.
@Test func pairingWords() {
    let now = Date(timeIntervalSince1970: 0)
    #expect(DeviceWords.countdown(to: now.addingTimeInterval(600), now: now) == "10:00")
    #expect(DeviceWords.countdown(to: now.addingTimeInterval(61.2), now: now) == "1:02")
    #expect(DeviceWords.countdown(to: now.addingTimeInterval(-5), now: now) == "0:00")
    let o = AppAPI.DeviceOffer(name: "iPhone 2", phone: "iPhone", short: "77AB-0C1D", version: "1.0", writeKey: false, again: "Old phone",
                               level: "answer", answerBy: now)
    #expect(DeviceWords.offer(o) == ["“iPhone”, device id 77AB-0C1D, app 1.0",
                                     "This phone is paired already, as “Old phone”: pairing again keeps its level.",
                                     "Its app has no write key: it can only view, whatever its level."])
    #expect(DeviceWords.question(name: "iPhone 2", level: "view") == "Add it as “iPhone 2”, a view-only device?")
    #expect(DeviceWords.question(name: "iPhone 2", level: "answer") == "Add it as “iPhone 2”, an answer device?")
    #expect(DeviceWords.error(AppAPI.Error(code: "pair-expired", message: "x")).contains("time ran out"))
    #expect(DeviceWords.error(AppAPI.Error(code: "name-taken", message: "a device is already called iPhone")) == "a device is already called iPhone")
}

/// Write is a level the app offers (T821): its help says what it does.
@Test func writeLevelHelp() {
    #expect(DeviceWords.level("write") == "Write")
    #expect(DeviceWords.levelHelp("write").contains("prompts"))
    #expect(DeviceWords.levelHelp("write") != DeviceWords.levelHelp(DeviceWords.view))
}

import Foundation
import Testing
@testable import TerminatrKit

private func hello(_ api: Int, _ tm: String) -> AppAPI.Hello {
    AppAPI.Hello(appApi: api, tm: tm, build: tm + "+x")
}

@Test func compatibilityByAPI() {
    #expect(Compatibility.check(hello(1, "v0.50.0"), appVersion: "0.54.0", appAPI: 3) == .tmTooOld(tm: "v0.50.0", api: 1))
    #expect(Compatibility.check(hello(1, "v0.50.0"), appVersion: "0.54.0", appAPI: 3).blocks)
    // The API decides before the releases do.
    #expect(Compatibility.check(hello(2, "v0.55.0"), appVersion: "0.54.0", appAPI: 3) == .tmBehind(tm: "v0.55.0", app: "0.54.0"))
    #expect(Compatibility.check(hello(4, "v0.53.0"), appVersion: "0.54.0", appAPI: 3) == .appBehind(tm: "v0.53.0", app: "0.54.0"))
    #expect(!Compatibility.check(hello(4, "v0.53.0"), appVersion: "0.54.0", appAPI: 3).blocks)
}

@Test func compatibilityByRelease() {
    let api = AppAPI.version
    #expect(Compatibility.check(hello(api, "v0.54.0"), appVersion: "0.54.0") == .ok)
    #expect(Compatibility.check(hello(api, "v0.55.0"), appVersion: "0.54.0") == .appBehind(tm: "v0.55.0", app: "0.54.0"))
    #expect(Compatibility.check(hello(api, "v0.54.0"), appVersion: "0.54.1") == .tmBehind(tm: "v0.54.0", app: "0.54.1"))
    // A snapshot tm is its release's; source builds compare by the API only.
    #expect(Compatibility.check(hello(api, "v0.54.1-snapshot-abc1234"), appVersion: "0.54.1") == .ok)
    #expect(Compatibility.check(hello(api, "dev"), appVersion: "0.54.0") == .ok)
    #expect(Compatibility.check(hello(api, "v0.60.0"), appVersion: "0.0.0") == .ok)
    #expect(Compatibility.check(hello(api, "v0.60.0"), appVersion: nil) == .ok)
}

@Test func compatibilityMessages() {
    #expect(Compatibility.ok.message == nil)
    #expect(Compatibility.tmTooOld(tm: "v0.40.0", api: 1).message?.contains("tm update") == true)
    #expect(Compatibility.appBehind(tm: "v0.55.0", app: "0.54.0").message == "This app (0.54.0) is older than tm v0.55.0: update it with tm update.")
    #expect(Compatibility.tmBehind(tm: "v0.53.0", app: "").message?.hasPrefix("tm v0.53.0 is older than this app:") == true)
}

@Test func releaseParsing() {
    #expect(Release("v0.54.0") == Release(major: 0, minor: 54, patch: 0))
    #expect(Release("1.2.3-rc1") == Release(major: 1, minor: 2, patch: 3))
    #expect(Release("0.10.0")! > Release("0.9.9")!)
    for bad in ["dev", "52421c6-dirty", "1.2", "1.2.x", "", "v-1.0.0"] {
        #expect(Release(bad) == nil, "\(bad)")
    }
}

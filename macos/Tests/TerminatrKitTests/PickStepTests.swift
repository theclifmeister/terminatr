import Testing
@testable import TerminatrKit

@Test func pickStepWalksTheList() {
    let o = ["a", "b", "c"]
    #expect(PickStep.next(o, from: "b", up: true, down: false) == "a")
    #expect(PickStep.next(o, from: "b", up: false, down: true) == "c")
    #expect(PickStep.next(o, from: "a", up: true, down: false) == nil)
    #expect(PickStep.next(o, from: "c", up: false, down: true) == nil)
    #expect(PickStep.next(o, from: nil, up: false, down: true) == "a")
    #expect(PickStep.next(o, from: "gone", up: true, down: false) == "a")
    #expect(PickStep.next(o, from: "b", up: false, down: false) == nil)
    #expect(PickStep.next([String](), from: nil, up: false, down: true) == nil)
}

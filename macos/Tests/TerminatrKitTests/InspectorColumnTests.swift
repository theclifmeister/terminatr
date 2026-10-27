import Testing
@testable import TerminatrKit

@Test func inspectorColumnFitsTheToolbar() {
    // The toolbar's items over it with a two-digit count: 388 points.
    #expect(InspectorColumn.min >= 388)
    #expect(InspectorColumn.ideal(saved: nil) == InspectorColumn.min)
    // A width left narrower by an older release opens at the least.
    #expect(InspectorColumn.ideal(saved: 270) == InspectorColumn.min)
    #expect(InspectorColumn.ideal(saved: 450) == 450)
    #expect(InspectorColumn.ideal(saved: 900) == InspectorColumn.max)
}

@Test func inspectorColumnSavesOnlyWidthsInItsRange() {
    #expect(InspectorColumn.saving(InspectorColumn.min) == InspectorColumn.min)
    #expect(InspectorColumn.saving(450.4) == 450)
    #expect(InspectorColumn.saving(InspectorColumn.max) == InspectorColumn.max)
    // Sliding in or out: none kept.
    #expect(InspectorColumn.saving(0) == nil)
    #expect(InspectorColumn.saving(300) == nil)
    #expect(InspectorColumn.saving(InspectorColumn.max + 1) == nil)
}

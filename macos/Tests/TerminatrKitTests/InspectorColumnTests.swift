import Testing
@testable import TerminatrKit

@Test func inspectorPanelKeepsItsWidthInRange() {
    #expect(InspectorColumn.width(saved: nil) == InspectorColumn.ideal)
    #expect(InspectorColumn.width(saved: 120) == InspectorColumn.min)
    #expect(InspectorColumn.width(saved: 450) == 450)
    #expect(InspectorColumn.width(saved: 900) == 900)
}

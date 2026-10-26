import Foundation
import Testing
@testable import TerminatrKit

// Settings and the View menu (T748 milestone G): the stored choices,
// the text size's steps, New Task…'s form and the bridge log.

@Test func storedChoices() {
    #expect(Appearance.of(nil) == .system)
    #expect(Appearance.of("dark") == .dark)
    #expect(Appearance.of("sepia") == .system)
    #expect(Density.of(nil) == .regular)
    #expect(Density.of("roomy") == .roomy)
    #expect(Density.of("tight") == .regular)
    #expect(Density.compact.minRowHeight != nil && Density.regular.minRowHeight == nil)
    #expect(Density.roomy.rowPadding > Density.regular.rowPadding)
}

@Test func textSizeSteps() {
    #expect(TextSize.of(nil) == 1)
    #expect(TextSize.of(0) == 1)
    #expect(TextSize.of(5) == TextSize.steps.last)
    #expect(TextSize.of(0.1) == TextSize.steps.first)
    #expect(TextSize.step(1, by: 1) == 1.1)
    #expect(TextSize.step(1, by: -1) == 0.9)
    // Off a step (the slider's 1.05): from the nearest.
    #expect(TextSize.step(1.04, by: 1) == 1.1)
    // Held at the ends.
    #expect(TextSize.step(1.3, by: 1) == 1.3)
    #expect(TextSize.step(0.85, by: -1) == 0.85)
    #expect(TextSize.label(1.1) == "110%")
    #expect(TextSize.label(0.85) == "85%")
}

@Test func newTaskForm() {
    #expect(!NewTaskForm().canAdd)
    #expect(!NewTaskForm(title: "  \n ").canAdd)
    #expect(!NewTaskForm(title: "two\nlines").canAdd)
    let f = NewTaskForm(title: "  Cache layer \n", notes: "\nFor project.watch.\n")
    #expect(f.canAdd)
    #expect(f.params(project: "terminatr") == AppAPI.TaskAddParams(project: "terminatr", title: "Cache layer", notes: "For project.watch."))
    #expect(NewTaskForm.done(AppAPI.TaskAdded(task: "T9"), project: "toe") == "Added T9 to toe's board.")
    #expect(NewTaskForm.done(AppAPI.TaskAdded(task: "T2", existing: true), project: "toe") == "T2 is on toe's board already.")
}

@Test func bridgeLog() {
    var log = BridgeLog(limit: 3)
    let t0 = Date(timeIntervalSince1970: 0)
    for i in 1...5 {
        log.add("line \(i)", at: t0)
    }
    #expect(log.entries.map(\.text) == ["line 3", "line 4", "line 5"])
    #expect(Set(log.entries.map(\.id)).count == 3)
    #expect(log.text { _ in "t" } == "t  line 3\nt  line 4\nt  line 5")
}

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

/// New Task's templates (app API 20): task.add takes the ref; the sheet
/// shows the title the template makes, and names a template by its
/// <mod>:<name> only when another mod's has its name, as the TUI's.
@Test func newTaskTemplates() {
    let bug = AppAPI.Template(ref: "default-templates:bug", mod: "default-templates", name: "bug", title: "Bug: {title}",
                              steps: ["Reproduce", "Fix"], builtin: true)
    let mine = AppAPI.Template(ref: "mine:bug", mod: "mine", name: "bug", title: "{title} (bug)")
    let chore = AppAPI.Template(ref: "default-templates:chore", mod: "default-templates", name: "chore", title: "Chore: {title}")
    let f = NewTaskForm(title: " Login fails ", template: bug.ref)
    #expect(f.params(project: "web") == AppAPI.TaskAddParams(project: "web", title: "Login fails", template: "default-templates:bug"))
    #expect(NewTaskForm().params(project: "web").template == "")
    #expect(NewTaskForm.title(" Login fails ", with: bug) == "Bug: Login fails")
    #expect(NewTaskForm.title("", with: bug) == "Bug: …")
    #expect(NewTaskForm.title("Login", with: nil) == "Login")
    #expect(NewTaskForm.title("x", with: AppAPI.Template(ref: "m:t", mod: "m", name: "t", title: "Fixed")) == "Fixed")
    #expect(NewTaskForm.label(bug, among: [bug, mine, chore]) == "default-templates:bug")
    #expect(NewTaskForm.label(chore, among: [bug, mine, chore]) == "chore")
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

// Accessibility (T748 J): text styles keep Bloom's sizes at 100%, run
// from largest to smallest, and the sidebar follows the system's size.
@Test func textStyles() {
    #expect(TextStyle.body.points == 13)
    #expect(TextStyle.display.points == 30)
    let sizes = TextStyle.allCases.map(\.points)
    #expect(sizes == sizes.sorted(by: >))
    #expect(Set(sizes).count == sizes.count)
    #expect(SidebarSize.medium.scale == 1)
    #expect(SidebarSize.small.scale < 1 && SidebarSize.large.scale > 1)
}

@Test func spokenAttention() {
    #expect(Attention(word: "allow?", tone: .approval, why: "needs approval", waits: true).spoken == "needs approval")
    #expect(Attention(word: "asks", tone: .question, why: "asks a question · answer in Terminal", waits: true).spoken
        == "asks a question, answer in Terminal")
    #expect(Attention(word: "working", tone: .working, why: "Running tests", waits: false).spoken == "working")
    #expect(Attention(word: "blocked", tone: .attention, why: "", waits: true).spoken == "blocked")
}

// T845: a StateGlyph is filled for what waits on the user, and each
// tone is spoken, so its colour is never the only sign.
@Test func toneGlyphs() {
    let waiting: [Attention.Tone] = [.approval, .question, .attention, .review]
    let not: [Attention.Tone] = [.working, .idle, .done]
    #expect(waiting.map(\.waits) == [true, true, true, true])
    #expect(not.map(\.waits) == [false, false, false])
    #expect(Attention.Tone.approval.spoken == "needs approval")
}

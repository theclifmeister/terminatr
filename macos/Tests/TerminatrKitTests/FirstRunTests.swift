import Testing
@testable import TerminatrKit

@Test func firstRunShowsOnce() {
    #expect(FirstRun.shows(done: false, notificationsAsked: false))
    #expect(!FirstRun.shows(done: true, notificationsAsked: false))
    // Asked by an older release at launch: not a first run.
    #expect(!FirstRun.shows(done: false, notificationsAsked: true))
}

@Test func firstRunCommands() {
    #expect(FirstRun.addTask("demo") == #"tm task add "What to do" --project demo"#)
    #expect(FirstRun.newProject.hasPrefix("tm project new "))
    #expect(FirstRun.homebrew.split(separator: "\n").last == "brew install terminatr")
}

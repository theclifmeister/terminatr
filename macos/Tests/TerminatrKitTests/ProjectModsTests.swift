import Foundation
import Testing
@testable import TerminatrKit

/// The Mods page (T869) as the demo's acme-web has it: a mod that runs
/// here turns off at once, one that is off asks its consent, in tm's
/// lines ending in its question, a mod that runs for every project asks
/// before it is limited, and tm's own required mods have no button.
@Test func demoProjectMods() async throws {
    let b = try await demoBridge()
    let hello = try #require(await b.hello)
    #expect(Compatibility.listsMods(hello) && Compatibility.changesMods(hello))
    #expect(ProjectPage.pages(hello).last == .mods)
    let mods = try await b.call(AppAPI.Methods.modsList, AppAPI.ModsListParams(project: "acme-web")).mods
    let age = try #require(mods.first { $0.id == "pr-age" })
    #expect(age.runs && age.where == "on for this project" && age.own == "settings: 1 of its own")
    #expect(ProjectMods.act(age) == .off && ProjectMods.button(age) == "Turn off" && !ProjectMods.turnsOn(age))
    let days = try #require(age.settings.first { $0.key == "days" })
    #expect(days.own && ProjectMods.words(days.value) == "5" && ProjectMods.range(days) == "1 to 14")
    #expect(ProjectMods.help(days) == "Days before a pull request reads as old. Default 3.")
    #expect(ProjectMods.fits(days, 14) && !ProjectMods.fits(days, 15))
    let weekends = try #require(age.settings.first { $0.key == "weekends" })
    #expect(!weekends.own && ProjectMods.from(weekends) == "all projects" && ProjectMods.words(weekends.value) == "off")

    let deploy = try #require(mods.first { $0.id == "deploy-check" })
    #expect(ProjectMods.act(deploy) == .consent(question: "Enable Deploy check for acme-web?"))
    #expect(ProjectMods.button(deploy) == "Turn on…" && deploy.toggle.consent.isEmpty && !deploy.toggle.key.isEmpty)
    let set = ProjectMods.set(deploy, project: "acme-web")
    #expect(set.on && set.key == deploy.toggle.key && set.mod == "deploy-check")

    // Its consent, read with the list.
    let withConsent = try await b.call(AppAPI.Methods.modsList, AppAPI.ModsListParams(project: "acme-web", consent: "deploy-check")).mods
    let lines = try #require(withConsent.first { $0.id == "deploy-check" }).toggle.consent
    let text = ProjectMods.text(lines)
    #expect(text.hasPrefix("Deploy check 1.2.0 (deploy-check) by Ada"))
    #expect(text.contains("It may:") && text.hasSuffix("Enable Deploy check for acme-web?"))
    #expect(lines.last?.spans.first?.tone == "head")

    let every = try #require(mods.first { $0.id == "tasks-tab" })
    guard case .ask(let title, let question) = ProjectMods.act(every) else {
        Issue.record("tasks-tab: \(every.toggle)")
        return
    }
    #expect(title == "Turn Tasks off for acme-web" || title.hasPrefix("Turn "))
    #expect(question.contains("runs for every project") && ProjectMods.button(every) == "Turn off…")
    let required = try #require(mods.first { $0.required })
    #expect(ProjectMods.button(required) == nil)

    // Writes are the demo's to refuse.
    await #expect(throws: AppAPI.Error.self) {
        _ = try await b.call(AppAPI.Methods.modsSet, set)
    }
}

/// A setting's value goes as its type says, and nil follows all
/// projects again.
@Test func modSettingParams() throws {
    let m = AppAPI.ProjectMod(id: "ci", name: "CI", kinds: "columns", where: "", toggle: AppAPI.ModToggle(do: "off"))
    let p = ProjectMods.setting(m, project: "web", key: "days", ProjectMods.int(4))
    let json = try #require(String(data: JSONEncoder().encode(p), encoding: .utf8))
    #expect(json.contains("\"type\":\"int\"") && json.contains("\"int\":4"))
    let clear = try #require(String(data: JSONEncoder().encode(ProjectMods.setting(m, project: "web", key: "days", nil)), encoding: .utf8))
    #expect(!clear.contains("value"))
}

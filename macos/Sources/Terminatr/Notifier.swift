import AppKit
import TerminatrKit
import UserNotifications

/// The app's notifications (design T734 §6, R9; T748 F): one when a
/// need appears, per kind as the settings say, grouped by project and
/// withdrawn when it goes; none for the session shown in front. Quiet
/// times are macOS Focus's: nothing here holds them back. A
/// notification names the project and the kind of need, never the
/// agent's words unless the user opted in. A single-select question
/// that isn't an escape's gets its options as buttons, which answer
/// through session.answer with its guard. Mods' notices are claimed
/// with notify.claim, so each shows once over the TUI and the app.
@MainActor
final class Notifier: NSObject {
    /// Settings keys: whether a kind notifies (default on), whether
    /// mods' notices do, and the opt-in to the agents' words.
    static func key(_ kind: Need.Kind) -> String { "notify.\(kind.rawValue)" }
    static let noticesKey = "notify.notices"
    static let showTextKey = "notify.showText"

    private let model: AppModel
    private let center: UNUserNotificationCenter?
    private var tracker = NeedTracker()
    private var connection = 0
    /// The notices this app tried to claim, while the tray lists them.
    private var claimed: Set<String> = []
    /// The categories of the notifications out now, by id: each
    /// question's buttons are its own.
    private var categories: [String: UNNotificationCategory] = [:]

    init(model: AppModel) {
        self.model = model
        center = Bundle.main.isApp ? .current() : nil
        super.init()
        center?.delegate = self
        model.onNeeds = { [weak self] m in self?.update(m) }
    }

    /// Asks macOS for leave to notify, once: later calls answer what the
    /// user said.
    func authorize() async {
        _ = try? await center?.requestAuthorization(options: [.alert, .sound, .badge])
    }

    private static func on(_ key: String) -> Bool {
        UserDefaults.standard.object(forKey: key) as? Bool ?? true
    }

    private func update(_ m: AppModel) {
        if m.connection != connection {
            connection = m.connection
            tracker.reset()
        }
        let (fresh, gone) = tracker.update(m.needs)
        if !gone.isEmpty {
            center?.removeDeliveredNotifications(withIdentifiers: gone)
            for id in gone {
                categories["answer:\(id)"] = nil
            }
        }
        for n in NeedTracker.notified(fresh, front: m.frontSession) where Self.on(Self.key(n.kind)) {
            post(n)
        }

        // A notice gone from the tray (dismissed in the TUI, or old)
        // goes from Notification Centre too.
        let listed = Set(m.notices.map(\.id))
        let left = claimed.subtracting(listed).map { "notice:\($0)" }
        if !left.isEmpty {
            center?.removeDeliveredNotifications(withIdentifiers: left)
            for id in left {
                categories[id] = nil
            }
        }
        claimed.formIntersection(listed)
        guard Self.on(Self.noticesKey) else { return }
        for n in m.notices where !n.drawn && !claimed.contains(n.id) {
            claimed.insert(n.id)
            Task {
                if await m.claim(n.id) {
                    post(n)
                }
            }
        }
    }

    // MARK: posting

    private func post(_ n: Need) {
        let c = UNMutableNotificationContent()
        c.title = n.title
        c.subtitle = n.detail
        if UserDefaults.standard.bool(forKey: Self.showTextKey) {
            c.body = n.text
        }
        c.threadIdentifier = n.project
        c.sound = .default
        var info: [String: Any] = [Info.project: n.project, Info.session: n.session]
        if let a = n.answer {
            let id = "answer:\(n.id)"
            categories[id] = UNNotificationCategory(
                identifier: id,
                actions: a.options.enumerated().map { i, label in
                    UNNotificationAction(identifier: "\(Info.option)\(i)", title: label)
                },
                intentIdentifiers: []
            )
            c.categoryIdentifier = id
            info[Info.answerSession] = a.session
            info[Info.answerIndex] = a.index
            info[Info.answerQuestion] = a.question
            info[Info.answerOptions] = a.options
            info[Info.who] = n.title
        }
        c.userInfo = info
        send(n.id, c)
    }

    /// A mod's notice, titled by its mod as the TUI's are ("terminatr ·
    /// mod"), its title and text the body. Its question.answer buttons
    /// become actions; its task buttons need the core's confirmation, so
    /// they stay in the TUI (M on the dashboard), and a click opens the
    /// app.
    private func post(_ n: AppAPI.Notice) {
        let c = UNMutableNotificationContent()
        c.title = "terminatr · \(n.mod)"
        c.body = n.text.isEmpty ? n.title : "\(n.title): \(n.text)"
        c.threadIdentifier = "notice"
        let answers = n.buttons.filter { $0.id == "question.answer" && !$0.session.isEmpty }.prefix(Need.maxButtons)
        let id = "notice:\(n.id)"
        categories[id] = UNNotificationCategory(
            identifier: id,
            actions: answers.enumerated().map { i, b in
                UNNotificationAction(identifier: "\(Info.option)\(i)", title: b.option.isEmpty ? b.label : "\(b.label) “\(b.option)”")
            },
            intentIdentifiers: [],
            options: .customDismissAction
        )
        c.categoryIdentifier = id
        c.userInfo = [
            Info.notice: n.id,
            Info.noticeButtons: answers.map { [$0.session, $0.question, $0.option] },
            Info.project: answers.first?.project ?? "",
        ]
        send(id, c)
    }

    /// A word of how a button press went, when it didn't.
    private func tell(_ title: String, _ why: String) {
        let c = UNMutableNotificationContent()
        c.title = title
        c.body = why
        send("failed:\(UUID().uuidString)", c)
    }

    private func send(_ id: String, _ c: UNMutableNotificationContent) {
        guard let center else { return }
        center.setNotificationCategories(Set(categories.values))
        center.add(UNNotificationRequest(identifier: id, content: c, trigger: nil))
    }

    // MARK: answering

    private func respond(_ r: Response) async {
        if let notice = r.notice {
            await respond(notice: notice, r)
            return
        }
        guard let i = r.option else {
            model.open(project: r.project, session: r.session)
            return
        }
        guard let session = r.answerSession, let index = r.answerIndex, let question = r.answerQuestion,
              i < r.answerOptions.count
        else { return }
        let params = AppAPI.AnswerParams(id: session, index: index, answer: r.answerOptions[i], question: question)
        do {
            _ = try await model.answer(params)
        } catch {
            tell("\(r.who.isEmpty ? "The question" : r.who): not answered", error.localizedDescription)
        }
    }

    private func respond(notice id: String, _ r: Response) async {
        switch r.action {
        case UNNotificationDismissActionIdentifier:
            await model.dismiss(id)
        case UNNotificationDefaultActionIdentifier:
            model.open(project: r.project, session: r.session)
        default:
            guard let i = r.option, i < r.noticeButtons.count, r.noticeButtons[i].count == 3 else { return }
            let (session, question, option) = (r.noticeButtons[i][0], r.noticeButtons[i][1], r.noticeButtons[i][2])
            // The index is the live menu's; the guard is the question's
            // text, so a menu replaced since gets nothing.
            let open = model.sessions.first { $0.id == session }?.question?.questions
            guard let index = open?.firstIndex(where: { $0.question == question && !$0.answered }) else {
                tell("Not answered", "That question isn't open any more.")
                return
            }
            do {
                _ = try await model.answer(AppAPI.AnswerParams(id: session, index: index, answer: option, question: question))
                await model.dismiss(id)
            } catch {
                tell("Not answered", error.localizedDescription)
            }
        }
    }
}

/// The userInfo keys of the app's notifications.
private enum Info {
    static let project = "project"
    static let session = "session"
    static let who = "who"
    static let option = "option."
    static let answerSession = "answer.session"
    static let answerIndex = "answer.index"
    static let answerQuestion = "answer.question"
    static let answerOptions = "answer.options"
    static let notice = "notice"
    static let noticeButtons = "notice.buttons"
}

/// A notification's response, read out of its userInfo before it
/// crosses to the main actor.
private struct Response: Sendable {
    let action: String
    let option: Int?
    let project: String
    let session: String
    let who: String
    let answerSession: String?
    let answerIndex: Int?
    let answerQuestion: String?
    let answerOptions: [String]
    let notice: String?
    let noticeButtons: [[String]]

    init(_ r: UNNotificationResponse) {
        let info = r.notification.request.content.userInfo
        action = r.actionIdentifier
        option = action.hasPrefix(Info.option) ? Int(action.dropFirst(Info.option.count)) : nil
        project = info[Info.project] as? String ?? ""
        session = info[Info.session] as? String ?? ""
        who = info[Info.who] as? String ?? ""
        answerSession = info[Info.answerSession] as? String
        answerIndex = info[Info.answerIndex] as? Int
        answerQuestion = info[Info.answerQuestion] as? String
        answerOptions = info[Info.answerOptions] as? [String] ?? []
        notice = info[Info.notice] as? String
        noticeButtons = info[Info.noticeButtons] as? [[String]] ?? []
    }
}

extension Notifier: UNUserNotificationCenterDelegate {
    /// Banners show while the app is in front too.
    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification) async -> UNNotificationPresentationOptions {
        [.banner, .list, .sound]
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse) async {
        let r = Response(response)
        await respond(r)
    }
}

extension Bundle {
    /// Running as Terminatr.app: UserNotifications and SMAppService need
    /// the bundle (under swift run there is none, and the centre traps).
    var isApp: Bool {
        bundleURL.pathExtension == "app" && bundleIdentifier != nil
    }
}

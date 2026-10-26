import Foundation

/// The first-run sheet (T748 milestone H): where tm is (or how to
/// install it), whether the server runs, and what the notifications are
/// for, before macOS asks for them. It shows once.
public enum FirstRun {
    /// The settings key set once the sheet was closed.
    public static let doneKey = "firstRunDone"

    /// Whether the app opens on the sheet: not once it was closed, nor
    /// for a user macOS already asked about notifications (an older
    /// release asked at launch: they know the app).
    public static func shows(done: Bool, notificationsAsked: Bool) -> Bool {
        !done && !notificationsAsked
    }

    /// The ways to install tm, as the README has them: Homebrew, or the
    /// install script into ~/.local/bin.
    public static let homebrew = """
        brew tap theclifmeister/terminatr https://github.com/theclifmeister/terminatr
        brew trust theclifmeister/terminatr
        brew install terminatr
        """
    public static let script = "curl -fsSL https://github.com/theclifmeister/terminatr/releases/latest/download/install.sh | sh"

    /// The command that makes a project, for the no-projects state.
    public static let newProject = #"tm project new my-project --goal "What it is for" --repo ~/code/my-repo"#

    /// The command that adds a task to project's board.
    public static func addTask(_ project: String) -> String {
        #"tm task add "What to do" --project "# + project
    }
}

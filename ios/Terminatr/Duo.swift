import SwiftUI
import UIKit

/// iPhone Duo (T824): the app is portrait only on an iPhone, but on a
/// phone with a hinge it turns to landscape too, which the outer display
/// honours (the inner one ignores the lock anyway). Info.plist allows
/// both; this delegate narrows it to portrait until a hinge is seen
/// (onHinge), as no API names the device. An iPad (T830) turns all four
/// ways, as its multitasking (Split View, Stage Manager) expects.
final class AppDelegate: NSObject, UIApplicationDelegate {
    @MainActor static var hinged = false

    func application(_ application: UIApplication, supportedInterfaceOrientationsFor window: UIWindow?) -> UIInterfaceOrientationMask {
        if UIDevice.current.userInterfaceIdiom == .pad { return .all }
        return Self.hinged ? .allButUpsideDown : .portrait
    }
}

extension View {
    /// Notes whether the phone has a hinge, for AppDelegate's orientations.
    func onHinge() -> some View {
        modifier(HingeModifier())
    }
}

private struct HingeModifier: ViewModifier {
    func body(content: Content) -> some View {
        if #available(iOS 27.1, *) {
            content.onHingeChange { _, new in
                let hinged = new.hinge != nil
                guard hinged != AppDelegate.hinged else { return }
                AppDelegate.hinged = hinged
                for case let s as UIWindowScene in UIApplication.shared.connectedScenes {
                    for w in s.windows { w.rootViewController?.setNeedsUpdateOfSupportedInterfaceOrientations() }
                }
            }
        } else {
            content
        }
    }
}

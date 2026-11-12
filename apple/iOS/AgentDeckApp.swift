import SwiftUI
import UIKit

final class AppPushDelegate: NSObject, UIApplicationDelegate {
    func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        PromptNotifier.shared.setAPNSToken(deviceToken)
    }
}

@main
struct AgentDeckApp: App {
    @UIApplicationDelegateAdaptor(AppPushDelegate.self) private var appDelegate

    init() {
        // O delegate tem de existir antes de o iOS entregar a ação de uma notificação.
        PromptNotifier.shared.activate()
    }

    var body: some Scene {
        WindowGroup {
            ContentView()
        }
    }
}

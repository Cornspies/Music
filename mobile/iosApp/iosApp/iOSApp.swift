import SwiftUI
import ComposeApp

@main
struct iOSApp: App {
    var body: some Scene {
        WindowGroup {
            ContentView()
                .ignoresSafeArea(.container, edges: .vertical)
                .onOpenURL { url in
                    DeepLinks.shared.handle(url: url.absoluteString)
                }
        }
    }
}

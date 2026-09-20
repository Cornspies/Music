import SwiftUI
import ComposeApp

@main
struct iOSApp: App {
    var body: some Scene {
        WindowGroup {
            ContentView()
                // Universal links (https://<host>/playlists/…, /register?invite=…)
                // and the music:// scheme both arrive here, on cold start as
                // well as while running. The shared code parses them and
                // navigates once a session exists.
                .onOpenURL { url in
                    DeepLinks.shared.handle(url: url.absoluteString)
                }
        }
    }
}

import SwiftUI
import ComposeApp

@main
struct iOSApp: App {
    var body: some Scene {
        WindowGroup {
            // Compose draws under the status bar and home indicator and pads
            // for them itself, like on Android. Otherwise the screen ends above
            // the home indicator and a strip of plain window shows below the
            // tab bar. The sides stay in SwiftUI's hands (the notch in
            // landscape), and so does the keyboard.
            ContentView()
                .ignoresSafeArea(.container, edges: .vertical)
                .onOpenURL { url in
                    DeepLinks.shared.handle(url: url.absoluteString)
                }
        }
    }
}

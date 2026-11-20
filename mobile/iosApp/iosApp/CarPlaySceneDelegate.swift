import CarPlay
import ComposeApp

// CarPlay's way into the app, named in Info.plist. The car's screens are built
// in Kotlin: composeApp/src/iosMain/kotlin/com/music/app/CarScreens.kt.
final class CarPlaySceneDelegate: UIResponder, CPTemplateApplicationSceneDelegate {
    func templateApplicationScene(
        _ templateApplicationScene: CPTemplateApplicationScene,
        didConnect interfaceController: CPInterfaceController
    ) {
        CarScreens.shared.connect(controller: interfaceController)
    }

    func templateApplicationScene(
        _ templateApplicationScene: CPTemplateApplicationScene,
        didDisconnectInterfaceController interfaceController: CPInterfaceController
    ) {
        CarScreens.shared.disconnect()
    }
}

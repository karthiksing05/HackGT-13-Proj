import SwiftUI

@main
struct SideQuestzApp: App {
    @State private var env: AppEnvironment
    @State private var router: Router

    init() {
        let env = AppEnvironment.makeDefault()
        let defaults = UserDefaults.standard
        // The intro used to play only once per install; this flag is no longer used.
        defaults.removeObject(forKey: "hasSeenIntro")
        // UI tests start signed out: `-SQResetSession YES`.
        if defaults.bool(forKey: "SQResetSession") { env.auth.clear() }
        // The intro plays on every cold launch (not when returning from the background). UI tests
        // and screenshots skip it with `-SQSkipIntro YES`; `-SQRoute` deep links skip it too.
        let skipIntro = defaults.bool(forKey: "SQSkipIntro")

        let router = Router(phase: !skipIntro ? .splash : env.auth.isSignedIn ? .main : .auth)
        if let launch = LaunchRoute.fromLaunchArguments() {
            router.applyLaunch(launch, env: env)
        }
        _env = State(initialValue: env)
        _router = State(initialValue: router)
    }

    var body: some Scene {
        WindowGroup {
            RootView()
                .environment(env)
                .environment(router)
                .tint(Theme.sageInk)
                .dynamicTypeSize(...DynamicTypeSize.accessibility2)
                .task {
                    if env.auth.isSignedIn { await env.refreshSession() }
                }
        }
    }
}

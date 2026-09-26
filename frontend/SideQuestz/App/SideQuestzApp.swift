import SwiftUI

@main
struct SideQuestzApp: App {
    @State private var env: AppEnvironment
    @State private var router: Router

    init() {
        let env = AppEnvironment.makeDefault()
        let defaults = UserDefaults.standard
        if defaults.bool(forKey: "SQResetIntro") { defaults.set(false, forKey: "hasSeenIntro") }
        // UI tests start signed out: `-SQResetSession YES`.
        if defaults.bool(forKey: "SQResetSession") { env.auth.clear() }
        let hasSeenIntro = defaults.bool(forKey: "hasSeenIntro")

        let router = Router(phase: !hasSeenIntro ? .splash : env.auth.isSignedIn ? .main : .auth)
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

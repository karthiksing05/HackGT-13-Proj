import Observation
import SwiftUI
import UIKit

/// App-wide services and session state. One instance, injected with `.environment(env)` and read
/// in views as `@Environment(AppEnvironment.self) private var env`.
///
/// Mock is the default for the demo build. Switch to the real backend with `SQAPIMode = live` in
/// Info.plist (plus `SQAPIBaseURL` / `SQWebSocketURL`), or at launch with `-SQAPIMode live`.
@Observable
final class AppEnvironment {
    enum Mode: String { case mock, live }

    let mode: Mode
    let clock: AppClock
    let api: any APIClient
    let auth: AuthStore
    let realtime: RealtimeHub
    let voice: VoiceInput
    let places: PlaceSearch
    let location: LocationService
    @ObservationIgnored private let socket: WebSocketService?

    // MARK: Session (shared by Home header, Account, Setup, Photo sheet…)

    var user: User?
    /// The profile photo picked on this device (shown immediately while/after uploading).
    var profileImage: UIImage?
    var preferences: Preferences?

    var isMock: Bool { mode == .mock }
    var format: TimeFormat { TimeFormat(clock: clock) }

    init(mode: Mode, clock: AppClock, api: any APIClient, auth: AuthStore, socketURL: URL?, wisprAPIKey: String?, forceVoiceDemo: Bool) {
        self.mode = mode
        self.clock = clock
        self.api = api
        self.auth = auth
        let hub = RealtimeHub()
        self.realtime = hub
        self.voice = VoiceInput(wisprAPIKey: wisprAPIKey, allowDemoFallback: mode == .mock, forceDemo: forceVoiceDemo)
        self.places = PlaceSearch(api: api, isMock: mode == .mock)
        self.location = LocationService(isMock: mode == .mock)
        self.socket = mode == .live ? socketURL.map { WebSocketService(url: $0, hub: hub) } : nil
    }

    /// Reads Info.plist (`SQAPIMode`, `SQAPIBaseURL`, `SQWebSocketURL`, `SQWisprFlowAPIKey`) and launch
    /// arguments (`-SQAPIMode live`, `-SQMockFail forum`, `-SQMockLatency 0`, `-SQVoiceDemo YES`).
    static func makeDefault() -> AppEnvironment {
        let info = Bundle.main.infoDictionary ?? [:]
        let defaults = UserDefaults.standard
        let modeString = defaults.string(forKey: "SQAPIMode") ?? info["SQAPIMode"] as? String ?? "mock"
        let mode = Mode(rawValue: modeString) ?? .mock
        let auth = AuthStore()
        let clock: AppClock = mode == .mock ? .demo : .live
        let wisprKey = defaults.string(forKey: "SQWisprFlowAPIKey") ?? info["SQWisprFlowAPIKey"] as? String
        let forceVoiceDemo = defaults.bool(forKey: "SQVoiceDemo")

        let api: any APIClient
        switch mode {
        case .mock:
            let failing = Set((defaults.string(forKey: "SQMockFail") ?? "").split(separator: ",").map { String($0).trimmingCharacters(in: .whitespaces) })
            let latency = defaults.object(forKey: "SQMockLatency") != nil ? defaults.double(forKey: "SQMockLatency") : 1
            api = MockAPIClient(clock: clock, latencyScale: latency, failing: failing)
        case .live:
            let base = URL(string: defaults.string(forKey: "SQAPIBaseURL") ?? info["SQAPIBaseURL"] as? String ?? "") ?? URL(string: "http://127.0.0.1:8000")!
            api = LiveAPIClient(baseURL: base, auth: auth, clock: clock)
        }
        let socketURL = URL(string: defaults.string(forKey: "SQWebSocketURL") ?? info["SQWebSocketURL"] as? String ?? "")
        return AppEnvironment(mode: mode, clock: clock, api: api, auth: auth, socketURL: socketURL, wisprAPIKey: wisprKey, forceVoiceDemo: forceVoiceDemo)
    }

    /// Test/preview environment: mock backend, no delay.
    static func preview() -> AppEnvironment {
        let env = AppEnvironment(mode: .mock, clock: .demo, api: MockAPIClient(clock: .demo, latencyScale: 0), auth: AuthStore(service: "preview"),
                                 socketURL: nil, wisprAPIKey: nil, forceVoiceDemo: true)
        env.user = MockData.user()
        env.preferences = MockData.preferences
        return env
    }

    // MARK: Session lifecycle

    /// After login / signup: store tokens, remember the user, connect realtime.
    func startSession(_ response: AuthResponse) {
        auth.save(response.tokens)
        user = response.user
        socket?.connect(token: response.tokens.accessToken)
        Task { await refreshSession() }
    }

    /// Mock-only shortcut used by demo launch routes: signs in as Jordan Lee.
    func startDemoSessionIfNeeded() {
        guard isMock else { return }
        if !auth.isSignedIn {
            auth.save(AuthTokens(accessToken: "mock-access", refreshToken: "mock-refresh", expiresAt: nil))
        }
        if user == nil { user = MockData.user() }
    }

    /// Reloads the current user and preferences (on launch when already signed in).
    func refreshSession() async {
        if let me = try? await api.me() { user = me }
        if let prefs = try? await api.preferences() { preferences = prefs }
        if !isMock, let token = auth.tokens?.accessToken { socket?.connect(token: token) }
    }

    func signOut() async {
        try? await api.logout()
        auth.clear()
        socket?.disconnect()
        voice.cancel()
        user = nil
        profileImage = nil
        preferences = nil
    }

    /// Account › Your status. Optimistic, then confirmed by the server.
    func setStatus(_ status: PresenceStatus) async {
        user?.status = status
        if let updated = try? await api.updateMe(UserPatch(status: status)) { user = updated }
    }
}

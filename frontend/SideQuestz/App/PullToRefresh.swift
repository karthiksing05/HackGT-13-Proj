import SwiftUI

private struct ReloadableModifier: ViewModifier {
    @Environment(AppEnvironment.self) private var env
    let key: String
    let reload: @MainActor () async -> Void

    func body(content: Content) -> some View {
        content
            .onAppear { env.registerReload(key, reload) }
            .onDisappear { env.unregisterReload(key) }
    }
}

private struct PullToRefreshModifier: ViewModifier {
    @Environment(AppEnvironment.self) private var env

    func body(content: Content) -> some View {
        content.refreshable { await env.reloadAll() }
    }
}

extension View {
    /// Registers how this screen reloads its API data, so a pull to refresh anywhere reloads it too.
    /// `reload` should keep what's on screen and swap in the fresh data (no skeleton flash), and keep
    /// the old data if the request fails. Use a unique `key` per screen ("forum", "thread.g1"…).
    func sqReloadable(_ key: String, reload: @escaping @MainActor () async -> Void) -> some View {
        modifier(ReloadableModifier(key: key, reload: reload))
    }

    /// Swipe down to reload everything from the API (every live screen plus the profile). Put it on
    /// the screen's main `ScrollView` / `List`.
    func sqPullToRefresh() -> some View {
        modifier(PullToRefreshModifier())
    }
}

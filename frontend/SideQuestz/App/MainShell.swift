import SwiftUI
import UIKit

/// Main: a ZStack of the current tab + the custom tab bar. All four tabs stay alive (so scroll
/// positions and filters survive tab switches); only the selected one is visible.
struct MainShell: View {
    @Environment(AppEnvironment.self) private var env
    @Environment(Router.self) private var router
    @State private var inviteAlert: MainInviteAlert?
    /// The keyboard is up (Home search, Friends search…): the tab bar steps aside, like a system
    /// tab bar staying behind the keyboard.
    @State private var keyboardUp = false

    var body: some View {
        @Bindable var router = router
        ZStack(alignment: .bottom) {
            tabView(.home) { HomeView() }
            tabView(.forum) { ForumView() }
            tabView(.groups) { GroupsView() }
            tabView(.account) { AccountView() }

            SQTabBar(selection: router.tab, onSelect: { router.select($0) }, onPlan: { router.openCreate() })
                .opacity(router.openThread == nil && !keyboardUp ? 1 : 0)
                .allowsHitTesting(router.openThread == nil && !keyboardUp)
                .ignoresSafeArea(.keyboard, edges: .bottom)
                .zIndex(1.5)

            if let thread = router.openThread {
                ThreadView(route: thread)
                    .transition(.move(edge: .trailing))
                    .zIndex(2)
            }
        }
        .fullScreenCover(item: $router.createDraft) { draft in
            CreateFlowView(draft: draft)
        }
        .fullScreenCover(item: $router.setupRedo) { entry in
            SetupFlowView(startStep: entry.step, isRedo: true)
        }
        .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillShowNotification)) { _ in
            withAnimation(.easeOut(duration: 0.15)) { keyboardUp = true }
        }
        .onReceive(NotificationCenter.default.publisher(for: UIResponder.keyboardWillHideNotification)) { _ in
            withAnimation(.easeOut(duration: 0.2)) { keyboardUp = false }
        }
        // An invite link opened (possibly before signing in): accept it now.
        .task(id: router.pendingInvite) { await acceptPendingInvite() }
        .alert(inviteAlert?.title ?? "", isPresented: Binding(get: { inviteAlert != nil }, set: { if !$0 { inviteAlert = nil } }),
               presenting: inviteAlert) { _ in
            Button("OK", role: .cancel) {}
        } message: { alert in
            Text(alert.message)
        }
    }

    private func acceptPendingInvite() async {
        guard let code = router.pendingInvite else { return }
        do {
            let friend = try await env.api.acceptInvite(code: code)
            router.pendingInvite = nil
            inviteAlert = MainInviteAlert(title: "You're friends now",
                                          message: "You and \(friend.person.name) can plan together and see each other in the Forum.")
            await env.reloadAll()
        } catch {
            router.pendingInvite = nil
            inviteAlert = MainInviteAlert(title: "Couldn't use that invite",
                                          message: (error as? LocalizedError)?.errorDescription ?? "Ask for a new link and try again.")
        }
    }

    private func tabView<Content: View>(_ tab: Router.Tab, @ViewBuilder content: () -> Content) -> some View {
        let selected = router.tab == tab
        return content()
            .safeAreaInset(edge: .bottom, spacing: 0) { Color.clear.frame(height: keyboardUp ? 0 : Metrics.tabBarContentHeight) }
            // The status bar keeps the screen's cream, so scrolled content never runs under the clock.
            .overlay(alignment: .top) {
                Color.clear
                    .frame(height: 0)
                    .background(Theme.cream.ignoresSafeArea(edges: .top))
                    .allowsHitTesting(false)
                    .accessibilityHidden(true)
            }
            // Like the prototype, tab content ends at the tab bar instead of showing through it.
            .mask(alignment: .top) {
                Rectangle().padding(.bottom, keyboardUp ? 0 : Metrics.tabBarContentHeight).ignoresSafeArea(edges: .top)
            }
            .opacity(selected ? 1 : 0)
            .allowsHitTesting(selected)
            // An open thread covers the tabs: VoiceOver shouldn't reach the list underneath.
            .accessibilityHidden(!selected || router.openThread != nil)
            .zIndex(selected ? 1 : 0)
    }
}

private struct MainInviteAlert {
    let title: String
    let message: String
}

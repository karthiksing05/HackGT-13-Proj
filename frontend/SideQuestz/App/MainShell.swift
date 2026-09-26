import SwiftUI

/// Main: a ZStack of the current tab + the custom tab bar. All four tabs stay alive (so scroll
/// positions and filters survive tab switches); only the selected one is visible.
struct MainShell: View {
    @Environment(Router.self) private var router

    var body: some View {
        @Bindable var router = router
        ZStack(alignment: .bottom) {
            tabView(.home) { HomeView() }
            tabView(.forum) { ForumView() }
            tabView(.groups) { GroupsView() }
            tabView(.account) { AccountView() }

            SQTabBar(selection: router.tab, onSelect: { router.select($0) }, onPlan: { router.openCreate() })
                .opacity(router.openThread == nil ? 1 : 0)
                .allowsHitTesting(router.openThread == nil)
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
    }

    private func tabView<Content: View>(_ tab: Router.Tab, @ViewBuilder content: () -> Content) -> some View {
        let selected = router.tab == tab
        return content()
            .safeAreaInset(edge: .bottom, spacing: 0) { Color.clear.frame(height: Metrics.tabBarContentHeight) }
            .opacity(selected ? 1 : 0)
            .allowsHitTesting(selected)
            .accessibilityHidden(!selected)
            .zIndex(selected ? 1 : 0)
    }
}

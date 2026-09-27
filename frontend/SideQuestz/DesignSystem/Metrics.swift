import SwiftUI

/// Spacing, sizes and radii from GUI_PLAN.md §4.3. CSS px in the prototype = points here.
enum Metrics {
    /// Screen side padding.
    static let side: CGFloat = 20
    /// Prototype frame width. Full-width cards are `screenWidth - 2 × side` (350 on a 390pt phone).
    static let designWidth: CGFloat = 390

    // Cards
    static let cardRadius: CGFloat = 16
    static let cardPadding: CGFloat = 14

    // Buttons
    static let buttonHeight: CGFloat = 52
    static let buttonRadius: CGFloat = 14
    static let fieldRadius: CGFloat = 14

    // Sheets
    static let sheetRadius: CGFloat = 24

    // Tab bar: 84 tall on a 34pt home-indicator phone → 50 above the safe area.
    static let tabBarContentHeight: CGFloat = 50
    static let plusButtonSize: CGFloat = 58

    /// The tab bar's height above the bottom safe area. A phone without a home indicator (iPhone SE)
    /// has nothing below the bar, so it gets 10pt more: the raised +'s "Plan" label and the
    /// selected tab's shade then end above the screen's edge instead of running off it.
    static func tabBarHeight(safeAreaBottom: CGFloat) -> CGFloat {
        tabBarContentHeight + (safeAreaBottom > 0 ? 0 : 10)
    }

    /// Minimum touch target.
    static let minTouch: CGFloat = 44
}

// MARK: - Safe-area-aware "design top" padding

/// The prototype measures headers from the very top of a 390×844 frame (e.g. "padding-top: 56px"
/// for tab screens, 50px for flow headers). On device the status bar already takes part of that,
/// so we pad `max(designTop − safeAreaTop, minimum)` below the safe area.
private struct SafeAreaTopKey: EnvironmentKey {
    static let defaultValue: CGFloat = 47
}

private struct SafeAreaBottomKey: EnvironmentKey {
    static let defaultValue: CGFloat = 34
}

extension EnvironmentValues {
    /// Top safe-area inset of the window (status bar / Dynamic Island). Set once by `RootView`.
    var safeAreaTop: CGFloat {
        get { self[SafeAreaTopKey.self] }
        set { self[SafeAreaTopKey.self] = newValue }
    }

    /// Bottom safe-area inset of the window (home indicator). Set once by `RootView`.
    var safeAreaBottom: CGFloat {
        get { self[SafeAreaBottomKey.self] }
        set { self[SafeAreaBottomKey.self] = newValue }
    }
}

private struct DesignTopPadding: ViewModifier {
    @Environment(\.safeAreaTop) private var safeTop
    let designTop: CGFloat
    let minimum: CGFloat

    func body(content: Content) -> some View {
        content.padding(.top, max(designTop - safeTop, minimum))
    }
}

extension View {
    /// Pads so this view sits `designTop` points from the top of the screen, as in the prototype,
    /// while staying below the status bar. Apply to content that already respects the top safe area.
    func designTopPadding(_ designTop: CGFloat, minimum: CGFloat = 6) -> some View {
        modifier(DesignTopPadding(designTop: designTop, minimum: minimum))
    }
}

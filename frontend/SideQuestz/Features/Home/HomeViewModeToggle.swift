import SwiftUI

/// Timeline | Map for the selected sidequest (Home › Sidequests header): a small pill in the app's
/// segmented look, a white thumb on `segmentBg`. Tap a side, or swipe across the pill (the thumb
/// follows your finger), and the thumb slides over. VoiceOver reads it as one picker ("View,
/// Timeline"): swipe up or down to change it, or double-tap to switch.
struct HomeViewModeToggle: View {
    @Binding var showsMap: Bool
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    /// How far a finger has dragged the thumb from where it rests; it glides back when released.
    @GestureState(resetTransaction: Transaction(animation: UIAccessibility.isReduceMotionEnabled ? Motion.reduced : Motion.quick))
    private var drag: CGFloat = 0

    static let size = CGSize(width: 76, height: 30)
    private static let inset: CGFloat = 2
    private static var thumbWidth: CGFloat { (size.width - 2 * inset) / 2 }

    var body: some View {
        let travel = Self.thumbWidth
        let offset = min(max((showsMap ? travel : 0) + drag, 0), travel)
        let mapSide = offset >= travel / 2
        ZStack(alignment: .leading) {
            Capsule()
                .fill(Theme.segmentBg)
            Capsule()
                .fill(.white)
                .shadow(color: .black.opacity(0.12), radius: 1.5, x: 0, y: 1)
                .frame(width: Self.thumbWidth, height: Self.size.height - 2 * Self.inset)
                .offset(x: Self.inset + offset)
            HStack(spacing: 0) {
                segment("calendar.day.timeline.left", isOn: !mapSide)
                segment("map", isOn: mapSide)
            }
            .padding(.horizontal, Self.inset)
        }
        .frame(width: Self.size.width, height: Self.size.height)
        // A 44pt-tall touch target without making the header taller.
        .contentShape(Rectangle().inset(by: -7))
        .gesture(tapOrSwipe)
        .animation(reduceMotion ? Motion.reduced : Motion.quick, value: showsMap)
        .sensoryFeedback(.selection, trigger: showsMap)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("View")
        .accessibilityValue(showsMap ? "Map" : "Timeline")
        .accessibilityHint("Shows this sidequest's timeline or its map")
        .accessibilityAdjustableAction { direction in
            switch direction {
            case .increment: showsMap = true
            case .decrement: showsMap = false
            @unknown default: break
            }
        }
        .accessibilityAction { showsMap.toggle() }
        .accessibilityIdentifier("sidequest.viewMode")
    }

    private func segment(_ symbol: String, isOn: Bool) -> some View {
        Image(systemName: symbol)
            .font(.system(size: 13, weight: .semibold))
            .foregroundStyle(isOn ? Theme.ink : Theme.text3)
            .frame(width: Self.thumbWidth, height: Self.size.height)
            .animation(reduceMotion ? Motion.reduced : Motion.quick, value: isOn)
    }

    /// A tap picks the side it lands on; a swipe drags the thumb and settles on the side it's
    /// thrown toward.
    private var tapOrSwipe: some Gesture {
        DragGesture(minimumDistance: 0)
            .updating($drag) { value, drag, _ in
                // Taps stay put; only a swipe moves the thumb.
                if abs(value.translation.width) > 4 { drag = value.translation.width }
            }
            .onEnded { value in
                let target: Bool
                if abs(value.translation.width) > 4 {
                    let rest = showsMap ? Self.thumbWidth : 0
                    target = rest + value.predictedEndTranslation.width >= Self.thumbWidth / 2
                } else {
                    target = value.location.x >= Self.size.width / 2
                }
                guard target != showsMap else { return }
                withAnimation(reduceMotion ? Motion.reduced : Motion.quick) { showsMap = target }
            }
    }
}

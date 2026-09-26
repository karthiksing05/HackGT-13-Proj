import SwiftUI

/// Setup · 3 "What do you enjoy?": a 1–5 rating per trip type, then company and pace.
struct SetupLikesStep: View {
    @Bindable var draft: SetupDraft

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            SetupHeading(title: "What do you enjoy?",
                         subtitle: "Rate each from 1 (not for me) to 5 (love it). Skip any you're not sure about.")
            if !draft.facebookFilled.isEmpty {
                FacebookFilledNote()
            }
            SetupCard {
                ForEach(TripType.allCases) { type in
                    SetupRatingRow(type: type, value: rating(for: type))
                    RowDivider(color: Theme.cream)
                }
            }

            SetupEyebrow(text: "WHO DO YOU USUALLY GO WITH?")
                .padding(.top, 6)
            SetupChoiceGrid(options: Company.allCases, columns: 3, selection: $draft.preferences.company) { $0.label }

            SetupEyebrow(text: "YOUR USUAL PACE")
                .padding(.top, 6)
            SetupChoiceGrid(options: Pace.allCases, columns: 3, selection: $draft.preferences.pace) { $0.setupLabel }
        }
    }

    /// 0 = not rated.
    private func rating(for type: TripType) -> Binding<Int> {
        Binding(
            get: { draft.preferences.ratings[type] ?? 0 },
            set: { draft.preferences.ratings[type] = $0 == 0 ? nil : $0 }
        )
    }
}

/// One trip type: name + "4 · Like it" / "Not rated", then five 36pt buttons. Tapping the
/// selected number clears the row. VoiceOver: one adjustable element (swipe up/down).
private struct SetupRatingRow: View {
    let type: TripType
    @Binding var value: Int
    /// The number that was just picked pops for a moment.
    @State private var popped: Int?
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        VStack(spacing: 8) {
            HStack(alignment: .firstTextBaseline) {
                Text(type.label)
                    .sqFont(15, .semibold)
                    .foregroundStyle(Theme.ink)
                Spacer(minLength: 8)
                Text(valueLabel)
                    .sqFont(12, .semibold)
                    .foregroundStyle(value > 0 ? Theme.sageInk : Theme.text3)
            }
            .authLineHeight(1.35, size: 15)
            HStack(spacing: 6) {
                ForEach(1...5, id: \.self) { n in
                    Button {
                        pick(n)
                    } label: {
                        Text("\(n)")
                            .sqFont(15, .semibold)
                            .foregroundStyle(Theme.ink)
                            .frame(maxWidth: .infinity)
                            .frame(height: 36)
                            .background {
                                // Only the fill pops (the digit stays put).
                                RoundedRectangle(cornerRadius: 10, style: .continuous)
                                    .fill(fill(for: n))
                                    .scaleEffect(popped == n ? 1.1 : 1)
                            }
                            .frame(minHeight: Metrics.minTouch)
                            .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .authHitHeight(36)
                }
            }
        }
        .padding(.vertical, 12)
        .padding(.horizontal, 14)
        .sensoryFeedback(.selection, trigger: value)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(type.label)
        .accessibilityValue(valueLabel)
        .accessibilityHint("Rate from 1, not for me, to 5, love it")
        .accessibilityAdjustableAction { direction in
            switch direction {
            case .increment: withMotion(Motion.quick) { value = min(5, value + 1) }
            case .decrement: withMotion(Motion.quick) { value = max(0, value - 1) }
            @unknown default: break
            }
        }
    }

    private func pick(_ n: Int) {
        let new = value == n ? 0 : n
        withMotion(Motion.quick) { value = new }
        guard new > 0, !reduceMotion else { return }
        withAnimation(.snappy(duration: 0.12)) {
            popped = new
        } completion: {
            withAnimation(.bouncy(duration: 0.3)) { popped = nil }
        }
    }

    private var valueLabel: String {
        value > 0 ? "\(value) · \(TripType.scaleLabel(value))" : "Not rated"
    }

    /// Selected: sage. Below the selection: `sageTint`. Others: cream.
    private func fill(for n: Int) -> Color {
        if n == value { return Theme.sage }
        if value > 0 && n < value { return Theme.sageTint }
        return Theme.cream
    }
}

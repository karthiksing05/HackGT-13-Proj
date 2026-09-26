import SwiftUI

/// Segmented control: track `segmentBg`, padding 2, radius 10; segments 34 tall, radius 8;
/// selected = white + shadow 0 1 3 rgba(0,0,0,.12) + Semibold; unselected Medium.
struct SQSegmentedControl<Value: Hashable>: View {
    @Binding var selection: Value
    let options: [(value: Value, label: String)]
    var height: CGFloat = 34
    var fontSize: CGFloat = 14
    var accessibilityLabel: String = ""
    @Namespace private var namespace

    var body: some View {
        HStack(spacing: 2) {
            ForEach(options, id: \.value) { option in
                let selected = option.value == selection
                Button {
                    withAnimation(.snappy(duration: 0.22)) { selection = option.value }
                } label: {
                    Text(option.label)
                        .sqFont(fontSize, selected ? .semibold : .medium)
                        .foregroundStyle(Theme.ink)
                        .lineLimit(1)
                        .minimumScaleFactor(0.85)
                        .frame(maxWidth: .infinity)
                        .frame(height: height)
                        .background {
                            if selected {
                                RoundedRectangle(cornerRadius: 8, style: .continuous)
                                    .fill(.white)
                                    .shadow(color: .black.opacity(0.12), radius: 1.5, x: 0, y: 1)
                                    .matchedGeometryEffect(id: "segment", in: namespace)
                            }
                        }
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityAddTraits(selected ? .isSelected : [])
            }
        }
        .padding(2)
        .background(Theme.segmentBg, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
        .sensoryFeedback(.selection, trigger: selection)
        .accessibilityElement(children: .contain)
        .accessibilityLabel(accessibilityLabel)
    }
}

/// iOS-style switch, 52×32: on = sage, off = `line`, white 28pt knob.
struct SQToggle: View {
    @Binding var isOn: Bool
    var label: String

    var body: some View {
        Button {
            withAnimation(.snappy(duration: 0.2)) { isOn.toggle() }
        } label: {
            ZStack(alignment: isOn ? .trailing : .leading) {
                Capsule().fill(isOn ? Theme.sage : Theme.line)
                Circle()
                    .fill(.white)
                    .frame(width: 28, height: 28)
                    .shadow(color: .black.opacity(0.25), radius: 1.5, x: 0, y: 1)
                    .padding(2)
            }
            .frame(width: 52, height: 32)
            .frame(minHeight: Metrics.minTouch)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(label)
        .accessibilityValue(isOn ? "On" : "Off")
        .accessibilityAddTraits(.isToggle)
        .sensoryFeedback(.selection, trigger: isOn)
    }
}

/// A white row with a title, optional subtitle and a trailing `SQToggle`.
struct ToggleRow: View {
    let title: String
    var subtitle: String?
    @Binding var isOn: Bool

    var body: some View {
        HStack(spacing: 10) {
            VStack(alignment: .leading, spacing: 0) {
                Text(title).sqFont(15, .semibold).foregroundStyle(Theme.ink)
                if let subtitle { Text(subtitle).captionStyle() }
            }
            Spacer(minLength: 0)
            SQToggle(isOn: $isOn, label: title)
        }
    }
}

/// Progress dots for Forgot password: active 22×6 pill sage, done 6×6 sage, future 6×6 `lineStrong`.
struct ProgressDots: View {
    let total: Int
    let current: Int

    var body: some View {
        HStack(spacing: 6) {
            ForEach(1...total, id: \.self) { n in
                Capsule()
                    .fill(n <= current ? Theme.sage : Theme.lineStrong)
                    .frame(width: n == current ? 22 : 6, height: 6)
            }
        }
        .animation(.snappy, value: current)
        .accessibilityElement()
        .accessibilityLabel("Step \(current) of \(total)")
    }
}

/// Thin progress bar (Setup: 4pt, track `segmentBg`; Forum spots: 6pt, track `line`).
struct ProgressBar: View {
    let fraction: Double
    var height: CGFloat = 4
    var track: Color = Theme.segmentBg
    var fill: Color = Theme.sage

    var body: some View {
        GeometryReader { proxy in
            ZStack(alignment: .leading) {
                Capsule().fill(track)
                Capsule().fill(fill).frame(width: proxy.size.width * max(0, min(1, fraction)))
            }
        }
        .frame(height: height)
        .animation(.easeInOut(duration: 0.25), value: fraction)
    }
}

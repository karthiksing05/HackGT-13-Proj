import CoreText
import SwiftUI
import UIKit

// Small building blocks shared by the Create steps. Everything here is prefixed `Create…` (or
// private) so it can't collide with other features in the module.

// MARK: - Greedy line breaking

/// Multi-line text that wraps greedily, like the prototype's browser layout. SwiftUI's `Text`
/// avoids a one-word last line by pushing a word down ("Where do you start / and end?"); the
/// prototype keeps "Where do you start and / end?". Backed by a `UILabel` with no line-break
/// strategy; scales with Dynamic Type and sits in a CSS line box (`lineHeight` × size).
struct CreateWrapText: View {
    enum Face {
        case mono(MonoWeight)
        case system(UIFont.Weight)
    }

    let text: String
    var face: Face = .system(.regular)
    var size: CGFloat
    var textStyle: UIFont.TextStyle = .body
    var color: Color = Theme.ink
    var kern: CGFloat = 0
    var lineHeight: CGFloat = 1.35
    var alignment: NSTextAlignment = .natural
    var isHeader = false

    var body: some View {
        CreateWrapLabel(text: text, face: face, size: size, textStyle: textStyle, color: UIColor(color), kern: kern,
                        lineHeight: lineHeight, alignment: alignment, isHeader: isHeader)
            .padding(.vertical, halfLeading)
    }

    /// The CSS half-leading above the first and below the last line.
    @ScaledMetric private var scale: CGFloat = 1

    private var halfLeading: CGFloat {
        let font = CreateWrapLabel.baseFont(face, size: size)
        return max(0, (lineHeight * size - font.lineHeight) / 2) * scale
    }
}

private struct CreateWrapLabel: UIViewRepresentable {
    let text: String
    let face: CreateWrapText.Face
    let size: CGFloat
    let textStyle: UIFont.TextStyle
    let color: UIColor
    let kern: CGFloat
    let lineHeight: CGFloat
    let alignment: NSTextAlignment
    let isHeader: Bool

    static func baseFont(_ face: CreateWrapText.Face, size: CGFloat) -> UIFont {
        switch face {
        case .mono(let weight): UIFont(name: weight.rawValue, size: size) ?? .monospacedSystemFont(ofSize: size, weight: .heavy)
        case .system(let weight): .systemFont(ofSize: size, weight: weight)
        }
    }

    func makeUIView(context: Context) -> UILabel {
        let label = UILabel()
        label.numberOfLines = 0
        label.lineBreakMode = .byWordWrapping
        label.lineBreakStrategy = []
        label.backgroundColor = .clear
        label.setContentHuggingPriority(.required, for: .vertical)
        label.setContentCompressionResistancePriority(.defaultLow, for: .horizontal)
        return label
    }

    func updateUIView(_ label: UILabel, context: Context) {
        let traits = UITraitCollection(preferredContentSizeCategory: UIContentSizeCategory(context.environment.dynamicTypeSize))
        let font = UIFontMetrics(forTextStyle: textStyle).scaledFont(for: Self.baseFont(face, size: size), compatibleWith: traits)
        let paragraph = NSMutableParagraphStyle()
        paragraph.lineBreakMode = .byWordWrapping
        paragraph.lineBreakStrategy = []
        paragraph.alignment = alignment
        paragraph.lineSpacing = max(0, lineHeight * font.pointSize - font.lineHeight)
        var attributes: [NSAttributedString.Key: Any] = [.font: font, .foregroundColor: color, .paragraphStyle: paragraph]
        // CSS letter-spacing = tracking. (An explicit `.kern` of 0 would switch pair kerning off.)
        if kern != 0 { attributes[.tracking] = kern }
        label.attributedText = NSAttributedString(string: text, attributes: attributes)
        label.accessibilityTraits = isHeader ? .header : .staticText
    }

    func sizeThatFits(_ proposal: ProposedViewSize, uiView: UILabel, context: Context) -> CGSize? {
        let scale = max(1, context.environment.displayScale)
        func pixelCeil(_ value: CGFloat) -> CGFloat { ceil(value * scale) / scale }
        guard let width = proposal.width, width.isFinite, width > 0 else {
            let size = uiView.sizeThatFits(CGSize(width: CGFloat.greatestFiniteMagnitude, height: .greatestFiniteMagnitude))
            return CGSize(width: pixelCeil(size.width), height: pixelCeil(size.height))
        }
        let size = uiView.sizeThatFits(CGSize(width: width, height: .greatestFiniteMagnitude))
        return CGSize(width: width, height: pixelCeil(size.height))
    }
}

// MARK: - Text that animates its changes

/// A `CreateWrapText` whose changes animate: digits roll (`.sqNumeric()`) when a time or a count
/// changes, and new words settle in.
///
/// At rest it *is* the `CreateWrapText` label, so it looks exactly like one. When `text` changes,
/// the label steps aside for a SwiftUI copy of the old text (same font, line box and greedy line
/// breaks, computed with Core Text and drawn as explicit lines), which rolls to the new text; once
/// the roll has played the label, already holding the new text, takes over again. Each change
/// restarts the roll from whatever is on screen, and the latest text always wins. The label stays
/// in the layout throughout, so the container eases to the new height with the change.
struct CreateLiveText: View {
    let text: String
    var weight: UIFont.Weight = .regular
    var size: CGFloat
    var textStyle: UIFont.TextStyle = .body
    var color: Color = Theme.ink
    var lineHeight: CGFloat = 1.35
    var alignment: NSTextAlignment = .natural

    /// What the rolling copy shows; it catches up with `text` inside the roll animation.
    @State private var shown: String
    @State private var rolling = false
    @State private var width: CGFloat?
    @Environment(\.dynamicTypeSize) private var dynamicTypeSize
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    /// The CSS half-leading above the first and below the last line scales like `CreateWrapText`'s.
    @ScaledMetric private var scale: CGFloat = 1

    init(text: String, weight: UIFont.Weight = .regular, size: CGFloat, textStyle: UIFont.TextStyle = .body,
         color: Color = Theme.ink, lineHeight: CGFloat = 1.35, alignment: NSTextAlignment = .natural) {
        self.text = text
        self.weight = weight
        self.size = size
        self.textStyle = textStyle
        self.color = color
        self.lineHeight = lineHeight
        self.alignment = alignment
        _shown = State(initialValue: text)
    }

    var body: some View {
        // Already false on the first frame after a change (before the roll starts), so the label
        // never flashes the new text ahead of the roll.
        let settled = text == shown && !rolling
        CreateWrapText(text: text, face: .system(weight), size: size, textStyle: textStyle, color: color,
                       lineHeight: lineHeight, alignment: alignment)
            .opacity(settled ? 1 : 0)
            .animation(nil, value: settled)
            .overlay(alignment: alignment == .center ? .top : .topLeading) {
                rollingCopy
                    .opacity(settled ? 0 : 1)
                    .animation(nil, value: settled)
                    .accessibilityHidden(true)
            }
            .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
            .task(id: text) { await roll(to: text) }
    }

    private var rollingCopy: some View {
        let font = scaledFont
        let lines = width.map { Self.greedyLines(shown, font: font, width: $0) }
        return Text(lines ?? shown)
            .font(Font(font as CTFont))
            .foregroundStyle(color)
            .lineSpacing(max(0, lineHeight * font.pointSize - font.lineHeight))
            .multilineTextAlignment(alignment == .center ? .center : .leading)
            // Explicit lines never re-wrap (a line measured a hair wider than the frame would).
            .fixedSize(horizontal: lines != nil, vertical: true)
            .sqNumeric()
            .padding(.vertical, halfLeading)
    }

    /// A little longer than `Motion.standard`, so the roll has finished when the label takes over.
    private static let rollDuration: Duration = .milliseconds(450)

    /// Rolls the copy to `new`, then hands back to the label. A newer change cancels this roll and
    /// starts its own from what's on screen.
    private func roll(to new: String) async {
        guard new != shown else {
            // First appearance, or back on screen after an interrupted roll.
            rolling = false
            return
        }
        guard !reduceMotion else {
            shown = new
            return
        }
        rolling = true
        withAnimation(Motion.standard) { shown = new }
        try? await Task.sleep(for: Self.rollDuration)
        guard !Task.isCancelled else { return }
        rolling = false
    }

    private var scaledFont: UIFont {
        let traits = UITraitCollection(preferredContentSizeCategory: UIContentSizeCategory(dynamicTypeSize))
        return UIFontMetrics(forTextStyle: textStyle).scaledFont(for: .systemFont(ofSize: size, weight: weight), compatibleWith: traits)
    }

    private var halfLeading: CGFloat {
        max(0, (lineHeight * size - UIFont.systemFont(ofSize: size, weight: weight).lineHeight) / 2) * scale
    }

    /// Breaks `text` greedily at `width`, the way `CreateWrapText`'s label does (Core Text's
    /// suggested breaks, no line-break strategy), and joins the lines with newlines.
    private static func greedyLines(_ text: String, font: UIFont, width: CGFloat) -> String {
        guard width > 0, !text.isEmpty else { return text }
        let string = text as NSString
        let typesetter = CTTypesetterCreateWithAttributedString(NSAttributedString(string: text, attributes: [.font: font]))
        var lines: [String] = []
        var start = 0
        while start < string.length {
            let count = CTTypesetterSuggestLineBreak(typesetter, start, Double(width))
            guard count > 0 else { break }
            var line = string.substring(with: NSRange(location: start, length: count))
            while let last = line.unicodeScalars.last, CharacterSet.whitespacesAndNewlines.contains(last) {
                line.unicodeScalars.removeLast()
            }
            lines.append(line)
            start += count
        }
        return lines.joined(separator: "\n")
    }
}

/// The kit's shimmer highlight for text that's waiting on the server ("Updating transit…"). Unlike
/// `.sqShimmer(active:)`, turning it off only removes the highlight: the text underneath keeps its
/// identity, so it can roll to its new value in the same update instead of being replaced.
private struct CreatePendingShimmer: ViewModifier {
    let active: Bool
    @State private var start = Date()
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func body(content: Content) -> some View {
        content.overlay {
            if active && !reduceMotion {
                TimelineView(.animation(minimumInterval: 1.0 / 30)) { timeline in
                    let cycle = 1.4
                    let t = timeline.date.timeIntervalSince(start).truncatingRemainder(dividingBy: cycle) / cycle
                    GeometryReader { proxy in
                        let width = proxy.size.width
                        LinearGradient(colors: [.white.opacity(0), .white.opacity(0.75), .white.opacity(0)],
                                       startPoint: .leading, endPoint: .trailing)
                            .frame(width: width * 0.45)
                            .offset(x: -width * 0.45 + CGFloat(t) * width * 1.45)
                    }
                }
                .mask(content)
                .allowsHitTesting(false)
                .accessibilityHidden(true)
                .transition(.opacity)
            }
        }
    }
}

extension View {
    /// A soft highlight sweeping across text while it waits on the server (see `CreatePendingShimmer`).
    func createPendingShimmer(_ active: Bool) -> some View {
        modifier(CreatePendingShimmer(active: active))
    }
}

/// Cross-fades a UIKit-backed text (`CreateWrapText`) when `value` changes, since a label can't
/// animate its own text: the old and new copies overlap while the height eases to the new one.
/// (A ZStack places its content at a fixed size; that's fine for labels, which size themselves.)
struct CreateCrossfade<Value: Hashable, Content: View>: View {
    let value: Value
    @ViewBuilder var content: Content

    var body: some View {
        ZStack(alignment: .topLeading) {
            content
                .id(value)
                .transition(.opacity)
        }
        .animation(Motion.standard, value: value)
    }
}

// MARK: - CSS line boxes

/// The prototype sets `line-height: 1.35` on everything; SF's natural line height is ≈1.19×.
/// This adds the missing half-leading above/below a text (and the extra spacing between its
/// lines) so vertical rhythm matches the reference renders. Scales with Dynamic Type.
private struct CreateLineBox: ViewModifier {
    @ScaledMetric private var leading: CGFloat

    init(size: CGFloat, multiplier: CGFloat, relativeTo textStyle: Font.TextStyle) {
        _leading = ScaledMetric(wrappedValue: max(0, (multiplier - 1.193) * size), relativeTo: textStyle)
    }

    func body(content: Content) -> some View {
        content.lineSpacing(leading).padding(.vertical, leading / 2)
    }
}

extension View {
    /// SF text at `size` laid out in a CSS line box of `multiplier × size` (default 1.35).
    func createLine(_ size: CGFloat, _ multiplier: CGFloat = 1.35, relativeTo textStyle: Font.TextStyle = .body) -> some View {
        modifier(CreateLineBox(size: size, multiplier: multiplier, relativeTo: textStyle))
    }
}

/// A Create step title ("Where do you start and end?"): JetBrains Mono 26 ExtraBold, tracking
/// −0.4, in a 1.35 line box, wrapped like the prototype.
struct CreateStepTitle: View {
    let text: String

    init(_ text: String) {
        self.text = text
    }

    var body: some View {
        CreateWrapText(text: text, face: .mono(.extraBold), size: 26, textStyle: .title1, kern: -0.4, isHeader: true)
            .frame(maxWidth: .infinity, alignment: .leading)
    }
}

// MARK: - Section label

/// "HOW FAR WILL YOU GO IN BETWEEN?" — 13 Semibold, tracking 0.4, `text3`, in a 1.35 line box.
struct CreateEyebrow: View {
    let text: String
    /// Extra space above (the prototype's `margin-top`), on top of the stack spacing.
    var topMargin: CGFloat = 0

    var body: some View {
        Eyebrow(text: text)
            .createLine(13, relativeTo: .footnote)
            .padding(.top, topMargin)
    }
}

// MARK: - Glyphs (drawn from the prototype's SVG paths, 24-unit viewBox)

/// The prototype's outline map pin: `M12 21s-6.5-6.2-6.5-11.2a6.5 6.5 0 0 1 13 0C18.5 14.8 12 21 12 21z` + a 2.3 circle.
struct CreatePinGlyph: View {
    var size: CGFloat = 18
    var lineWidth: CGFloat = 2

    var body: some View {
        Canvas { context, canvas in
            let s = canvas.width / 24
            var pin = Path()
            pin.move(to: CGPoint(x: 12 * s, y: 21 * s))
            pin.addCurve(to: CGPoint(x: 5.5 * s, y: 9.8 * s), control1: CGPoint(x: 12 * s, y: 21 * s), control2: CGPoint(x: 5.5 * s, y: 14.8 * s))
            pin.addArc(center: CGPoint(x: 12 * s, y: 9.8 * s), radius: 6.5 * s, startAngle: .degrees(180), endAngle: .degrees(0), clockwise: false)
            pin.addCurve(to: CGPoint(x: 12 * s, y: 21 * s), control1: CGPoint(x: 18.5 * s, y: 14.8 * s), control2: CGPoint(x: 12 * s, y: 21 * s))
            pin.closeSubpath()
            pin.addEllipse(in: CGRect(x: (12 - 2.3) * s, y: (9.8 - 2.3) * s, width: 4.6 * s, height: 4.6 * s))
            context.stroke(pin, with: .foreground, style: StrokeStyle(lineWidth: lineWidth * s, lineCap: .round, lineJoin: .round))
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }
}

/// The prototype's "More options" sliders: two rules with knobs.
struct CreateSlidersGlyph: View {
    var size: CGFloat = 20
    var lineWidth: CGFloat = 1.9

    var body: some View {
        Canvas { context, canvas in
            let s = canvas.width / 24
            var path = Path()
            path.move(to: CGPoint(x: 4 * s, y: 7 * s)); path.addLine(to: CGPoint(x: 14 * s, y: 7 * s))
            path.move(to: CGPoint(x: 18 * s, y: 7 * s)); path.addLine(to: CGPoint(x: 20 * s, y: 7 * s))
            path.move(to: CGPoint(x: 4 * s, y: 17 * s)); path.addLine(to: CGPoint(x: 8 * s, y: 17 * s))
            path.move(to: CGPoint(x: 12 * s, y: 17 * s)); path.addLine(to: CGPoint(x: 20 * s, y: 17 * s))
            path.addEllipse(in: CGRect(x: 14 * s, y: 5 * s, width: 4 * s, height: 4 * s))
            path.addEllipse(in: CGRect(x: 8 * s, y: 15 * s, width: 4 * s, height: 4 * s))
            context.stroke(path, with: .foreground, style: StrokeStyle(lineWidth: lineWidth * s, lineCap: .round))
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }
}

/// The prototype's refresh arrow: `M20 12a8 8 0 1 1-2.3-5.6` + `M20 4v5h-5`.
struct CreateRefreshGlyph: View {
    var size: CGFloat = 20
    var lineWidth: CGFloat = 2.2

    var body: some View {
        Canvas { context, canvas in
            let s = canvas.width / 24
            var path = Path()
            // Arc from (20, 12) around the center (12, 12) to ≈(17.7, 6.4), the long way (large arc, sweep 1).
            path.addArc(center: CGPoint(x: 12 * s, y: 12 * s), radius: 8 * s, startAngle: .degrees(0),
                        endAngle: .degrees(-44.6), clockwise: false)
            path.move(to: CGPoint(x: 20 * s, y: 4 * s))
            path.addLine(to: CGPoint(x: 20 * s, y: 9 * s))
            path.addLine(to: CGPoint(x: 15 * s, y: 9 * s))
            context.stroke(path, with: .foreground, style: StrokeStyle(lineWidth: lineWidth * s, lineCap: .round, lineJoin: .round))
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }
}

/// The prototype's chevrons (`M6 9l6 6 6-6` down, `M6 15l6-6 6 6` up, `M9 5l7 7-7 7` right).
struct CreateChevron: View {
    enum Direction { case up, down, right }
    let direction: Direction
    var size: CGFloat = 16
    var lineWidth: CGFloat = 2.2

    var body: some View {
        Canvas { context, canvas in
            let s = canvas.width / 24
            var path = Path()
            switch direction {
            case .down:
                path.move(to: CGPoint(x: 6 * s, y: 9 * s)); path.addLine(to: CGPoint(x: 12 * s, y: 15 * s)); path.addLine(to: CGPoint(x: 18 * s, y: 9 * s))
            case .up:
                path.move(to: CGPoint(x: 6 * s, y: 15 * s)); path.addLine(to: CGPoint(x: 12 * s, y: 9 * s)); path.addLine(to: CGPoint(x: 18 * s, y: 15 * s))
            case .right:
                path.move(to: CGPoint(x: 9 * s, y: 5 * s)); path.addLine(to: CGPoint(x: 16 * s, y: 12 * s)); path.addLine(to: CGPoint(x: 9 * s, y: 19 * s))
            }
            context.stroke(path, with: .foreground, style: StrokeStyle(lineWidth: lineWidth * s, lineCap: .round, lineJoin: .round))
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }
}

// MARK: - Controls

/// A white field card with a 12pt `text3` label on top (When › Date / Start / Back at end by,
/// Vibe › Or type it). Padding 10 × 14, radius 14.
struct CreateFieldCard<Content: View>: View {
    let label: String
    var spacing: CGFloat = 2
    @ViewBuilder var content: Content

    var body: some View {
        VStack(alignment: .leading, spacing: spacing) {
            Text(label)
                .sqFont(12, relativeTo: .caption)
                .foregroundStyle(Theme.text3)
                .createLine(12, relativeTo: .caption)
                .accessibilityHidden(true)
            content
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.white, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
    }
}

/// Equal-width grid of borderless choices (Budget, Who's coming, Pace): 40 tall, radius 12,
/// sage + ink when selected, white otherwise.
struct CreateChoiceGrid<Value: Hashable>: View {
    let options: [(value: Value, label: String)]
    let selection: Value
    var fontSize: CGFloat = 14
    let select: (Value) -> Void

    var body: some View {
        var style = ChipStyle.gridPlain
        style.fontSize = fontSize
        return HStack(spacing: 8) {
            ForEach(options, id: \.value) { option in
                SQChip(label: option.label, isOn: option.value == selection, style: style) { select(option.value) }
            }
        }
    }
}

/// Three two-line choice cards in a row (Where › "How far…", "Can you provide a ride…"):
/// title 15 Semibold over a 12pt caption, sage when selected. Titles wrap like the prototype
/// ("I'll cover / rides") and every card takes the row's tallest height.
struct CreateChoiceCards<Value: Hashable>: View {
    let options: [(value: Value, title: String, subtitle: String)]
    let selection: Value
    let select: (Value) -> Void

    var body: some View {
        HStack(spacing: 8) {
            ForEach(options, id: \.value) { option in
                let isOn = option.value == selection
                Button { select(option.value) } label: {
                    VStack(spacing: 2) {
                        CreateWrapText(text: option.title, face: .system(.semibold), size: 15, alignment: .center)
                        CreateWrapText(text: option.subtitle, size: 12, textStyle: .caption1, alignment: .center)
                    }
                    .accessibilityHidden(true)
                    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
                    .padding(.vertical, 10)
                    .padding(.horizontal, 6)
                    .background(isOn ? Theme.sage : .white, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                    .contentShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
                }
                .buttonStyle(.sqPressable)
                .sqBounce(when: isOn, scale: 1.04)
                .accessibilityLabel("\(option.title), \(option.subtitle)")
                .accessibilityAddTraits(isOn ? .isSelected : [])
            }
        }
        .fixedSize(horizontal: false, vertical: true)
        .animation(Motion.quick, value: selection)
        .sensoryFeedback(.selection, trigger: selection)
    }
}

/// A small pill button with a ≥44pt touch target that doesn't change its layout size.
struct CreatePillButton: View {
    let title: String
    var height: CGFloat = 30
    var fontSize: CGFloat = 13
    var horizontalPadding: CGFloat = 12
    var fill: Color = Theme.cream
    var foreground: Color = Theme.sageInk
    var accessibilityLabel: String?
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Text(title)
                .sqFont(fontSize, .semibold)
                .foregroundStyle(foreground)
                .lineLimit(1)
                .padding(.horizontal, horizontalPadding)
                .frame(height: height)
                .background(fill, in: Capsule())
                .contentShape(Rectangle().inset(by: -max(0, (Metrics.minTouch - height) / 2)))
        }
        .buttonStyle(.plain)
        .accessibilityLabel(accessibilityLabel ?? title)
    }
}

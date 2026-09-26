import SwiftUI

/// JetBrains Mono weights bundled in Resources/Fonts (registered through `UIAppFonts`).
enum MonoWeight: String {
    case medium = "JetBrainsMono-Medium"
    case bold = "JetBrainsMono-Bold"
    case extraBold = "JetBrainsMono-ExtraBold"
}

extension Font {
    /// JetBrains Mono at `size`, scaling with Dynamic Type relative to `textStyle`.
    static func mono(_ size: CGFloat, _ weight: MonoWeight, relativeTo textStyle: Font.TextStyle = .title) -> Font {
        .custom(weight.rawValue, size: size, relativeTo: textStyle)
    }
}

/// SF Pro at an exact point size that still scales with Dynamic Type.
private struct ScaledSystemFont: ViewModifier {
    @ScaledMetric private var size: CGFloat
    private let weight: Font.Weight
    private let design: Font.Design

    init(size: CGFloat, weight: Font.Weight, design: Font.Design, relativeTo textStyle: Font.TextStyle) {
        _size = ScaledMetric(wrappedValue: size, relativeTo: textStyle)
        self.weight = weight
        self.design = design
    }

    func body(content: Content) -> some View {
        content.font(.system(size: size, weight: weight, design: design))
    }
}

extension View {
    /// Apple system font (SF Pro) at an exact size — use for everything people read.
    func sqFont(_ size: CGFloat, _ weight: Font.Weight = .regular, design: Font.Design = .default, relativeTo textStyle: Font.TextStyle = .body) -> some View {
        modifier(ScaledSystemFont(size: size, weight: weight, design: design, relativeTo: textStyle))
    }

    /// Approximates a CSS `line-height` multiplier (SF's natural line height is ≈1.2×).
    func lineHeight(_ multiplier: CGFloat, fontSize: CGFloat) -> some View {
        lineSpacing(max(0, (multiplier - 1.2) * fontSize))
    }

    // MARK: Header styles (JetBrains Mono) — GUI_PLAN.md §4.2

    /// 32 / ExtraBold, tracking −1. Tab screen titles ("Your sidequestz", "Forum", "Groups").
    func largeTitleStyle() -> some View {
        font(.mono(32, .extraBold, relativeTo: .largeTitle)).tracking(-1).foregroundStyle(Theme.ink)
            .lineLimit(1).minimumScaleFactor(0.6)
    }

    /// 28 / ExtraBold, tracking −0.8. Setup + forgot-password step titles.
    func setupTitleStyle() -> some View {
        font(.mono(28, .extraBold, relativeTo: .title)).tracking(-0.8).foregroundStyle(Theme.ink)
    }

    /// 26 / ExtraBold, tracking −0.4. Create step titles.
    func stepTitleStyle() -> some View {
        font(.mono(26, .extraBold, relativeTo: .title)).tracking(-0.4).foregroundStyle(Theme.ink)
    }

    /// 19 / Bold, tracking −0.4. Section headers ("Active itineraries", "Taste profile").
    func sectionStyle() -> some View {
        font(.mono(19, .bold, relativeTo: .title3)).tracking(-0.4).foregroundStyle(Theme.ink)
    }

    // MARK: Body styles (SF Pro)

    /// 13 / Semibold, UPPERCASE, tracking +0.4, `text3`. Use with already-uppercased copy.
    func eyebrowStyle() -> some View {
        sqFont(13, .semibold, relativeTo: .footnote).tracking(0.4).foregroundStyle(Theme.text3)
    }

    /// 15 / Regular, line spacing ≈1.35×.
    func bodyStyle(_ color: Color = Theme.ink) -> some View {
        sqFont(15).lineHeight(1.35, fontSize: 15).foregroundStyle(color)
    }

    /// 12–13 / Regular, `text3`.
    func captionStyle(_ size: CGFloat = 12, color: Color = Theme.text3) -> some View {
        sqFont(size, relativeTo: .caption).foregroundStyle(color)
    }
}

/// "SideQuest" in ink + "z" in sage, JetBrains Mono ExtraBold, tracking −2. Never recolored.
struct Wordmark: View {
    var size: CGFloat = 40

    var body: some View {
        (Text("SideQuest").foregroundStyle(Theme.ink) + Text("z").foregroundStyle(Theme.sage))
            .font(.mono(size, .extraBold, relativeTo: .largeTitle))
            .tracking(-2)
            .lineLimit(1)
            .minimumScaleFactor(0.6)
            .accessibilityLabel("SideQuestz")
    }
}

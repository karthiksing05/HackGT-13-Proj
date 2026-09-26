import SwiftUI

/// Full-width buttons (GUI_PLAN.md §4.3). Presets:
/// - `.sqPrimary`   sage fill, ink 17 Semibold, 52 tall, radius 14 ("Sign in", "Next")
/// - `.sqDark`      ink fill, white text ("Approve purchase", "Done")
/// - `.sqSecondary` white fill, ink text ("Back" in the Create footer)
/// - `.sqCream`     cream fill, ink text ("Website", "Choose photo")
/// - `.sqOutlined`  white fill, 1.5pt sage border, sageInk text ("Regenerate options…")
/// - `.sqDisabled`  grey fill for not-yet-valid actions ("Reset password", "Pick a star rating")
/// Never white text on sage.
struct SQButtonStyle: ButtonStyle {
    var fill: Color
    var foreground: Color
    var border: Color? = nil
    var borderWidth: CGFloat = 1
    var height: CGFloat = Metrics.buttonHeight
    var radius: CGFloat = Metrics.buttonRadius
    var fontSize: CGFloat = 17
    var weight: Font.Weight = .semibold
    var fullWidth = true

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .sqFont(fontSize, weight)
            .foregroundStyle(foreground)
            .lineLimit(1)
            .minimumScaleFactor(0.7)
            .padding(.horizontal, fullWidth ? 12 : 14)
            .frame(maxWidth: fullWidth ? .infinity : nil, minHeight: height)
            .background(fill, in: RoundedRectangle(cornerRadius: radius, style: .continuous))
            .overlay {
                if let border {
                    RoundedRectangle(cornerRadius: radius, style: .continuous).strokeBorder(border, lineWidth: borderWidth)
                }
            }
            .contentShape(RoundedRectangle(cornerRadius: radius, style: .continuous))
            .opacity(configuration.isPressed ? 0.82 : 1)
            .scaleEffect(configuration.isPressed ? 0.985 : 1)
            .animation(.easeOut(duration: 0.12), value: configuration.isPressed)
    }
}

extension ButtonStyle where Self == SQButtonStyle {
    static var sqPrimary: SQButtonStyle { SQButtonStyle(fill: Theme.sage, foreground: Theme.ink) }
    static var sqDark: SQButtonStyle { SQButtonStyle(fill: Theme.ink, foreground: .white) }
    static var sqSecondary: SQButtonStyle { SQButtonStyle(fill: .white, foreground: Theme.ink) }
    static var sqCream: SQButtonStyle { SQButtonStyle(fill: Theme.cream, foreground: Theme.ink) }
    static var sqOutlined: SQButtonStyle { SQButtonStyle(fill: .white, foreground: Theme.sageInk, border: Theme.sage, borderWidth: 1.5) }
    static func sqDisabled(_ fill: Color = Theme.mutedBorder, foreground: Color = Theme.ink) -> SQButtonStyle {
        SQButtonStyle(fill: fill, foreground: foreground)
    }
    /// Any size/color combination.
    static func sq(fill: Color, foreground: Color, border: Color? = nil, borderWidth: CGFloat = 1, height: CGFloat = Metrics.buttonHeight,
                   radius: CGFloat = Metrics.buttonRadius, fontSize: CGFloat = 17, weight: Font.Weight = .semibold, fullWidth: Bool = true) -> SQButtonStyle {
        SQButtonStyle(fill: fill, foreground: foreground, border: border, borderWidth: borderWidth, height: height, radius: radius,
                      fontSize: fontSize, weight: weight, fullWidth: fullWidth)
    }
}

/// Small pill buttons: height 30–36, radius = height/2, 13–15 Semibold ("+ Plan", "Accept", "Edit photo").
struct PillButtonStyle: ButtonStyle {
    var fill: Color = Theme.sage
    var foreground: Color = Theme.ink
    var border: Color? = nil
    var borderWidth: CGFloat = 1
    var height: CGFloat = 34
    var fontSize: CGFloat = 14
    var horizontalPadding: CGFloat = 14

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .sqFont(fontSize, .semibold)
            .foregroundStyle(foreground)
            .lineLimit(1)
            .padding(.horizontal, horizontalPadding)
            .frame(height: height)
            .background(fill, in: Capsule())
            .overlay {
                if let border { Capsule().strokeBorder(border, lineWidth: borderWidth) }
            }
            .contentShape(Capsule())
            .frame(minHeight: Metrics.minTouch)
            .contentShape(Rectangle())
            .opacity(configuration.isPressed ? 0.8 : 1)
            .scaleEffect(configuration.isPressed ? 0.96 : 1)
            .animation(.easeOut(duration: 0.12), value: configuration.isPressed)
    }
}

extension ButtonStyle where Self == PillButtonStyle {
    /// Sage pill, ink text.
    static var sqPill: PillButtonStyle { PillButtonStyle() }
    /// sageTint pill, sageInk text ("+ Plan", "Redo setup questions").
    static var sqTintPill: PillButtonStyle { PillButtonStyle(fill: Theme.sageTint, foreground: Theme.sageInk, height: 32, fontSize: 13, horizontalPadding: 12) }
    static func sqPill(fill: Color, foreground: Color, border: Color? = nil, height: CGFloat = 34, fontSize: CGFloat = 14, horizontalPadding: CGFloat = 14) -> PillButtonStyle {
        PillButtonStyle(fill: fill, foreground: foreground, border: border, height: height, fontSize: fontSize, horizontalPadding: horizontalPadding)
    }
}

/// Text-only link: sageInk Semibold ("Forgot password?", "Rate", "See all & rate").
struct LinkButtonStyle: ButtonStyle {
    var fontSize: CGFloat = 14
    var color: Color = Theme.sageInk
    var weight: Font.Weight = .semibold

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .sqFont(fontSize, weight)
            .foregroundStyle(color)
            .frame(minHeight: Metrics.minTouch)
            .contentShape(Rectangle())
            .opacity(configuration.isPressed ? 0.6 : 1)
    }
}

extension ButtonStyle where Self == LinkButtonStyle {
    static var sqLink: LinkButtonStyle { LinkButtonStyle() }
    static func sqLink(size: CGFloat, color: Color = Theme.sageInk, weight: Font.Weight = .semibold) -> LinkButtonStyle {
        LinkButtonStyle(fontSize: size, color: color, weight: weight)
    }
}

/// Plain tap style for cards and rows: dims and settles in slightly while pressed.
struct PressableStyle: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .contentShape(Rectangle())
            .opacity(configuration.isPressed ? 0.75 : 1)
            .scaleEffect(configuration.isPressed ? 0.985 : 1)
            .animation(.easeOut(duration: 0.12), value: configuration.isPressed)
    }
}

extension ButtonStyle where Self == PressableStyle {
    static var sqPressable: PressableStyle { PressableStyle() }
}

/// "‹ Back" in sageInk 17 (flow headers).
struct BackButton: View {
    var title = "Back"
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 2) {
                // A 20pt box like the prototype's 20×20 chevron SVG.
                Image(systemName: "chevron.left").font(.system(size: 17, weight: .semibold))
                    .frame(width: 20, height: 20)
                Text(title).sqFont(17)
            }
            .foregroundStyle(Theme.sageInk)
            .frame(minWidth: Metrics.minTouch, minHeight: Metrics.minTouch, alignment: .leading)
            .padding(.leading, 8)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}

/// 32pt round × on cream (Event sheet, Checkout sheet).
struct CloseCircleButton: View {
    var label = "Close"
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            // The prototype's 16pt × (lines 12 units long in a 24-unit box, 2.4 stroke).
            Path { p in
                p.move(to: CGPoint(x: 0, y: 0)); p.addLine(to: CGPoint(x: 8, y: 8))
                p.move(to: CGPoint(x: 8, y: 0)); p.addLine(to: CGPoint(x: 0, y: 8))
            }
            .stroke(style: StrokeStyle(lineWidth: 1.6, lineCap: .round))
            .frame(width: 8, height: 8)
                .foregroundStyle(Theme.text2)
                .frame(width: 32, height: 32)
                .background(Theme.cream, in: Circle())
                .frame(width: Metrics.minTouch, height: Metrics.minTouch)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(label)
    }
}

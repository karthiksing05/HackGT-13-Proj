import SwiftUI

/// Custom tab bar: 84 tall including the home indicator, white at 97%, 1pt top `line`, 5 equal
/// columns, 26pt icons, 10pt Semibold labels. Active = `sageInk` on a `sageTint` rounded shade that
/// slides to the tab you pick; inactive = `text3`.
/// Center: raised + (58pt sage circle, ink plus, 4pt cream ring, shadow 0 6 16 18%, up 24pt) + "Plan".
struct SQTabBar: View {
    let selection: Router.Tab
    let onSelect: (Router.Tab) -> Void
    let onPlan: () -> Void

    @Namespace private var shade
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        HStack(alignment: .top, spacing: 0) {
            item(.home, "Home", .home)
            item(.forum, "Forum", .people)
            plan
            item(.groups, "Groups", .chat)
            item(.account, "Account", .person)
        }
        .animation(reduceMotion ? nil : Motion.quick, value: selection)
        .padding(.top, 6)
        .frame(height: Metrics.tabBarContentHeight, alignment: .top)
        .frame(maxWidth: .infinity)
        .background {
            Color.white.opacity(0.97)
                .overlay(alignment: .top) { RowDivider() }
                .ignoresSafeArea(edges: .bottom)
        }
    }

    private func item(_ tab: Router.Tab, _ title: String, _ glyph: TabGlyph.Kind) -> some View {
        let active = selection == tab
        return Button {
            onSelect(tab)
        } label: {
            // Measured against the prototype: icon box 2pt into the 48pt button, label 2.8pt below.
            VStack(spacing: 2.8) {
                TabGlyph(kind: glyph)
                    .stroke(style: StrokeStyle(lineWidth: 1.8 * 26 / 24, lineCap: .round, lineJoin: .round))
                    .frame(width: 26, height: 26)
                    .sqBounce(when: active, scale: 1.12)
                Text(title).font(.system(size: 10, weight: .semibold))
            }
            // The selected tab sits on a soft sage shade (drawn outside the content, so nothing moves).
            .background {
                if active {
                    RoundedRectangle(cornerRadius: 14, style: .continuous)
                        .fill(Theme.sageTint)
                        .padding(.horizontal, -14)
                        .padding(.vertical, -4)
                        .matchedGeometryEffect(id: "tab.shade", in: shade)
                }
            }
            .padding(.top, 2)
            .foregroundStyle(active ? Theme.sageInk : Theme.text3)
            .frame(maxWidth: .infinity, minHeight: 48, alignment: .top)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(title)
        .accessibilityIdentifier("tab.\(tab)")
        .accessibilityAddTraits(active ? [.isSelected, .isButton] : .isButton)
        .sensoryFeedback(.selection, trigger: active)
    }

    private var plan: some View {
        Button(action: onPlan) {
            VStack(spacing: 2.4) {
                ZStack {
                    Circle().fill(Theme.sage)
                    Circle().strokeBorder(Theme.cream, lineWidth: 4)
                    PlusGlyph(length: 15.2, lineWidth: 2.6).foregroundStyle(Theme.ink)
                }
                .frame(width: Metrics.plusButtonSize, height: Metrics.plusButtonSize)
                .shadow(color: .black.opacity(0.18), radius: 8, x: 0, y: 6)
                Text("Plan").font(.system(size: 10, weight: .semibold)).foregroundStyle(Theme.text2)
            }
            // Up 24 from the prototype's content box, which starts under its 1pt top border.
            .offset(y: -23)
            .frame(maxWidth: .infinity)
            .contentShape(Rectangle())
        }
        .buttonStyle(.sqPressable)
        .accessibilityLabel("New sidequest")
        .accessibilityIdentifier("tab.plan")
    }
}

/// A plus drawn with two round-capped strokes.
struct PlusGlyph: View {
    var length: CGFloat
    var lineWidth: CGFloat

    var body: some View {
        Path { p in
            p.move(to: CGPoint(x: length / 2, y: 0)); p.addLine(to: CGPoint(x: length / 2, y: length))
            p.move(to: CGPoint(x: 0, y: length / 2)); p.addLine(to: CGPoint(x: length, y: length / 2))
        }
        .stroke(style: StrokeStyle(lineWidth: lineWidth, lineCap: .round))
        .frame(width: length, height: length)
    }
}

/// The prototype's tab icons (24-unit viewBox, drawn with a 1.8 stroke): house, two people, chat
/// bubble, person.
struct TabGlyph: Shape {
    enum Kind { case home, people, chat, person }

    let kind: Kind

    func path(in rect: CGRect) -> Path {
        let s = min(rect.width, rect.height) / 24
        func p(_ x: CGFloat, _ y: CGFloat) -> CGPoint { CGPoint(x: rect.minX + x * s, y: rect.minY + y * s) }
        func circle(_ x: CGFloat, _ y: CGFloat, _ r: CGFloat) -> CGRect {
            CGRect(x: rect.minX + (x - r) * s, y: rect.minY + (y - r) * s, width: 2 * r * s, height: 2 * r * s)
        }

        var path = Path()
        switch kind {
        case .home:
            // M3 10.5 12 3l9 7.5 · M5 9.5V21h14V9.5
            path.move(to: p(3, 10.5)); path.addLine(to: p(12, 3)); path.addLine(to: p(21, 10.5))
            path.move(to: p(5, 9.5)); path.addLine(to: p(5, 21)); path.addLine(to: p(19, 21)); path.addLine(to: p(19, 9.5))
        case .people:
            path.addEllipse(in: circle(9, 8, 3.5))
            // M2.5 20c0-3.6 2.9-6 6.5-6s6.5 2.4 6.5 6
            path.move(to: p(2.5, 20))
            path.addCurve(to: p(9, 14), control1: p(2.5, 16.4), control2: p(5.4, 14))
            path.addCurve(to: p(15.5, 20), control1: p(12.6, 14), control2: p(15.5, 16.4))
            // M16 4.6a3.5 3.5 0 0 1 0 6.8: the second head's back, bulging right.
            let dx = (3.5 * 3.5 - 3.4 * 3.4).squareRoot()
            let angle = Angle(radians: atan2(3.4, dx))
            path.move(to: p(16, 4.6))
            path.addArc(center: p(16 - dx, 8), radius: 3.5 * s, startAngle: -angle, endAngle: angle, clockwise: false)
            // M18 14.3c2.1.7 3.5 2.8 3.5 5.7
            path.move(to: p(18, 14.3))
            path.addCurve(to: p(21.5, 20), control1: p(20.1, 15), control2: p(21.5, 17.1))
        case .chat:
            // M4 5h16v11H9l-5 4z
            path.move(to: p(4, 5)); path.addLine(to: p(20, 5)); path.addLine(to: p(20, 16))
            path.addLine(to: p(9, 16)); path.addLine(to: p(4, 20)); path.closeSubpath()
        case .person:
            path.addEllipse(in: circle(12, 8, 4))
            // M4 21c0-4.4 3.6-7 8-7s8 2.6 8 7
            path.move(to: p(4, 21))
            path.addCurve(to: p(12, 14), control1: p(4, 16.6), control2: p(7.6, 14))
            path.addCurve(to: p(20, 21), control1: p(16.4, 14), control2: p(20, 16.6))
        }
        return path
    }
}

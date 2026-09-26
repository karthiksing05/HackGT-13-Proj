import SwiftUI

/// Custom tab bar: 84 tall including the home indicator, white at 97%, 1pt top `line`, 5 equal
/// columns, 26pt icons, 10pt Semibold labels. Active = sage, inactive = `text3`.
/// Center: raised + (58pt sage circle, ink plus, 4pt cream ring, shadow 0 6 16 18%, up 24pt) + "Plan".
struct SQTabBar: View {
    let selection: Router.Tab
    let onSelect: (Router.Tab) -> Void
    let onPlan: () -> Void

    var body: some View {
        HStack(alignment: .top, spacing: 0) {
            item(.home, "Home", "house")
            item(.forum, "Forum", "person.2")
            plan
            item(.groups, "Groups", "bubble.left")
            item(.account, "Account", "person")
        }
        .padding(.top, 6)
        .frame(height: Metrics.tabBarContentHeight, alignment: .top)
        .frame(maxWidth: .infinity)
        .background {
            Color.white.opacity(0.97)
                .overlay(alignment: .top) { RowDivider() }
                .ignoresSafeArea(edges: .bottom)
        }
    }

    private func item(_ tab: Router.Tab, _ title: String, _ symbol: String) -> some View {
        let active = selection == tab
        return Button {
            onSelect(tab)
        } label: {
            VStack(spacing: 2) {
                Image(systemName: symbol)
                    .font(.system(size: 21, weight: .regular))
                    .frame(width: 26, height: 26)
                Text(title).font(.system(size: 10, weight: .semibold))
            }
            .foregroundStyle(active ? Theme.sage : Theme.text3)
            .frame(maxWidth: .infinity, minHeight: 48)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(title)
        .accessibilityAddTraits(active ? [.isSelected, .isButton] : .isButton)
        .sensoryFeedback(.selection, trigger: active)
    }

    private var plan: some View {
        Button(action: onPlan) {
            VStack(spacing: 2) {
                ZStack {
                    Circle().fill(Theme.sage)
                    Circle().strokeBorder(Theme.cream, lineWidth: 4)
                    PlusGlyph(length: 15.2, lineWidth: 2.6).foregroundStyle(Theme.ink)
                }
                .frame(width: Metrics.plusButtonSize, height: Metrics.plusButtonSize)
                .shadow(color: .black.opacity(0.18), radius: 8, x: 0, y: 6)
                Text("Plan").font(.system(size: 10, weight: .semibold)).foregroundStyle(Theme.text2)
            }
            .offset(y: -24)
            .frame(maxWidth: .infinity)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("New sidequest")
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

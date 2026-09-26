import SwiftUI

extension View {
    /// White card, radius 16 (continuous), padding 14, no shadow.
    func sqCard(padding: CGFloat = Metrics.cardPadding, radius: CGFloat = Metrics.cardRadius, fill: Color = .white) -> some View {
        self.padding(padding)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(fill, in: RoundedRectangle(cornerRadius: radius, style: .continuous))
    }

    /// Clips a group of rows into a white rounded card (rows supply their own padding + dividers).
    func sqGroupedCard(radius: CGFloat = Metrics.cardRadius, fill: Color = .white) -> some View {
        background(fill).clipShape(RoundedRectangle(cornerRadius: radius, style: .continuous))
    }
}

/// 1pt separator (`line` or `cream`, per the prototype's per-card choice).
struct RowDivider: View {
    var color: Color = Theme.line

    var body: some View {
        Rectangle().fill(color).frame(height: 1)
    }
}

/// Uppercase section label ("QUICK PICKS", "HOW FAR WILL YOU GO IN BETWEEN?").
struct Eyebrow: View {
    let text: String
    var size: CGFloat = 13
    var weight: Font.Weight = .semibold

    var body: some View {
        Text(text)
            .sqFont(size, weight, relativeTo: .footnote)
            .tracking(0.4)
            .foregroundStyle(Theme.text3)
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityAddTraits(.isHeader)
    }
}

/// Mono section header ("Active sidequests", "Taste profile").
struct SectionHeader: View {
    let title: String

    var body: some View {
        Text(title)
            .sectionStyle()
            .lineLimit(1)
            .minimumScaleFactor(0.8)
            .accessibilityAddTraits(.isHeader)
    }
}

/// Tab screen header: optional eyebrow + large mono title, optional trailing view.
/// Sits 56pt from the top of the screen (as in the prototype), sides 20, bottom 6.
struct TabScreenHeader<Trailing: View>: View {
    var eyebrow: String?
    let title: String
    @ViewBuilder var trailing: Trailing

    var body: some View {
        HStack(alignment: .bottom) {
            VStack(alignment: .leading, spacing: 2) {
                if let eyebrow {
                    // The prototype's CSS line box (13 × 1.35).
                    Text(eyebrow).eyebrowStyle().padding(.vertical, 1)
                }
                Text(title).largeTitleStyle().accessibilityAddTraits(.isHeader)
                    .padding(.bottom, 0.67)
            }
            Spacer(minLength: 8)
            trailing
        }
        .padding(.horizontal, Metrics.side)
        .designTopPadding(56)
        .padding(.bottom, 6)
    }
}

extension TabScreenHeader where Trailing == EmptyView {
    init(eyebrow: String? = nil, title: String) {
        self.init(eyebrow: eyebrow, title: title) { EmptyView() }
    }
}

/// Three-column flow header (Back · center · right), 50pt from the top like the prototype.
struct FlowHeader<Leading: View, Center: View, Trailing: View>: View {
    var sideWidth: CGFloat = 90
    @ViewBuilder var leading: Leading
    @ViewBuilder var center: Center
    @ViewBuilder var trailing: Trailing

    var body: some View {
        HStack(spacing: 0) {
            // Color.clear keeps each side column its full width even when that side is empty.
            ZStack(alignment: .leading) { Color.clear; leading }.frame(width: sideWidth)
            center.frame(maxWidth: .infinity)
            ZStack(alignment: .trailing) { Color.clear; trailing }.frame(width: sideWidth)
        }
        .frame(minHeight: 44)
        .padding(.horizontal, 12)
        .designTopPadding(50, minimum: 0)
    }
}

/// Screen width helper for "full width minus side padding" cards (350 on a 390pt phone).
struct ScreenWidthReader<Content: View>: View {
    @ViewBuilder var content: (CGFloat) -> Content

    var body: some View {
        GeometryReader { proxy in
            content(proxy.size.width)
        }
    }
}

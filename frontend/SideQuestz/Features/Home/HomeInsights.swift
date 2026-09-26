import SwiftUI
import UIKit

/// Home › Past › "What you like": what your best-rated sidequests have in common, computed by the
/// server (`GET /me/insights`). A headline, 2–4 highlight tiles and your top tags. With nothing rated
/// yet it's just the headline, as a gentle nudge. A skeleton while it loads; `PastView` leaves it
/// out if it fails (it's optional).
struct HomeInsightsCard: View {
    let state: Loadable<PastInsights>
    /// Tiles arrive one after another when they replace the skeleton.
    var arrives = false
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        ZStack(alignment: .topLeading) {
            switch state {
            case .loading:
                HomeInsightsSkeleton()
                    .transition(.opacity)
            case .failed:
                EmptyView()
            case .loaded(let insights):
                Group {
                    if insights.basedOn == 0 || (insights.highlights.isEmpty && insights.topTags.isEmpty) {
                        nudge(insights)
                    } else {
                        card(insights)
                    }
                }
                .transition(.opacity)
            }
        }
        .animation(reduceMotion ? Motion.reduced : Motion.standard, value: state.phase)
    }

    // MARK: Card

    private func card(_ insights: PastInsights) -> some View {
        let highlights = Array(insights.highlights.prefix(4))
        let tags = Array(insights.topTags.prefix(5))
        return VStack(alignment: .leading, spacing: 10) {
            Text("WHAT YOU LIKE")
                .sqFont(12, .bold, relativeTo: .caption)
                .tracking(0.4)
                .foregroundStyle(Theme.text3)
                .homeLine(12)
                .accessibilityAddTraits(.isHeader)
            Text(insights.headline)
                .sqFont(17, .bold)
                .foregroundStyle(Theme.ink)
                .homeLine(17, 1.25)
                .fixedSize(horizontal: false, vertical: true)
                .contentTransition(.opacity)
            if !highlights.isEmpty {
                VStack(spacing: 8) {
                    ForEach(Array(stride(from: 0, to: highlights.count, by: 2)), id: \.self) { start in
                        // Two tiles a row, as tall as the taller one (an odd last tile spans the row).
                        HStack(spacing: 8) {
                            tile(highlights[start], index: start)
                            if start + 1 < highlights.count {
                                tile(highlights[start + 1], index: start + 1)
                            }
                        }
                        .fixedSize(horizontal: false, vertical: true)
                    }
                }
            }
            if !tags.isEmpty {
                FlowLayout(spacing: 6) {
                    ForEach(tags, id: \.self) { tag in
                        TagLabel(text: tag, fill: Theme.sageTint, foreground: Theme.sageInk, fontSize: 12,
                                 horizontalPadding: 10, verticalPadding: 5, radius: 12)
                    }
                }
                .accessibilityElement(children: .combine)
                .accessibilityLabel("Top tags: \(tags.joined(separator: ", "))")
                .homeArrival(highlights.count + 1, enabled: arrives)
            }
            Text("Based on \(insights.basedOn) rated \(insights.basedOn == 1 ? "sidequest" : "sidequests")")
                .sqFont(12, relativeTo: .caption)
                .foregroundStyle(Theme.text3)
                .homeLine(12)
                .sqNumeric()
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.white, in: RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
    }

    /// "Favorite time · Evenings · 2 of your 3 favorites" on cream, with its symbol.
    private func tile(_ highlight: PastInsight, index: Int) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            Image(systemName: Self.symbol(highlight.symbol))
                .font(.system(size: 15, weight: .medium))
                .foregroundStyle(Theme.sageInk)
                .frame(height: 20, alignment: .leading)
                .padding(.bottom, 4)
                .accessibilityHidden(true)
            Text(highlight.title)
                .sqFont(12, relativeTo: .caption)
                .foregroundStyle(Theme.text3)
                .homeLine(12)
            Text(highlight.value)
                .sqFont(15, .semibold)
                .foregroundStyle(Theme.ink)
                .homeLine(15, 1.25)
                .lineLimit(2)
                .fixedSize(horizontal: false, vertical: true)
            if let detail = highlight.detail, !detail.isEmpty {
                Text(detail)
                    .sqFont(12, relativeTo: .caption)
                    .foregroundStyle(Theme.text3)
                    .homeLine(12)
                    .lineLimit(2)
            }
        }
        .padding(.vertical, 10)
        .padding(.horizontal, 12)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .background(Theme.cream, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        .accessibilityElement(children: .combine)
        .homeArrival(index + 1, enabled: arrives)
    }

    /// Nothing rated yet (or nothing to say): the headline as a nudge, like the "to rate" card.
    private func nudge(_ insights: PastInsights) -> some View {
        HStack(spacing: 12) {
            StarShape()
                .stroke(Theme.sageInk, style: StrokeStyle(lineWidth: 1.8 * 22 / 24, lineJoin: .round))
                .frame(width: 22, height: 22)
                .frame(width: 40, height: 40)
                .background(Theme.sageTint, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 0) {
                Text("What you like")
                    .sqFont(15, .semibold)
                    .foregroundStyle(Theme.ink)
                    .homeLine(15)
                Text(insights.headline)
                    .sqFont(13)
                    .foregroundStyle(Theme.text3)
                    .homeLine(13)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
        .padding(.vertical, 12)
        .padding(.horizontal, 14)
        .background(.white, in: RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
        .accessibilityElement(children: .combine)
    }

    /// The server's SF Symbol, or a neutral one if this iOS doesn't have it.
    private static func symbol(_ name: String?) -> String {
        guard let name, UIImage(systemName: name) != nil else { return "sparkles" }
        return name
    }
}

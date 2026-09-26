import SwiftUI

/// Loading placeholders for Home, each shaped like the content that replaces it (same card sizes,
/// line boxes and paddings), shimmering. VoiceOver reads each as "Loading".
private extension View {
    func homeSkeletonGroup() -> some View {
        sqShimmer()
            .accessibilityElement(children: .ignore)
            .accessibilityLabel("Loading")
    }
}

// MARK: - Itineraries

/// Two itinerary cards (the second peeking, like the real row), the timeline header with its page
/// dots, and a timeline card with hour rules and block shapes.
struct HomeItinerariesSkeleton: View {
    /// (top, height) of the placeholder blocks, in hours from the first rule.
    private static let blocks: [(CGFloat, CGFloat)] = [(0, 0.8), (1.5, 1.47), (3.33, 1.1), (5, 1.47)]

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            // Laid out like the real row (a horizontal scroll view), so the page width never
            // depends on the placeholder.
            ScrollView(.horizontal) {
                HStack(spacing: 12) {
                    card(title: 150, when: 92, summary: 108)
                    card(title: 118, when: 104, summary: 86)
                }
                .padding(.horizontal, Metrics.side)
                .padding(.top, 4)
                .padding(.bottom, 6)
            }
            .scrollDisabled(true)
            .scrollIndicators(.hidden)
            HStack(alignment: .center, spacing: 8) {
                Text(verbatim: "A").sectionStyle().hidden()
                    .frame(width: 196, alignment: .leading)
                    .overlay { SkeletonBlock(width: 196, height: 15, radius: 5, color: Theme.skeletonOnCream) }
                Spacer(minLength: 0)
                HStack(spacing: 5) {
                    ForEach(0..<2, id: \.self) { _ in
                        Circle().fill(Theme.skeletonOnCream).frame(width: 7, height: 7)
                    }
                }
            }
            .padding(.horizontal, Metrics.side)
            .padding(.top, 20)
            .padding(.bottom, 10)
            timeline
                .padding(.horizontal, Metrics.side)
                .padding(.bottom, 8)
        }
        .homeSkeletonGroup()
    }

    /// A 212pt card: title, "Today · 1–8 PM", the row of 6pt bars, "3 stops · 3 going".
    private func card(title: CGFloat, when: CGFloat, summary: CGFloat) -> some View {
        VStack(alignment: .leading, spacing: 8) {
            HomeSkeletonLine(size: 16, width: title)
            HomeSkeletonLine(size: 13, width: when)
            HStack(spacing: 4) {
                ForEach(0..<6, id: \.self) { _ in
                    Capsule().fill(Theme.skeleton).frame(height: 6)
                }
            }
            HomeSkeletonLine(size: 13, width: summary)
        }
        .padding(14)
        .frame(width: 212, alignment: .topLeading)
        .background(.white, in: RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: Metrics.cardRadius + 1, style: .continuous)
                .strokeBorder(Theme.line, lineWidth: 1)
                .padding(-1)
        }
    }

    private var timeline: some View {
        let hour = HomeTimelineLayout.hourHeight
        let inset = HomeTimelineLayout.topInset
        return ZStack(alignment: .topLeading) {
            ForEach(0..<8, id: \.self) { index in
                HStack(spacing: 8) {
                    SkeletonBlock(width: 30, height: 9, radius: 4)
                        .frame(width: 46, alignment: .trailing)
                    Rectangle().fill(Theme.line).frame(height: 1)
                }
                .frame(height: 14)
                .padding(.trailing, 12)
                .offset(y: inset + CGFloat(index) * hour - 7)
            }
            ForEach(Array(Self.blocks.enumerated()), id: \.offset) { _, block in
                RoundedRectangle(cornerRadius: 10, style: .continuous)
                    .fill(Theme.skeleton)
                    .frame(height: block.1 * hour - 2)
                    .padding(.leading, 62)
                    .padding(.trailing, 12)
                    .offset(y: inset + block.0 * hour)
            }
        }
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .frame(height: inset + 7 * hour + HomeTimelineLayout.bottomInset, alignment: .top)
        .background(.white, in: RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
    }
}

// MARK: - Calendar

/// A row of day chips and one day card: its header, the 6 AM–11 PM rules and a few blocks.
struct HomeCalendarSkeleton: View {
    /// Placeholder blocks as (start, end) in hours.
    private static let blocks: [(Double, Double)] = [(9.5, 10.75), (13, 13.83), (14.5, 16), (16.33, 17.5), (18, 19.5)]

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ScrollView(.horizontal) {
                HStack(spacing: 8) {
                    ForEach(0..<7, id: \.self) { index in
                        chip(index)
                    }
                }
                .padding(.horizontal, Metrics.side)
                .padding(.top, 14)
                .padding(.bottom, 10)
            }
            .scrollDisabled(true)
            .scrollIndicators(.hidden)
            dayCard
                .padding(.horizontal, Metrics.side)
                .padding(.bottom, 8)
        }
        .homeSkeletonGroup()
    }

    private func chip(_ index: Int) -> some View {
        VStack(spacing: 7) {
            SkeletonBlock(width: index == 0 ? 30 : 22, height: 8, radius: 4)
            SkeletonBlock(width: 22, height: 15, radius: 5)
            SkeletonBlock(width: 13, height: 5, radius: 2.5)
        }
        .frame(width: 50, height: 66)
        .background(.white, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
    }

    private var dayCard: some View {
        VStack(spacing: 0) {
            HStack(alignment: .center, spacing: 8) {
                VStack(alignment: .leading, spacing: 0) {
                    HomeSkeletonLine(size: 17, width: 128)
                    HomeSkeletonLine(size: 12, width: 142)
                }
                Spacer(minLength: 0)
                Capsule().fill(Theme.skeleton).frame(width: 60, height: 32)
            }
            .padding(.top, 14)
            .padding(.horizontal, 14)
            .padding(.bottom, 6)
            timeline
        }
        .background(.white, in: RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
    }

    private var timeline: some View {
        let hour = HomeDayPanel.hourHeight
        let inset = HomeDayPanel.topInset
        let first = Double(HomeDayPanel.firstHour)
        return ZStack(alignment: .topLeading) {
            ForEach(Array(stride(from: HomeDayPanel.firstHour, through: 22, by: 2)), id: \.self) { hourMark in
                HStack(spacing: 8) {
                    SkeletonBlock(width: 26, height: 8, radius: 4)
                        .frame(width: 44, alignment: .trailing)
                    Rectangle().fill(Theme.hourLine).frame(height: 1)
                }
                .frame(height: 12)
                .padding(.trailing, 12)
                .offset(y: inset + CGFloat(Double(hourMark) - first) * hour - 6)
            }
            ForEach(Array(Self.blocks.enumerated()), id: \.offset) { _, block in
                RoundedRectangle(cornerRadius: 8, style: .continuous)
                    .fill(Theme.skeleton)
                    .frame(height: max(20, CGFloat(block.1 - block.0) * hour - 2))
                    .padding(.leading, 58)
                    .padding(.trailing, 12)
                    .offset(y: inset + CGFloat(block.0 - first) * hour)
            }
        }
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .frame(height: HomeDayPanel.timelineHeight, alignment: .top)
    }
}

// MARK: - Past

/// Day eyebrows over white cards of rows: a 10pt dot, a title and a caption line, a "Rate" pill.
struct HomePastSkeleton: View {
    private static let sections = [2, 2, 1]
    private static let titles: [CGFloat] = [132, 118, 146, 124, 138]
    private static let captions: [CGFloat] = [150, 96, 128, 110, 140]
    private static let eyebrows: [CGFloat] = [128, 118, 112]

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(Array(Self.sections.enumerated()), id: \.offset) { section, rows in
                HomeSkeletonLine(size: 13, width: Self.eyebrows[section], color: Theme.skeletonOnCream)
                    .padding(.horizontal, Metrics.side)
                    .padding(.top, 16)
                    .padding(.bottom, 8)
                VStack(spacing: 0) {
                    ForEach(0..<rows, id: \.self) { row in
                        self.row(seed: section * 2 + row)
                        RowDivider(color: Theme.cream)
                    }
                }
                .sqGroupedCard()
                .padding(.horizontal, Metrics.side)
            }
        }
        .homeSkeletonGroup()
    }

    private func row(seed: Int) -> some View {
        HStack(spacing: 12) {
            Circle().fill(Theme.skeleton).frame(width: 10, height: 10)
            VStack(alignment: .leading, spacing: 0) {
                HomeSkeletonLine(size: 15, width: Self.titles[seed % Self.titles.count])
                HomeSkeletonLine(size: 12, width: Self.captions[seed % Self.captions.count])
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            Capsule().fill(Theme.skeleton).frame(width: 54, height: 30)
        }
        .padding(.vertical, 12)
        .padding(.horizontal, 14)
    }
}

// MARK: - Event sheet

/// The Event sheet body while its details load: title, time and place rows, a description, the
/// "Getting there" cards, the buttons row and the notes box.
struct HomeEventSkeleton: View {
    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            HomeSkeletonLine(size: 26, multiplier: 1.15, width: 236)
            VStack(alignment: .leading, spacing: 6) {
                iconRow(width: 118)
                iconRow(width: 176)
            }
            VStack(alignment: .leading, spacing: 0) {
                HomeSkeletonLine(size: 15, multiplier: 1.45)
                HomeSkeletonLine(size: 15, multiplier: 1.45, width: 210)
            }
            HomeSkeletonLine(size: 13, width: 104)
            HomeTransitSkeleton.cards
            HStack(spacing: 10) {
                ForEach(0..<2, id: \.self) { _ in
                    RoundedRectangle(cornerRadius: Metrics.buttonRadius, style: .continuous)
                        .fill(Theme.skeleton)
                        .frame(height: 48)
                }
            }
            VStack(alignment: .leading, spacing: 8) {
                HomeSkeletonLine(size: 12, width: 44)
                HomeSkeletonLine(size: 15, width: 220)
            }
            .padding(.vertical, 13)
            .padding(.horizontal, 15)
            .frame(maxWidth: .infinity, minHeight: 107, alignment: .topLeading)
            .background(Theme.field, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
            .overlay { RoundedRectangle(cornerRadius: 14, style: .continuous).strokeBorder(Theme.line, lineWidth: 1) }
        }
        .homeSkeletonGroup()
    }

    private func iconRow(width: CGFloat) -> some View {
        HStack(spacing: 8) {
            Circle().fill(Theme.skeleton).frame(width: 18, height: 18)
            HomeSkeletonLine(size: 15, width: width)
        }
    }
}

/// Three travel-mode cards (Walk / MARTA / Rideshare) while the options load.
struct HomeTransitSkeleton: View {
    var body: some View {
        Self.cards.homeSkeletonGroup()
    }

    private static let labels: [CGFloat] = [34, 48, 70]
    private static let captions: [CGFloat] = [72, 80, 66]

    static var cards: some View {
        HStack(spacing: 8) {
            ForEach(0..<3, id: \.self) { index in
                VStack(spacing: 3) {
                    RoundedRectangle(cornerRadius: 6, style: .continuous)
                        .fill(Theme.skeleton)
                        .frame(width: 22, height: 22)
                    HomeSkeletonLine(size: 14, width: labels[index])
                    HomeSkeletonLine(size: 12, width: captions[index])
                }
                .frame(maxWidth: .infinity)
                .padding(.vertical, 10)
                .padding(.horizontal, 6)
                .background(.white, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
                .overlay {
                    RoundedRectangle(cornerRadius: 15, style: .continuous)
                        .strokeBorder(Theme.line, lineWidth: 1)
                        .padding(-1)
                }
            }
        }
    }
}

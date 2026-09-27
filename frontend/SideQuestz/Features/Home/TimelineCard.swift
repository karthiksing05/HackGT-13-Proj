import SwiftUI

/// Vertical geometry of an itinerary timeline: whole hours from the first block's start hour to
/// the last block's end hour (demo 1–8 PM), 64pt per hour, 14pt top inset, 18pt bottom inset.
struct HomeTimelineLayout {
    static let hourHeight: CGFloat = 64
    static let topInset: CGFloat = 14
    static let bottomInset: CGFloat = 18

    let dayStart: Date
    let firstHour: Int
    let lastHour: Int

    init(itinerary: Itinerary, clock: AppClock) {
        let dayStart = clock.startOfDay(itinerary.date)
        let first = itinerary.items.map(\.start).min() ?? itinerary.start
        let last = itinerary.items.map(\.end).max() ?? itinerary.backBy
        let lo = first.timeIntervalSince(dayStart) / 60
        let hi = last.timeIntervalSince(dayStart) / 60
        self.dayStart = dayStart
        firstHour = Int((lo / 60).rounded(.down))
        lastHour = max(firstHour + 1, Int((hi / 60).rounded(.up)))
    }

    var hours: [Int] { Array(firstHour...lastHour) }
    var height: CGFloat { Self.topInset + CGFloat(lastHour - firstHour) * Self.hourHeight + Self.bottomInset }

    func y(_ date: Date) -> CGFloat {
        let minutes = date.timeIntervalSince(dayStart) / 60 - Double(firstHour * 60)
        return Self.topInset + CGFloat(minutes) * Self.hourHeight / 60
    }

    func y(hour: Int) -> CGFloat { Self.topInset + CGFloat(hour - firstHour) * Self.hourHeight }

    /// 2pt gap between blocks.
    func blockHeight(_ item: ItineraryItem) -> CGFloat { max(12, y(item.end) - y(item.start) - 2) }

    func contains(_ date: Date) -> Bool {
        let top = y(hour: firstHour), bottom = y(hour: lastHour), value = y(date)
        return value >= top && value <= bottom
    }
}

/// One itinerary's timeline (Home › Itineraries carousel): hour rules, blocks and the Now line.
/// On first load (`arrives`) the blocks rise in one after another; later changes animate in place.
struct HomeTimelineCard: View {
    let itinerary: Itinerary
    /// Blocks running late (`transit.delay`): minutes by item id.
    var late: [String: Int] = [:]
    var arrives = false
    let open: (ItineraryItem) -> Void
    @Environment(AppEnvironment.self) private var env
    /// The blocks' Dynamic Type scale (their text is relative to `.body`).
    @ScaledMetric(relativeTo: .body) private var textScale: CGFloat = 1

    var body: some View {
        let layout = HomeTimelineLayout(itinerary: itinerary, clock: env.clock)
        ZStack(alignment: .topLeading) {
            ForEach(layout.hours, id: \.self) { hour in
                hourRow(hour)
                    .offset(y: layout.y(hour: hour) - 7)
            }
            ForEach(Array(itinerary.items.enumerated()), id: \.element.id) { index, item in
                let height = layout.blockHeight(item)
                HomeTimelineBlock(item: item, time: env.format.range(item.start, item.end),
                                  height: height, lateMinutes: late[item.id]) { open(item) }
                    .padding(.leading, 62)
                    .padding(.trailing, 12)
                    .homeArrival(index + 2, enabled: arrives)
                    .sqTransition(.rise)
                    .offset(y: layout.y(item.start))
                    // A slim bar's label capsule overlaps the blocks next to it: keep it on top.
                    .zIndex(HomeTimelineBlock.labelStyle(height: height, textScale: textScale) == .capsule ? 1 : 0)
            }
            if env.clock.isToday(itinerary.date), layout.contains(env.clock.now) {
                nowLine
                    .homeArrival(itinerary.items.count + 2, enabled: arrives)
                    .offset(y: layout.y(env.clock.now) - 1 - 4)
            }
        }
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .frame(height: layout.height, alignment: .top)
        .background(.white, in: RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
        .accessibilityElement(children: .contain)
        .accessibilityLabel(itinerary.title)
    }

    private func hourRow(_ hour: Int) -> some View {
        HStack(spacing: 8) {
            Text(env.format.hourLabel(hour))
                .sqFont(11, relativeTo: .caption2)
                .foregroundStyle(Theme.text3)
                .lineLimit(1)
                .fixedSize()
                .frame(width: 46, alignment: .trailing)
            Rectangle().fill(Theme.line).frame(height: 1)
        }
        .frame(height: 14)
        .padding(.trailing, 12)
        .allowsHitTesting(false)
        .accessibilityHidden(true)
    }

    /// 2pt sage line from x = 56 with a 10pt dot at its left end.
    private var nowLine: some View {
        ZStack(alignment: .leading) {
            Rectangle().fill(Theme.sage).frame(height: 2)
            Circle().fill(Theme.sage).frame(width: 10, height: 10).offset(x: -5)
        }
        .frame(height: 10)
        .padding(.leading, 56)
        .padding(.trailing, 12)
        .allowsHitTesting(false)
        .accessibilityElement()
        .accessibilityLabel("Now, \(env.format.time(env.clock.now))")
    }
}

/// A block on an itinerary timeline. Its label never gets cut through; how it fits depends on the
/// block's height (`labelStyle(height:textScale:)`):
/// - 50pt and taller: the title, then "2:30–4:00 PM · Sidequest"; group blocks from 80pt add faces
///   + counts. A title that would push those out of the block ends in "…".
/// - 22–50pt: one line "Title · 2:00–2:25 PM", 12pt, centered in the block, "…" when too wide.
/// - 16–22pt: the same line at 11pt.
/// - Under 16pt: the block is a slim bar with no text inside; its label sits in a small capsule
///   over the bar's leading end, above the blocks next to it, and opens the block too.
/// A block running late adds a small "8 min late" chip after its time (in the capsule for a bar).
struct HomeTimelineBlock: View {
    let item: ItineraryItem
    let time: String
    let height: CGFloat
    var lateMinutes: Int? = nil
    let action: () -> Void

    /// How a block's label fits its height.
    enum LabelStyle: Equatable {
        /// Title, then "time · kind" (and faces on tall group blocks).
        case twoLines
        /// "Title · time" on one centered line, at this point size.
        case oneLine(CGFloat)
        /// A slim bar with its label in a capsule on top.
        case capsule
    }

    /// The label style for a block this tall. The thresholds are for the default text size; larger
    /// Dynamic Type sizes (`textScale` > 1) raise them in step, so a line never outgrows its block.
    static func labelStyle(height: CGFloat, textScale: CGFloat = 1) -> LabelStyle {
        let scale = max(1, textScale)
        if height >= 50 * scale { return .twoLines }
        if height >= 22 * scale { return .oneLine(12) }
        if height >= 16 * scale { return .oneLine(11) }
        return .capsule
    }

    /// How many lines a two-line block's title may take and still leave room for the time line
    /// (and the faces): at least one, "…" past that. Lines are 17.5pt (title) and 16.2pt (time) at
    /// the default size, the faces row 28pt, inside 5pt padding.
    static func titleLineLimit(height: CGFloat, textScale: CGFloat = 1, showsPeople: Bool = false) -> Int {
        let scale = max(1, textScale)
        let people = showsPeople ? 6 + max(22, 13.2 * scale) : 0
        let room = height - 10 - 16.2 * scale - people
        return max(1, Int((room / (17.5 * scale)).rounded(.down)))
    }

    @ScaledMetric(relativeTo: .body) private var textScale: CGFloat = 1

    private var palette: BlockPalette { item.kind.palette }
    private var style: LabelStyle { Self.labelStyle(height: height, textScale: textScale) }
    private var showsPeople: Bool { item.kind == .group && item.hasPeople && height >= 80 * max(1, textScale) }
    private var border: StrokeStyle { StrokeStyle(lineWidth: 1, dash: palette.dashed ? [3, 3] : []) }

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: 10, style: .continuous)
        let style = self.style
        Button(action: action) {
            label(style)
                .foregroundStyle(palette.text)
                .frame(maxWidth: .infinity, alignment: .leading)
                .frame(height: height, alignment: style == .twoLines ? .top : .center)
                .background(palette.background, in: shape)
                .clipShape(shape)
                .overlay { shape.strokeBorder(palette.border, style: border) }
                .contentShape(shape)
        }
        .buttonStyle(.sqPressable)
        .overlay(alignment: .leading) {
            if style == .capsule {
                // Its own button (same action) so all of it takes taps, also where it overhangs the bar.
                Button(action: action) { capsule }
                    .buttonStyle(.sqPressable)
                    .padding(.horizontal, 8)
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(accessibilityText)
        .accessibilityHint("Opens details")
        .accessibilityAddTraits(.isButton)
    }

    @ViewBuilder private func label(_ style: LabelStyle) -> some View {
        switch style {
        case .twoLines:
            VStack(alignment: .leading, spacing: 0) {
                Text(item.title)
                    .sqFont(14, .semibold)
                    .homeLine(14, 1.25)
                    .lineLimit(Self.titleLineLimit(height: height, textScale: textScale, showsPeople: showsPeople))
                    .fixedSize(horizontal: false, vertical: true)
                HStack(spacing: 6) {
                    Text("\(time) · \(palette.label)")
                        .sqFont(12)
                        .homeLine(12)
                        .opacity(0.85)
                        .lineLimit(1)
                    if let lateMinutes {
                        HomeLateChip(minutes: lateMinutes)
                            .sqTransition(.pop)
                    }
                }
                if showsPeople {
                    HStack(spacing: 0) {
                        HomeAvatarStack(people: Array((item.people + item.interested).prefix(4)), size: 22, fontSize: 9)
                        Text(item.peopleLine)
                            .sqFont(11, .semibold)
                            .lineLimit(1)
                            .padding(.leading, 6)
                    }
                    .padding(.top, 6)
                }
            }
            // 1pt border + the prototype's 4 / 10 padding.
            .padding(.vertical, 5)
            .padding(.horizontal, 11)
        case .oneLine(let size):
            // Centered by the block's frame; the chip keeps 2pt clear of the border.
            HStack(spacing: 6) {
                titleAndTime(size)
                if let lateMinutes {
                    HomeLateChip(minutes: lateMinutes, height: min(16 * max(1, textScale), height - 4))
                        .sqTransition(.pop)
                }
            }
            .padding(.horizontal, 11)
        case .capsule:
            // Just the bar: the label is in `capsule`.
            Color.clear
        }
    }

    /// "Title · 2:00–2:25 PM" on one line, "…" at the end when it doesn't fit.
    private func titleAndTime(_ size: CGFloat) -> some View {
        Text("\(item.title) · \(time)")
            .sqFont(size, .semibold)
            .lineLimit(1)
            .truncationMode(.tail)
    }

    /// The label of a block too short to hold it: 18pt tall in the block's colors, centered on the
    /// bar 8pt in from its leading end, as wide as the text (up to 8pt short of the trailing end).
    /// Its border is solid even on a dashed (transit) bar, so the two outlines don't run together.
    private var capsule: some View {
        HStack(spacing: 6) {
            titleAndTime(11)
            if let lateMinutes {
                HomeLateChip(minutes: lateMinutes, height: 14 * max(1, textScale))
                    .sqTransition(.pop)
            }
        }
        .foregroundStyle(palette.text)
        .padding(.horizontal, 8)
        .frame(height: 18 * max(1, textScale))
        .background(palette.background, in: Capsule())
        .overlay { Capsule().strokeBorder(palette.border, lineWidth: 1) }
        .contentShape(Capsule())
    }

    private var accessibilityText: String {
        var parts = [item.title, time, palette.label]
        if let lateMinutes { parts.append("running \(lateMinutes) minutes late") }
        if item.kind == .group && item.hasPeople { parts.append(item.peopleLine) }
        return parts.joined(separator: ", ")
    }
}

/// "8 min late" on a timeline block the server says is running late (`transit.delay`).
struct HomeLateChip: View {
    let minutes: Int
    var height: CGFloat = 16

    var body: some View {
        Text("\(minutes) min late")
            .sqFont(10, .bold, relativeTo: .caption2)
            .foregroundStyle(Theme.dangerText)
            .lineLimit(1)
            .fixedSize()
            .padding(.horizontal, 6)
            .frame(height: height)
            .background(Theme.dangerBg, in: Capsule())
            .overlay { Capsule().strokeBorder(Theme.errorBorder, lineWidth: 1) }
            .accessibilityHidden(true)
    }
}

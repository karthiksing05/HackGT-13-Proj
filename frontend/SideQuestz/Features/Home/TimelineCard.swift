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
struct HomeTimelineCard: View {
    let itinerary: Itinerary
    let open: (ItineraryItem) -> Void
    @Environment(AppEnvironment.self) private var env

    var body: some View {
        let layout = HomeTimelineLayout(itinerary: itinerary, clock: env.clock)
        ZStack(alignment: .topLeading) {
            ForEach(layout.hours, id: \.self) { hour in
                hourRow(hour)
                    .offset(y: layout.y(hour: hour) - 7)
            }
            ForEach(itinerary.items) { item in
                HomeTimelineBlock(item: item, time: env.format.range(item.start, item.end),
                                  height: layout.blockHeight(item)) { open(item) }
                    .padding(.leading, 62)
                    .padding(.trailing, 12)
                    .offset(y: layout.y(item.start))
            }
            if env.clock.isToday(itinerary.date), layout.contains(env.clock.now) {
                nowLine
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

/// A block on an itinerary timeline. Tall blocks (≥ 50pt) show the title + "2:30–4:00 PM · Sidequest";
/// short ones one truncated line "Title · 2:00–2:25 PM". Group blocks ≥ 80pt add faces + counts.
struct HomeTimelineBlock: View {
    let item: ItineraryItem
    let time: String
    let height: CGFloat
    let action: () -> Void

    private var palette: BlockPalette { item.kind.palette }
    private var isTall: Bool { height >= 50 }
    private var showsPeople: Bool { item.kind == .group && item.hasPeople && height >= 80 }

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: 10, style: .continuous)
        Button(action: action) {
            VStack(alignment: .leading, spacing: 0) {
                if isTall {
                    Text(item.title)
                        .sqFont(14, .semibold)
                        .homeLine(14, 1.25)
                        .fixedSize(horizontal: false, vertical: true)
                    Text("\(time) · \(palette.label)")
                        .sqFont(12)
                        .homeLine(12)
                        .opacity(0.85)
                        .lineLimit(1)
                } else {
                    // The prototype's one-line label shrinks to the block and clips (overflow: hidden).
                    Text("\(item.title) · \(time)")
                        .sqFont(12, .semibold)
                        .lineLimit(1)
                        .truncationMode(.tail)
                        .homeLine(12)
                        .frame(height: max(0, min(12 * 1.35, height - 10)), alignment: .top)
                        .clipped()
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
            .foregroundStyle(palette.text)
            // 1pt border + the prototype's 4 / 10 padding.
            .padding(.vertical, 5)
            .padding(.horizontal, 11)
            .frame(maxWidth: .infinity, alignment: .topLeading)
            .frame(height: height, alignment: .top)
            .background(palette.background, in: shape)
            .clipShape(shape)
            .overlay {
                shape.strokeBorder(palette.border, style: StrokeStyle(lineWidth: 1, dash: palette.dashed ? [3, 3] : []))
            }
            .contentShape(shape)
        }
        .buttonStyle(.sqPressable)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(accessibilityText)
        .accessibilityHint("Opens details")
        .accessibilityAddTraits(.isButton)
    }

    private var accessibilityText: String {
        var parts = [item.title, time, palette.label]
        if item.kind == .group && item.hasPeople { parts.append(item.peopleLine) }
        return parts.joined(separator: ", ")
    }
}

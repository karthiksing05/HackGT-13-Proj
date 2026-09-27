import SwiftUI

/// A ticket's outline: a rounded rectangle with a half circle bitten out of each end of its tear
/// line, the way a paper ticket tears. The tear runs up and down near the trailing edge (a card's
/// stub) or across (the pass in the detail sheet).
struct TicketShape: Shape {
    enum Tear: Equatable {
        /// Top to bottom, this far from the trailing edge.
        case vertical(fromTrailing: CGFloat)
        /// Side to side, this far from the top.
        case horizontal(fromTop: CGFloat)
    }

    var tear: Tear
    var notch: CGFloat = 9
    var cornerRadius: CGFloat = Metrics.cardRadius

    func path(in rect: CGRect) -> Path {
        let outline = RoundedRectangle(cornerRadius: cornerRadius, style: .continuous).path(in: rect)
        var bites = Path()
        for center in Self.notchCenters(tear, in: rect) {
            bites.addEllipse(in: CGRect(x: center.x - notch, y: center.y - notch, width: 2 * notch, height: 2 * notch))
        }
        return outline.subtracting(bites)
    }

    /// Where the two notches sit: the ends of the tear line.
    static func notchCenters(_ tear: Tear, in rect: CGRect) -> [CGPoint] {
        switch tear {
        case .vertical(let inset):
            let x = rect.maxX - inset
            return [CGPoint(x: x, y: rect.minY), CGPoint(x: x, y: rect.maxY)]
        case .horizontal(let top):
            let y = rect.minY + top
            return [CGPoint(x: rect.minX, y: y), CGPoint(x: rect.maxX, y: y)]
        }
    }
}

/// The dotted tear line between a ticket's notches. Stroke it with `TicketPerforation.style`.
struct TicketPerforation: Shape {
    var tear: TicketShape.Tear
    var notch: CGFloat = 9

    static let style = StrokeStyle(lineWidth: 1.6, lineCap: .round, dash: [0.1, 5.5])

    func path(in rect: CGRect) -> Path {
        let ends = TicketShape.notchCenters(tear, in: rect)
        let gap = notch + 4
        var path = Path()
        switch tear {
        case .vertical:
            path.move(to: CGPoint(x: ends[0].x, y: ends[0].y + gap))
            path.addLine(to: CGPoint(x: ends[1].x, y: ends[1].y - gap))
        case .horizontal:
            path.move(to: CGPoint(x: ends[0].x + gap, y: ends[0].y))
            path.addLine(to: CGPoint(x: ends[1].x - gap, y: ends[1].y))
        }
        return path
    }
}

/// How a ticket's time reads: "Today · 7:00–9:00 PM", "Sat, Sep 26 · 1:00–2:45 PM", and in full for
/// VoiceOver.
enum TicketTime {
    static func line(_ ticket: MyTicket, format: TimeFormat) -> String {
        let day = format.clock.isToday(ticket.start) ? "Today" : format.shortDate(ticket.start)
        return "\(day) · \(format.range(ticket.start, ticket.end))"
    }

    /// "Saturday, Sep 26 · 1:00–2:45 PM" (the detail sheet).
    static func full(_ ticket: MyTicket, format: TimeFormat) -> String {
        "\(format.dayTitle(ticket.start)) · \(format.range(ticket.start, ticket.end))"
    }

    static func spoken(_ ticket: MyTicket, format: TimeFormat) -> String {
        "\(format.longDate(ticket.start)), \(format.range(ticket.start, ticket.end))"
    }
}

/// One ticket in Your tickets: when, the stop, where and which plan on the left; on the right a stub
/// in the stop's colors (sage for a sidequest, clay for a group) with how many it admits, torn off
/// along a dotted line. Past tickets get a grey stub. Tap for the ticket itself.
struct TicketCard: View {
    let ticket: MyTicket
    var isPast = false
    let open: () -> Void
    @Environment(AppEnvironment.self) private var env

    static let stubWidth: CGFloat = 84
    private let tear = TicketShape.Tear.vertical(fromTrailing: TicketCard.stubWidth)

    var body: some View {
        Button(action: open) {
            VStack(alignment: .leading, spacing: 3) {
                Text(TicketTime.line(ticket, format: env.format).uppercased())
                    .sqFont(12, .semibold, relativeTo: .caption)
                    .tracking(0.4)
                    .foregroundStyle(isPast ? Theme.text3 : Theme.sageInk)
                    .homeLine(12)
                Text(ticket.title)
                    .sqFont(17, .semibold)
                    .foregroundStyle(Theme.ink)
                    .homeLine(17, 1.25)
                    .lineLimit(2)
                    .fixedSize(horizontal: false, vertical: true)
                if !ticket.place.name.isEmpty {
                    HStack(spacing: 5) {
                        HomeIcon(glyph: .pin, size: 15)
                        Text(ticket.place.name)
                            .sqFont(14)
                            .lineLimit(1)
                    }
                    .foregroundStyle(Theme.text2)
                    .homeLine(14)
                }
                Text(planLine)
                    .sqFont(13)
                    .foregroundStyle(Theme.text3)
                    .lineLimit(1)
                    .homeLine(13)
            }
            .padding(.vertical, 14)
            .padding(.leading, 16)
            .padding(.trailing, Self.stubWidth + 12)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(alignment: .trailing) {
                stub.frame(width: Self.stubWidth)
            }
            .background(.white)
            .clipShape(TicketShape(tear: tear))
            .overlay {
                TicketPerforation(tear: tear)
                    .stroke(Theme.mutedBorder, style: TicketPerforation.style)
            }
            .contentShape(TicketShape(tear: tear))
        }
        .buttonStyle(.sqPressable)
        .accessibilityLabel(accessibilityText)
        .accessibilityHint("Shows the ticket")
    }

    /// "ADMITS" over the count, big in the mono face.
    private var stub: some View {
        let palette = ticket.kind.palette
        return ZStack {
            (isPast ? Theme.busyBg : palette.background)
            VStack(spacing: 0) {
                Text("ADMITS")
                    .sqFont(11, .bold, relativeTo: .caption2)
                    .tracking(0.6)
                Text("\(ticket.ticket.quantity)")
                    .font(.mono(30, .extraBold, relativeTo: .title))
                    .lineLimit(1)
                    .minimumScaleFactor(0.6)
            }
            .foregroundStyle(isPast ? Theme.text2 : palette.text)
            .padding(.horizontal, 6)
        }
    }

    /// The plan's name, or where it went.
    private var planLine: String {
        ticket.itineraryTitle ?? "No longer in your sidequests"
    }

    private var accessibilityText: String {
        [ticket.title, TicketTime.spoken(ticket, format: env.format), ticket.place.name.isEmpty ? nil : ticket.place.name,
         ticket.admitsLabel, planLine].compactMap { $0 }.joined(separator: ", ")
    }
}

/// Two ticket-shaped placeholders while the list loads (the loading policy's skeleton).
struct TicketCardSkeleton: View {
    private let tear = TicketShape.Tear.vertical(fromTrailing: TicketCard.stubWidth)
    private let widths: [(CGFloat, CGFloat, CGFloat)] = [(118, 190, 132), (96, 164, 150)]

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            SkeletonBlock(width: 108, height: 16, color: Theme.skeletonOnCream)
                .padding(.bottom, 2)
            ForEach(0..<widths.count, id: \.self) { index in
                VStack(alignment: .leading, spacing: 9) {
                    SkeletonBlock(width: widths[index].0, height: 10)
                    SkeletonBlock(width: widths[index].1, height: 15)
                    SkeletonBlock(width: widths[index].2, height: 11)
                    SkeletonBlock(width: 90, height: 10)
                }
                .padding(.vertical, 16)
                .padding(.leading, 16)
                .padding(.trailing, TicketCard.stubWidth + 12)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(alignment: .trailing) {
                    Theme.skeleton.frame(width: TicketCard.stubWidth)
                }
                .background(.white)
                .clipShape(TicketShape(tear: tear))
            }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Loading")
    }
}

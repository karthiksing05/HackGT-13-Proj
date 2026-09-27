import MapKit
import SwiftUI

/// Review › tap a stop: what it is, when you'd be there, where it is and what it costs, with "Swap
/// for something similar" and "Remove stop" pinned at the bottom. Presented by `CreateFlowView`
/// (`model.stopDetail`) in the app's sheet.
///
/// What the stop itself says shows at once (title, category, the route's times, the place on a
/// map); the catalog's details (`GET /activities/{id}`) fill in as they arrive, over skeleton lines
/// that give way to the logo after the slow-load threshold. If they can't be had, one short line
/// and "Try again" take their place and the rest stays. Swap closes the pane and then opens the swap
/// sheet for the stop; Remove closes it and then takes the stop out like the menu does (never the
/// last stop: a sidequest keeps at least one).
struct CreateStopDetailSheet: View {
    let model: CreateFlowModel
    let target: CreateStopDetailTarget
    let close: () -> Void

    @Environment(AppEnvironment.self) private var env
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var browserLink: CreateBrowserLink?

    static let style = SQSheetStyle(height: .fitted(max: 740))

    private var stop: PlanStop { target.stop }
    private var detail: ActivityDetail? { model.stopDetailInfo.value }
    private var isLoading: Bool { model.stopDetailInfo.isLoading }

    var body: some View {
        SheetScaffold(spacing: 14, shrinksToFit: true) {
            header
            ScrollView {
                VStack(alignment: .leading, spacing: 16) {
                    timing
                    placeSection
                    about
                    links
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.bottom, 4)
            }
            .scrollBounceBehavior(.basedOnSize)
            .sheetShrinks()
            actions
        }
        .animation(reduceMotion ? Motion.reduced : Motion.standard, value: model.stopDetailInfo.phase)
        .task(id: target.id) { await model.loadStopDetail() }
        .sheet(item: $browserLink) { link in
            SafariView(url: link.url).ignoresSafeArea()
        }
    }

    // MARK: Header

    /// Kind tile · title, "Your pick" and the category · ×.
    private var header: some View {
        HStack(alignment: .top, spacing: 12) {
            kindTile
            VStack(alignment: .leading, spacing: 3) {
                Text(stop.title)
                    .sqFont(20, .bold)
                    .foregroundStyle(Theme.ink)
                    .fixedSize(horizontal: false, vertical: true)
                HStack(spacing: 6) {
                    if isPick {
                        TagLabel(text: "Your pick", fill: Theme.sageTint, foreground: Theme.sageInk,
                                 fontSize: 11, horizontalPadding: 6, verticalPadding: 2, radius: 6)
                            .fixedSize()
                    }
                    if !categoryLabel.isEmpty {
                        Text(categoryLabel)
                            .sqFont(13)
                            .foregroundStyle(Theme.text3)
                            .lineLimit(1)
                            .contentTransition(.opacity)
                    }
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityElement(children: .combine)
            .accessibilityAddTraits(.isHeader)
            .accessibilityIdentifier("stopDetail.title")
            CloseCircleButton(action: close)
                .padding(.top, -6)
                .padding(.trailing, -6)
        }
    }

    /// One of the must-see picks the options were made with.
    private var isPick: Bool { stop.activityId.map(model.planPicks.contains) ?? false }

    /// The catalog's label, else the first part of the stop's line ("Games + views · $").
    private var categoryLabel: String {
        if let label = detail?.categoryLabel, !label.isEmpty { return label }
        return stop.subtitle.components(separatedBy: " · ").first ?? ""
    }

    /// The stop's kind, else the catalog's; a place when neither can say (a stop is somewhere).
    private var kind: PlanStopKind? {
        stop.kind ?? detail?.kind ?? (isLoading ? nil : .place)
    }

    /// 44pt tile: a calendar on sage tint for an event, a pin on cream for a place, shimmering while
    /// the kind isn't known yet.
    private var kindTile: some View {
        Group {
            switch kind {
            case .event?:
                CalendarGlyph(size: 22, lineWidth: 1.8)
                    .foregroundStyle(Theme.sageInk)
            case .place?:
                CreatePinGlyph(size: 22, lineWidth: 1.8)
                    .foregroundStyle(Theme.text2)
            case nil:
                Color.clear
            }
        }
        .frame(width: 44, height: 44)
        .background(kind == .event ? Theme.sageTint : kind == .place ? Theme.cream : Theme.skeleton,
                    in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        .sqShimmer(active: kind == nil)
        .accessibilityElement()
        .accessibilityLabel(kind == .event ? "Event" : "Place")
        .accessibilityHidden(kind == nil)
    }

    // MARK: When

    /// "Arrive 2:30 PM · leave 4:00 PM" from the route on screen, whether the start is fixed, and
    /// the hours that day (or an event's run).
    private var timing: some View {
        let times = model.visitTimes(of: stop, in: target.optionId)
        let schedule = CreateStopTiming.scheduleLine(stop: stop, detail: detail, format: env.format)
        let hours = detail?.hoursLine ?? CreateStopTiming.runLine(detail, format: env.format)
        return VStack(alignment: .leading, spacing: 5) {
            HStack(spacing: 8) {
                HomeIcon(glyph: .clock, size: 18)
                    .foregroundStyle(Theme.text2)
                Text(CreateStopTiming.visitLine(times, format: env.format) ?? "Timing your route…")
                    .sqFont(15, .semibold)
                    .foregroundStyle(times == nil ? Theme.text3 : Theme.ink)
                    .createLine(15)
                    .sqNumeric()
                    .createPendingShimmer(times == nil)
            }
            if model.isLate(stop.id, in: target.optionId) {
                Text("Late for a fixed start")
                    .sqFont(13, .semibold)
                    .foregroundStyle(Theme.danger)
                    .padding(.leading, 26)
            }
            Group {
                if let schedule {
                    Text(schedule)
                        .sqFont(13)
                        .foregroundStyle(Theme.text2)
                }
                if let hours {
                    Text(hours)
                        .sqFont(13)
                        .foregroundStyle(Theme.text2)
                        .transition(.opacity)
                } else if isLoading {
                    SkeletonBlock(width: 128, height: 10, color: Theme.skeletonOnCream)
                        .padding(.vertical, 3)
                        .sqShimmer()
                        .transition(.opacity)
                }
            }
            .padding(.leading, 26)
            .fixedSize(horizontal: false, vertical: true)
        }
        .padding(.vertical, 12)
        .padding(.horizontal, 14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.cream, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .accessibilityElement(children: .combine)
    }

    // MARK: Where

    /// The stop's pin on a small map, the place and its address, and "Directions".
    @ViewBuilder private var placeSection: some View {
        let coordinate = stop.place.coordinate ?? detail?.place.coordinate
        VStack(alignment: .leading, spacing: 10) {
            if let coordinate {
                CreateStopMap(coordinate: coordinate, number: stopNumber, placeName: stop.place.name)
            }
            HStack(alignment: .center, spacing: 12) {
                VStack(alignment: .leading, spacing: 1) {
                    Text(stop.place.name)
                        .sqFont(15, .semibold)
                        .foregroundStyle(Theme.ink)
                        .fixedSize(horizontal: false, vertical: true)
                    if let address = detail?.address {
                        Text(address)
                            .sqFont(13)
                            .foregroundStyle(Theme.text2)
                            .fixedSize(horizontal: false, vertical: true)
                            .transition(.opacity)
                    } else if isLoading {
                        SkeletonBlock(width: 180, height: 10)
                            .padding(.vertical, 4)
                            .sqShimmer()
                            .transition(.opacity)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .accessibilityElement(children: .combine)
                if let coordinate {
                    Button {
                        openDirections(to: coordinate)
                    } label: {
                        HStack(spacing: 5) {
                            Image(systemName: "arrow.triangle.turn.up.right.diamond")
                                .font(.system(size: 14, weight: .semibold))
                            Text("Directions")
                        }
                    }
                    .buttonStyle(.sqPill(fill: Theme.sageTint, foreground: Theme.sageInk, height: 34, fontSize: 14, horizontalPadding: 12))
                    .accessibilityLabel("Directions")
                    .accessibilityHint("Opens Apple Maps with directions to \(stop.place.name)")
                }
            }
        }
    }

    /// The stop's number on the route, as the route card shows it.
    private var stopNumber: Int? {
        model.routes[target.optionId]?.order.firstIndex(of: stop.id).map { $0 + 1 }
    }

    /// Apple Maps, with directions from wherever you are to the stop.
    private func openDirections(to coordinate: Coordinate) {
        let location = CLLocation(latitude: coordinate.lat, longitude: coordinate.lng)
        let item: MKMapItem
        if #available(iOS 26.0, *) {
            item = MKMapItem(location: location, address: nil)
        } else {
            item = MKMapItem(placemark: MKPlacemark(coordinate: location.coordinate))
        }
        item.name = stop.place.name
        item.openInMaps(launchOptions: [MKLaunchOptionsDirectionsModeKey: MKLaunchOptionsDirectionsModeDefault])
    }

    // MARK: About

    /// The summary and description, the price and the rating: skeleton lines while they load, one
    /// short line with "Try again" when they can't be had.
    @ViewBuilder private var about: some View {
        switch model.stopDetailInfo {
        case .loading:
            aboutSkeleton
                .sqSlowLoading(lines: ["Getting the details…"], logoSize: 32)
                .transition(.opacity)
        case .failed(let message):
            HStack(spacing: 8) {
                Text(message)
                    .sqFont(14)
                    .foregroundStyle(Theme.text2)
                    .fixedSize(horizontal: false, vertical: true)
                Spacer(minLength: 0)
                // A stop without an activity has nothing to load again.
                if stop.activityId != nil {
                    Button("Try again") { Task { await model.loadStopDetail() } }
                        .buttonStyle(.sqLink(size: 14))
                }
            }
            .transition(.opacity)
        case .loaded(let detail):
            VStack(alignment: .leading, spacing: 10) {
                if let about = detail.about {
                    CreateClampedText(text: about, lines: 5, identifier: "stopDetail.about")
                }
                facts(detail)
            }
            .transition(.opacity)
        }
    }

    /// Three lines of text and the price chip, shimmering.
    private var aboutSkeleton: some View {
        VStack(alignment: .leading, spacing: 9) {
            SkeletonBlock(height: 12)
            SkeletonBlock(height: 12)
            SkeletonBlock(width: 190, height: 12)
            HStack(spacing: 8) {
                SkeletonBlock(width: 52, height: 26, radius: 8)
                SkeletonBlock(width: 92, height: 12)
            }
            .padding(.top, 5)
        }
        .sqShimmer()
        .accessibilityElement()
        .accessibilityLabel("Loading details")
    }

    /// "$18" · "4.6 ★ (1,204)".
    @ViewBuilder private func facts(_ detail: ActivityDetail) -> some View {
        if !detail.priceLabel.isEmpty || detail.ratingLine != nil {
            HStack(spacing: 10) {
                if !detail.priceLabel.isEmpty {
                    TagLabel(text: detail.priceLabel, fill: Theme.cream, foreground: Theme.ink,
                             fontSize: 13, horizontalPadding: 10, verticalPadding: 5, radius: 8)
                        .accessibilityLabel("Price: \(detail.priceLabel)")
                }
                if let rating = detail.ratingLine {
                    Text(rating)
                        .sqFont(13, .semibold)
                        .foregroundStyle(Theme.text2)
                        .accessibilityLabel(Self.ratingAccessibility(detail))
                }
            }
        }
    }

    /// "Rated 4.6 out of 5 from 1,204 ratings".
    private static func ratingAccessibility(_ detail: ActivityDetail) -> String {
        guard let rating = detail.rating else { return "" }
        let stars = "Rated \(String(format: "%.1f", rating)) out of 5"
        guard let count = detail.ratingCount, count > 0 else { return stars }
        return "\(stars) from \(count.formatted(.number.locale(Locale(identifier: "en_US")))) rating\(count == 1 ? "" : "s")"
    }

    // MARK: Links

    /// "Website" and "Tickets", when the catalog has them, open in the in-app browser.
    @ViewBuilder private var links: some View {
        if let detail, detail.websiteURL != nil || detail.ticketURL != nil {
            HStack(spacing: 10) {
                if let url = detail.websiteURL {
                    Button {
                        browserLink = CreateBrowserLink(url: url)
                    } label: {
                        HStack(spacing: 6) {
                            HomeIcon(glyph: .globe, size: 18)
                            Text("Website")
                        }
                    }
                    .buttonStyle(.sq(fill: Theme.cream, foreground: Theme.ink, height: 46, fontSize: 15))
                    .accessibilityHint("Opens its website in the app")
                }
                if let url = detail.ticketURL {
                    Button {
                        browserLink = CreateBrowserLink(url: url)
                    } label: {
                        HStack(spacing: 6) {
                            Image(systemName: "ticket")
                                .font(.system(size: 15, weight: .medium))
                            Text("Tickets")
                        }
                    }
                    .buttonStyle(.sq(fill: Theme.cream, foreground: Theme.ink, height: 46, fontSize: 15))
                    .accessibilityHint("Opens where tickets are sold, in the app")
                }
            }
            .transition(.opacity)
        }
    }

    // MARK: Actions

    /// Pinned under the details: Swap, and Remove (not on the last stop).
    private var actions: some View {
        let canRemove = model.canRemoveStop(in: target.optionId)
        return VStack(spacing: 8) {
            Button {
                model.swapFromStopDetail()
            } label: {
                HStack(spacing: 8) {
                    Image(systemName: "arrow.triangle.2.circlepath")
                        .font(.system(size: 15, weight: .semibold))
                    Text("Swap for something similar")
                }
            }
            .buttonStyle(.sq(fill: Theme.sage, foreground: Theme.ink, height: 50, fontSize: 16))
            .accessibilityHint("Closes this and shows similar spots that could take its place")
            Button(role: .destructive) {
                model.removeFromStopDetail()
            } label: {
                HStack(spacing: 8) {
                    Image(systemName: "trash")
                        .font(.system(size: 15, weight: .semibold))
                    Text("Remove stop")
                }
            }
            .buttonStyle(.sq(fill: Theme.dangerBg, foreground: Theme.dangerText, height: 50, fontSize: 16))
            .disabled(!canRemove)
            .opacity(canRemove ? 1 : 0.45)
            .accessibilityHint(canRemove ? "Takes it out of this sidequest" : "")
            if !canRemove {
                Text("A sidequest needs at least one stop.")
                    .sqFont(13)
                    .foregroundStyle(Theme.text3)
                    .frame(maxWidth: .infinity)
            }
        }
    }
}

// MARK: - Timing lines

/// The pane's timing lines (pure, so the tests can pin them).
enum CreateStopTiming {
    /// "Arrive 2:30 PM · leave 4:00 PM".
    static func visitLine(_ times: DateInterval?, format: TimeFormat) -> String? {
        guard let times else { return nil }
        return "Arrive \(format.time(times.start)) · leave \(format.time(times.end))"
    }

    /// "Starts at 7:00 PM — fixed time" for a fixed start (the planner's `flexible: false`, or an
    /// event the route waits for), "Drop in any time while it's open" for a place you can visit
    /// whenever ("…while it's on" for a drop-in event); nil while the kind isn't known.
    static func scheduleLine(stop: PlanStop, detail: ActivityDetail?, format: TimeFormat) -> String? {
        let fixed = stop.flexible.map { !$0 } ?? (stop.kind == .event && stop.arriveTime != nil)
        if fixed {
            guard let start = detail?.start ?? stop.arriveTime else { return "Fixed start time" }
            return "Starts at \(format.time(start)) — fixed time"
        }
        switch stop.kind ?? detail?.kind {
        case .event?: return "Drop in any time while it's on"
        case .place?: return "Drop in any time while it's open"
        case nil: return nil
        }
    }

    /// An event's run, "Runs 7:00–9:00 PM" (events have no opening hours); nil otherwise.
    static func runLine(_ detail: ActivityDetail?, format: TimeFormat) -> String? {
        guard let detail, detail.kind == .event, let start = detail.start, let end = detail.end else { return nil }
        return "Runs \(format.range(start, end))"
    }
}

// MARK: - Pieces

/// A link the pane opens in the in-app browser.
private struct CreateBrowserLink: Identifiable {
    let url: URL
    var id: String { url.absoluteString }
}

/// 140pt map around the stop with its numbered pin (the route card's sage marker), in the app's
/// muted style. Not interactive: the pane scrolls over it.
private struct CreateStopMap: View {
    let coordinate: Coordinate
    let number: Int?
    let placeName: String

    var body: some View {
        let center = CLLocationCoordinate2D(latitude: coordinate.lat, longitude: coordinate.lng)
        // A narrow longitudinal span lets the map's height (the short side) set the zoom.
        Map(initialPosition: .region(MKCoordinateRegion(center: center, latitudinalMeters: 650, longitudinalMeters: 325)),
            interactionModes: []) {
            Annotation("", coordinate: center, anchor: .center) {
                marker
            }
            .annotationTitles(.hidden)
        }
        .mapStyle(.standard(emphasis: .muted, pointsOfInterest: .excludingAll))
        .frame(height: 140)
        .background(Theme.mapBackground)
        .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
        .allowsHitTesting(false)
        .id("\(coordinate.lat),\(coordinate.lng)")
        .accessibilityElement()
        .accessibilityLabel("Map of \(placeName)")
    }

    @ViewBuilder private var marker: some View {
        if let number {
            Text("\(number)")
                .sqFont(12, .bold)
                .foregroundStyle(Theme.ink)
                .frame(width: 26, height: 26)
                .background(Theme.sage, in: Circle())
                .overlay(Circle().stroke(.white, lineWidth: 3))
                .shadow(color: .black.opacity(0.18), radius: 4, x: 0, y: 2)
        } else {
            Circle()
                .fill(Theme.sage)
                .frame(width: 16, height: 16)
                .overlay(Circle().stroke(.white, lineWidth: 3))
        }
    }
}

/// Text clamped to `lines`, with "More" / "Less" when it's longer than that.
private struct CreateClampedText: View {
    let text: String
    var lines = 5
    /// For UI tests: the text's accessibility identifier.
    var identifier = ""

    @State private var expanded = false
    @State private var fullHeight: CGFloat = 0
    @State private var shownHeight: CGFloat = 0

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            Text(text)
                .sqFont(15)
                .foregroundStyle(Theme.ink)
                .lineSpacing(3)
                .lineLimit(expanded ? nil : lines)
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
                .accessibilityIdentifier(identifier)
                .onGeometryChange(for: CGFloat.self, of: { $0.size.height }) { shownHeight = $0 }
                .background(alignment: .topLeading) {
                    // The whole text, measured but never drawn.
                    Text(text)
                        .sqFont(15)
                        .lineSpacing(3)
                        .fixedSize(horizontal: false, vertical: true)
                        .hidden()
                        .onGeometryChange(for: CGFloat.self, of: { $0.size.height }) { fullHeight = $0 }
                        .accessibilityHidden(true)
                }
            if expanded || fullHeight > shownHeight + 1 {
                Button(expanded ? "Less" : "More") {
                    withMotion { expanded.toggle() }
                }
                .buttonStyle(.sqLink(size: 14))
                // Laid out shorter than its 44pt touch target, which still takes taps.
                .padding(.vertical, -6)
                .accessibilityHint(expanded ? "Shows less of the description" : "Shows all of the description")
            }
        }
    }
}

import MapKit
import SwiftUI

/// A sidequest's route for its map: where it starts, the stops that have a place on the map (in
/// visit order) and where it ends. Straight lines between them (no directions requests).
nonisolated struct SidequestRoute {
    /// A stop on the map. `number` is its place among all the sidequest's stops (1, 2, 3…), so it
    /// matches the progress strip even when a stop without a location is left off the map.
    nonisolated struct Stop: Identifiable {
        let number: Int
        let item: ItineraryItem
        let coordinate: Coordinate
        var id: String { item.id }
    }

    /// Within this, the end is the start: a round trip, with one marker.
    static let sameSpot: CLLocationDistance = 50

    let start: Coordinate?
    /// nil for a round trip (the start's marker stands for both) or an end without a location.
    let end: Coordinate?
    let isRoundTrip: Bool
    let stops: [Stop]
    /// Start, stops, end (back at the start for a round trip), skipping places without a location.
    let path: [Coordinate]
    /// Where the stops begin in `path` (after the start, when it has a location).
    private let firstStop: Int

    init(itinerary: Itinerary) {
        let start = itinerary.startPlace.coordinate
        let end = itinerary.endPlace.coordinate
        let roundTrip = start.flatMap { a in end.map { b in SidequestProgress.meters(a, b) <= Self.sameSpot } } ?? false
        var stops: [Stop] = []
        for (index, item) in SidequestProgress.stops(of: itinerary).enumerated() {
            guard let coordinate = item.place?.coordinate else { continue }
            stops.append(Stop(number: index + 1, item: item, coordinate: coordinate))
        }
        self.start = start
        self.end = roundTrip ? nil : end
        self.isRoundTrip = roundTrip
        self.stops = stops
        let head = [start].compactMap { $0 }
        path = head + stops.map(\.coordinate) + [roundTrip ? start : end].compactMap { $0 }
        firstStop = head.count
    }

    /// The route split where the sidequest is: solid through the stops behind you and up to the
    /// one you're at or heading to, then `ahead` (dashed). Before it starts it's all ahead; once
    /// the stops are behind you, all travelled.
    func split(at progress: SidequestProgress) -> (travelled: [Coordinate], ahead: [Coordinate]) {
        let through: Int
        switch progress.phase {
        case .later, .startsSoon:
            through = 0
        case .headingBack, .done:
            through = path.count - 1
        case .atStop(let stop, _), .onTheWay(let stop, _):
            // The target itself, or the last stop before it that's on the map.
            let number = progress.number(of: stop) ?? 0
            let onMap = stops.lastIndex { $0.number <= number }
            through = onMap.map { $0 + firstStop } ?? 0
        }
        guard through > 0 else { return ([], path) }
        guard through < path.count - 1 else { return (path, []) }
        return (Array(path[...through]), Array(path[through...]))
    }

    /// The part of the map that shows `coordinates` with room around them (so pins stay clear of
    /// the edges and the corner button), at least `minimumMeters` across.
    static func frame(_ coordinates: [Coordinate], minimumMeters: Double = 800) -> MKMapRect? {
        let points = coordinates.map { MKMapPoint(CLLocationCoordinate2D(latitude: $0.lat, longitude: $0.lng)) }
        guard let minX = points.map(\.x).min(), let maxX = points.map(\.x).max(),
              let minY = points.map(\.y).min(), let maxY = points.map(\.y).max(),
              let minLat = coordinates.map(\.lat).min(), let maxLat = coordinates.map(\.lat).max() else { return nil }
        let minimum = minimumMeters * MKMapPointsPerMeterAtLatitude((minLat + maxLat) / 2)
        let width = max(maxX - minX, minimum), height = max(maxY - minY, minimum)
        // A quarter of the size again on each side.
        let pad = 0.25
        return MKMapRect(x: (minX + maxX) / 2 - width * (0.5 + pad), y: (minY + maxY) / 2 - height * (0.5 + pad),
                         width: width * (1 + 2 * pad), height: height * (1 + 2 * pad))
    }
}

/// A sidequest on a map (Home › Sidequests › Map): its route in visit order, solid where you've
/// been and dashed ahead; its numbered stops (done ones dimmed with a check, the one you're at or
/// heading to larger, with a ring); and you, as a blue dot, when the phone knows where you are.
/// Tapping a stop opens it, like its timeline block. The map itself doesn't pan or zoom, so the
/// page and the carousel scroll over it; the corner button frames you and the next stop, then
/// the whole route again.
struct SidequestMapCard: View {
    let itinerary: Itinerary
    let progress: SidequestProgress
    /// The timeline's height, kept within 320…520 pt.
    let height: CGFloat
    let open: (ItineraryItem) -> Void

    @Environment(AppEnvironment.self) private var env
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var camera: MapCameraPosition
    /// The camera shows you and the next stop (the corner button) instead of the whole route.
    @State private var framesYou = false

    init(itinerary: Itinerary, progress: SidequestProgress, height: CGFloat, open: @escaping (ItineraryItem) -> Void) {
        self.itinerary = itinerary
        self.progress = progress
        self.height = height
        self.open = open
        _camera = State(initialValue: Self.camera(SidequestRoute(itinerary: itinerary).path))
    }

    var body: some View {
        let route = SidequestRoute(itinerary: itinerary)
        let shape = RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous)
        ZStack {
            if route.stops.isEmpty {
                empty
            } else {
                map(route)
            }
        }
        .frame(maxWidth: .infinity)
        .frame(height: route.stops.isEmpty ? 150 : height)
        .background(Theme.mapBackground)
        .clipShape(shape)
        .onChange(of: route.path) { _, path in
            framesYou = false
            moveCamera(to: Self.camera(path))
        }
        .onChange(of: env.locationFeed.coordinate) {
            // Following you: keep you and the stop in view as you move.
            guard framesYou else { return }
            if let focus = youAndTarget(route) {
                moveCamera(to: focus)
            } else {
                framesYou = false
                moveCamera(to: Self.camera(route.path))
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Map of \(itinerary.title)")
        .accessibilityIdentifier("sidequest.map")
    }

    // MARK: Map

    private func map(_ route: SidequestRoute) -> some View {
        let split = route.split(at: progress)
        let here = progress.isFarAway ? nil : env.locationFeed.coordinate
        return Map(position: $camera, interactionModes: []) {
            if split.ahead.count > 1 {
                MapPolyline(coordinates: split.ahead.map(\.location2D))
                    .stroke(Theme.sageInk.opacity(0.55), style: StrokeStyle(lineWidth: 3, lineCap: .round, lineJoin: .round, dash: [0.5, 7]))
            }
            if split.travelled.count > 1 {
                MapPolyline(coordinates: split.travelled.map(\.location2D))
                    .stroke(Theme.sage, style: StrokeStyle(lineWidth: 4, lineCap: .round, lineJoin: .round))
            }
            // Where annotations meet, the one declared first is drawn on top: the stops (they're
            // buttons), then you, then the route's ends.
            ForEach(route.stops) { stop in
                Annotation("", coordinate: stop.coordinate.location2D, anchor: .center) {
                    pin(stop)
                }
                .annotationTitles(.hidden)
            }
            if let here {
                Annotation("", coordinate: here.location2D, anchor: .center) {
                    SidequestYouDot()
                }
                .annotationTitles(.hidden)
            }
            if let start = route.start {
                Annotation("", coordinate: start.location2D, anchor: .center) {
                    RouteMarker(kind: .start, size: 22, onMap: true)
                        .accessibilityLabel(route.isRoundTrip ? "Start and end, \(itinerary.startPlace.name)" : "Start, \(itinerary.startPlace.name)")
                }
                .annotationTitles(.hidden)
            }
            if let end = route.end {
                Annotation("", coordinate: end.location2D, anchor: .center) {
                    RouteMarker(kind: .end, size: 22, onMap: true)
                        .accessibilityLabel("End, \(itinerary.endPlace.name)")
                }
                .annotationTitles(.hidden)
            }
        }
        .mapStyle(.standard(emphasis: .muted, pointsOfInterest: .excludingAll))
        .overlay(alignment: .topTrailing) {
            if youAndTarget(route) != nil {
                locateButton(route)
                    .transition(.opacity)
            }
        }
        .overlay(alignment: .bottom) {
            if env.locationFeed.canAsk {
                askButton
                    .sqTransition(.pop)
            }
        }
        .animation(reduceMotion ? Motion.reduced : Motion.standard, value: here == nil)
        .animation(reduceMotion ? Motion.reduced : Motion.standard, value: env.locationFeed.canAsk)
    }

    private func pin(_ stop: SidequestRoute.Stop) -> some View {
        let isTarget = progress.target?.id == stop.item.id
        let isDone = progress.fills.indices.contains(stop.number - 1) && progress.fills[stop.number - 1] >= 1
        return Button {
            open(stop.item)
        } label: {
            SidequestMapPin(number: stop.number, color: stop.item.kind.palette.dot, isDone: isDone && !isTarget, isTarget: isTarget)
        }
        .buttonStyle(.sqPressable)
        .accessibilityLabel("Stop \(stop.number), \(stop.item.title), \(env.format.time(stop.item.start))")
        .accessibilityValue(isTarget ? (progress.isHere ? "You're here" : pinStatus) : isDone ? "Done" : "")
        .accessibilityHint("Opens details")
    }

    /// "Now" at the stop you're at, "Next" on the way to one.
    private var pinStatus: String {
        if case .atStop = progress.phase { return "Now" }
        return "Next"
    }

    // MARK: Framing

    /// You and the stop you're at or heading to (the end once the stops are behind you); nil when
    /// the phone doesn't know where you are, or you're far from the route.
    private func youAndTarget(_ route: SidequestRoute) -> MapCameraPosition? {
        guard !progress.isFarAway, let here = env.locationFeed.coordinate else { return nil }
        guard let focus = progress.target?.place?.coordinate ?? route.end ?? route.start else { return nil }
        return Self.camera([here, focus], minimumMeters: 500)
    }

    private static func camera(_ coordinates: [Coordinate], minimumMeters: Double = 800) -> MapCameraPosition {
        SidequestRoute.frame(coordinates, minimumMeters: minimumMeters).map { .rect($0) } ?? .region(PlaceSearch.defaultRegion)
    }

    private func moveCamera(to position: MapCameraPosition) {
        withAnimation(reduceMotion ? nil : Motion.gentle) { camera = position }
    }

    /// Top trailing: frames you and the next stop; tapped again, the whole route.
    private func locateButton(_ route: SidequestRoute) -> some View {
        Button {
            if framesYou {
                framesYou = false
                moveCamera(to: Self.camera(route.path))
            } else if let focus = youAndTarget(route) {
                framesYou = true
                moveCamera(to: focus)
            }
        } label: {
            Image(systemName: framesYou ? "location.fill" : "location")
                .font(.system(size: 15, weight: .semibold))
                .foregroundStyle(Theme.sageInk)
                .frame(width: 36, height: 36)
                .background(.white, in: Circle())
                .shadow(color: .black.opacity(0.16), radius: 4, y: 1)
                .frame(width: Metrics.minTouch, height: Metrics.minTouch)
                .contentShape(Circle())
        }
        .buttonStyle(.sqPressable)
        .padding(6)
        .accessibilityLabel(framesYou ? "Show the whole route" : "Show where you are and the next stop")
        .accessibilityIdentifier("sidequest.map.locate")
    }

    /// Location was never asked for: this asks (the only place the feed does).
    private var askButton: some View {
        Button {
            env.locationFeed.requestPermission()
        } label: {
            Label("Show my location", systemImage: "location.fill")
                .labelStyle(SidequestIconFirstLabelStyle())
        }
        .buttonStyle(.sqPill(fill: .white, foreground: Theme.sageInk, height: 34, fontSize: 13, horizontalPadding: 14))
        .shadow(color: .black.opacity(0.14), radius: 6, y: 2)
        .padding(.bottom, 8)
    }

    // MARK: Empty

    /// None of the stops has a place on the map.
    private var empty: some View {
        VStack(spacing: 8) {
            Image(systemName: "map")
                .font(.system(size: 22, weight: .medium))
                .foregroundStyle(Theme.mutedIcon)
            Text("No map for this sidequest yet")
                .sqFont(15, .semibold)
                .foregroundStyle(Theme.ink)
            Text("Its stops don't have locations.")
                .sqFont(13)
                .foregroundStyle(Theme.text3)
        }
        .multilineTextAlignment(.center)
        .padding(20)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(.white)
        .accessibilityElement(children: .combine)
    }
}

/// A numbered stop on the sidequest map, in its kind's color (sage for sidequests, clay for group
/// plans). Stops that are over dim and show a check; the stop you're at or heading to is larger,
/// with a ring that pulses a few times when it becomes the one (not with Reduce Motion).
private struct SidequestMapPin: View {
    let number: Int
    let color: Color
    let isDone: Bool
    let isTarget: Bool

    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var pulses = 0

    var body: some View {
        let size: CGFloat = isTarget ? 32 : 26
        ZStack {
            if isTarget {
                Circle()
                    .stroke(color, lineWidth: 2)
                    .frame(width: size + 10, height: size + 10)
                    .keyframeAnimator(initialValue: SidequestPulse(), trigger: pulses) { ring, pulse in
                        ring.scaleEffect(pulse.scale).opacity(pulse.opacity)
                    } keyframes: { _ in
                        KeyframeTrack(\.scale) {
                            MoveKeyframe(1)
                            LinearKeyframe(1.7, duration: 1.3)
                            MoveKeyframe(1)
                            LinearKeyframe(1.7, duration: 1.3)
                            MoveKeyframe(1)
                            LinearKeyframe(1.7, duration: 1.3)
                        }
                        KeyframeTrack(\.opacity) {
                            MoveKeyframe(0.8)
                            LinearKeyframe(0, duration: 1.3)
                            MoveKeyframe(0.8)
                            LinearKeyframe(0, duration: 1.3)
                            MoveKeyframe(0.8)
                            LinearKeyframe(0, duration: 1.3)
                        }
                    }
                Circle()
                    .stroke(color.opacity(0.45), lineWidth: 2)
                    .frame(width: size + 10, height: size + 10)
            }
            Circle()
                .fill(color)
                .overlay(Circle().strokeBorder(.white, lineWidth: 2.5))
                .frame(width: size, height: size)
                .shadow(color: .black.opacity(0.22), radius: 3, y: 1.5)
            if isDone {
                CheckGlyph(lineWidth: 3)
                    .foregroundStyle(Theme.ink)
                    .frame(width: 14, height: 14)
            } else {
                // Text on sage (or clay) is ink.
                Text("\(number)")
                    .font(.system(size: isTarget ? 15 : 13, weight: .bold))
                    .foregroundStyle(Theme.ink)
            }
        }
        .opacity(isDone ? 0.55 : 1)
        .frame(width: size + 14, height: size + 14)
        .contentShape(Circle())
        .animation(reduceMotion ? Motion.reduced : Motion.quick, value: isTarget)
        .onAppear { if isTarget && !reduceMotion { pulses += 1 } }
        .onChange(of: isTarget) { _, now in if now && !reduceMotion { pulses += 1 } }
    }
}

/// The target pin's pulse ring (at rest: full size, invisible).
private nonisolated struct SidequestPulse {
    var scale: CGFloat = 1
    var opacity: Double = 0
}

/// You on the map, like Apple Maps: a blue dot with a white ring and a soft halo.
private struct SidequestYouDot: View {
    var body: some View {
        ZStack {
            Circle()
                .fill(Color.blue.opacity(0.16))
                .frame(width: 44, height: 44)
            Circle()
                .fill(.white)
                .frame(width: 20, height: 20)
                .shadow(color: .black.opacity(0.25), radius: 2.5, y: 1)
            Circle()
                .fill(Color.blue)
                .frame(width: 14, height: 14)
        }
        .allowsHitTesting(false)
        .accessibilityElement()
        .accessibilityLabel("You are here")
    }
}

/// The icon, then the title, 6pt apart ("Show my location").
private struct SidequestIconFirstLabelStyle: LabelStyle {
    func makeBody(configuration: Configuration) -> some View {
        HStack(spacing: 6) {
            configuration.icon.font(.system(size: 12, weight: .semibold))
            configuration.title
        }
    }
}

private extension Coordinate {
    var location2D: CLLocationCoordinate2D { CLLocationCoordinate2D(latitude: lat, longitude: lng) }
}

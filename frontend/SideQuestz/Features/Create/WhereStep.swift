import CoreLocation
import MapKit
import SwiftUI

/// Create › Where: start (A) and end (B) pins, search + suggestions, map, travel range and ride.
///
/// Loading: until the default places arrive the pin rows and the suggestion pills shimmer; later
/// searches keep the pills and show three dots in the search field. Picks and map taps drop the
/// pin with a spring; the place names cross-fade.
///
/// When the phone can't say where it is, nothing is assumed: there's no "Current location" pill,
/// a line says so above the search field, and the cursor goes to the search.
struct CreateWhereStep: View {
    @Bindable var model: CreateFlowModel
    var focus: FocusState<CreateField?>.Binding

    private static let locationHint = "Couldn't get your location. Search for your start."

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            CreateStepTitle("Where do you start and end?")
            pinCard
            if model.needsStartSearch {
                CreateWrapText(text: Self.locationHint, size: 13, color: Theme.text2)
                    .sqTransition(.rise)
            }
            SearchField(text: $model.search,
                        placeholder: model.editingPin == .end ? "Search for your end location" : "Search for your start location",
                        accessibilityLabel: "Search a place")
                .focused(focus, equals: .search)
                .submitLabel(.search)
                .onSubmit(pickFirstSuggestion)
                .overlay(alignment: .trailing) {
                    if model.searchingPlaces && model.suggestionsLoaded {
                        LoadingDots(color: Theme.text3, dotSize: 4.5)
                            .padding(.trailing, 14)
                            .allowsHitTesting(false)
                            .sqTransition(.pop)
                    }
                }
                .animation(Motion.quick, value: model.searchingPlaces)
            // No row at all when there's nothing to suggest, so the spacing stays even.
            if !model.suggestionsLoaded || !model.suggestions.isEmpty {
                suggestionPills
            }
            CreateMapCard(model: model)
            CreateEyebrow(text: "HOW FAR WILL YOU GO IN BETWEEN?", topMargin: 6)
            CreateChoiceCards(options: TravelRange.allCases.map { ($0, $0.label, $0.sublabel) }, selection: model.range) { range in
                withMotion(Motion.quick) { model.range = range }
            }
            CreateEyebrow(text: "CAN YOU PROVIDE A RIDE THIS TIME?", topMargin: 6)
            CreateChoiceCards(options: RideChoice.allCases.map { ($0, $0.label, $0.sublabel) }, selection: model.ride) { ride in
                withMotion { model.ride = ride }
            }
            if model.ride == .drive {
                CreateSeatPicker(seats: $model.openSeats)
                    .sqTransition(.rise)
            }
        }
        // New results pop in and everything below eases to the pills' new height.
        .animation(Motion.standard, value: model.suggestions)
        .animation(Motion.standard, value: model.suggestionsLoaded)
        .animation(Motion.standard, value: model.needsStartSearch)
        .onChange(of: model.needsStartSearch, initial: true) { _, needsStart in
            guard needsStart else { return }
            focus.wrappedValue = .search
            AccessibilityNotification.Announcement(Self.locationHint).post()
        }
        // The pills reload when the location answers ("Current location" joins them).
        .task(id: SuggestionKey(search: model.search, pin: model.editingPin, located: model.locationResolved)) {
            if !model.search.isEmpty {
                try? await Task.sleep(nanoseconds: 250_000_000)
                guard !Task.isCancelled else { return }
            }
            await model.loadSuggestions()
        }
    }

    private struct SuggestionKey: Hashable {
        var search: String
        var pin: CreatePin
        var located: Bool
    }

    // MARK: Pins

    private var pinCard: some View {
        VStack(spacing: 0) {
            pinRow(.start)
            RowDivider()
            pinRow(.end)
            RowDivider()
            HStack(spacing: 10) {
                Text("End where I start")
                    .sqFont(14)
                    .foregroundStyle(Theme.ink)
                    .createLine(14)
                Spacer(minLength: 0)
                SQToggle(isOn: $model.endSameAsStart, label: "End where I start")
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 4)
        }
        .background(.white)
        .clipShape(RoundedRectangle(cornerRadius: 16, style: .continuous))
    }

    private func pinRow(_ pin: CreatePin) -> some View {
        let isStart = pin == .start
        let active = model.editingPin == pin
        let place = isStart ? model.start : model.end
        let placeText: String? = if !isStart && model.endSameAsStart { "Same as start" } else { place?.name }
        let loading = placeText == nil && !(isStart ? model.startDefaultLoaded : model.placesLoaded)
        let label = isStart ? "Start" : "End"
        return Button {
            focus.wrappedValue = nil
            withMotion(Motion.quick) { model.selectPin(pin) }
        } label: {
            HStack(spacing: 12) {
                RouteMarker(kind: isStart ? .start : .end, size: 26)
                VStack(alignment: .leading, spacing: 0) {
                    CreateWrapText(text: label, size: 12, textStyle: .caption1, color: Theme.text3)
                    Text(placeText ?? (loading ? " " : "Not set"))
                        .sqFont(15, .semibold)
                        .foregroundStyle(placeText == nil ? Theme.text3 : Theme.ink)
                        .createLine(15)
                        .contentTransition(.opacity)
                        .overlay(alignment: .leading) {
                            // The default places are on their way: a bar where the name goes.
                            if loading {
                                SkeletonBlock(width: isStart ? 210 : 170, height: 13, color: Theme.skeletonOnCream)
                                    .sqShimmer()
                                    .transition(.opacity)
                            }
                        }
                }
                .multilineTextAlignment(.leading)
                .frame(maxWidth: .infinity, alignment: .leading)
                if active {
                    Text("Editing")
                        .sqFont(12, .semibold)
                        .foregroundStyle(Theme.sageInk)
                        .transition(.opacity)
                }
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 12)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(active ? Theme.sageTint : .clear)
            .contentShape(Rectangle())
            .animation(Motion.standard, value: placeText)
            .animation(Motion.standard, value: loading)
        }
        .buttonStyle(.plain)
        .accessibilityLabel("\(isStart ? "Start" : "End"): \(placeText ?? (loading ? "Loading" : "not set"))")
        .accessibilityValue(active ? "Editing" : "")
        .accessibilityAddTraits(active ? .isSelected : [])
    }

    // MARK: Suggestions

    /// Pills rise in and fade out as results change; until the first results arrive, pill-shaped
    /// placeholders shimmer in their place.
    private var suggestionPills: some View {
        VStack(alignment: .leading, spacing: 0) {
            if model.suggestionsLoaded {
                FlowLayout(spacing: 8) {
                    ForEach(model.suggestions) { suggestion in
                        // Rise, not pop: a scale transition leaves text a pixel off once it has run.
                        suggestionPill(suggestion)
                            .sqTransition(.rise)
                    }
                }
                .transition(.opacity)
            } else {
                CreateSuggestionSkeleton()
                    .transition(.opacity)
            }
        }
    }

    private func suggestionPill(_ suggestion: CreateSuggestion) -> some View {
        Button {
            focus.wrappedValue = nil
            Task { await model.pick(suggestion) }
        } label: {
            Text(suggestion.label)
                .sqFont(13, .semibold)
                .foregroundStyle(Theme.ink)
                .lineLimit(1)
                .padding(.horizontal, 12)
                .frame(height: 32)
                .background(.white, in: Capsule())
                .overlay(Capsule().strokeBorder(Theme.lineStrong, lineWidth: 1))
                .contentShape(Rectangle().inset(by: -6))
        }
        .buttonStyle(.plain)
        .accessibilityHint(model.editingPin == .end ? "Sets your end" : "Sets your start")
    }

    private func pickFirstSuggestion() {
        guard let first = model.suggestions.first else { return }
        focus.wrappedValue = nil
        Task { await model.pick(first) }
    }
}

/// Four pill placeholders shaped like the usual first results (Current location, Home, …).
private struct CreateSuggestionSkeleton: View {
    private static let widths: [CGFloat] = [126, 168, 140, 144]

    var body: some View {
        FlowLayout(spacing: 8) {
            ForEach(Array(Self.widths.enumerated()), id: \.offset) { _, width in
                SkeletonBlock(width: width, height: 32, radius: 16, color: Theme.skeletonOnCream)
            }
        }
        .sqShimmer()
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Loading")
    }
}

// MARK: - Map

/// 220pt MapKit map (full content width, radius 16). Press and hold drops the pin being edited (like
/// Apple Maps); pins are the logo's markers (start = diamond, end = ring). No line between them.
///
/// The gestures run *alongside* MapKit's own pan/zoom recognizers (an exclusive gesture loses real
/// touches to MapKit): a zero-distance drag tracks where the finger is, and a long press drops the
/// pin there once it holds still for 0.45 s. Moving more than 12pt cancels it, so panning never
/// drops a pin. A pin that lands somewhere new drops in with a spring and a medium haptic.
private struct CreateMapCard: View {
    let model: CreateFlowModel

    @State private var camera: MapCameraPosition = .region(PlaceSearch.defaultRegion)
    @State private var visibleRegion: MKCoordinateRegion?
    /// Pin positions that have already landed, so a pin only drops when it lands somewhere new
    /// (not every time the map is rebuilt, e.g. coming back to this step).
    @State private var landedPins: Set<String>
    /// Bumped by every press-and-hold that drops a pin (medium haptic).
    @State private var drops = 0
    /// Where the finger is while it's down, for the press-and-hold drop.
    @State private var touchLocation: CGPoint?

    init(model: CreateFlowModel) {
        self.model = model
        let shown = Self.pinCoordinates(model)
        _landedPins = State(initialValue: Set(shown.map(Self.key)))
        // Pins that are already set (coming back to this step, or an end from Home's search) are
        // in view from the start.
        let region = Self.region(showing: shown, from: PlaceSearch.defaultRegion) ?? PlaceSearch.defaultRegion
        _camera = State(initialValue: .region(region))
        _visibleRegion = State(initialValue: region)
    }

    var body: some View {
        MapReader { proxy in
            Map(position: $camera, interactionModes: [.pan, .zoom]) {
                if !model.endSameAsStart, let end = model.end?.coordinate {
                    Annotation("End", coordinate: end.clLocation, anchor: .center) {
                        pin(.end, at: end)
                    }
                    .annotationTitles(.hidden)
                }
                if let start = model.start?.coordinate {
                    Annotation("Start", coordinate: start.clLocation, anchor: .center) {
                        pin(.start, at: start)
                    }
                    .annotationTitles(.hidden)
                }
            }
            .mapStyle(.standard(elevation: .flat, emphasis: .muted, pointsOfInterest: .excludingAll, showsTraffic: false))
            .mapControls {}
            .safeAreaPadding(.bottom, 34)
            .simultaneousGesture(
                DragGesture(minimumDistance: 0, coordinateSpace: .local)
                    .onChanged { touchLocation = $0.location }
                    .onEnded { _ in touchLocation = nil }
            )
            .simultaneousGesture(
                LongPressGesture(minimumDuration: 0.45, maximumDistance: 12).onEnded { _ in
                    guard let location = touchLocation, let coordinate = proxy.convert(location, from: .local) else { return }
                    drops += 1
                    model.dropPin(at: Coordinate(lat: coordinate.latitude, lng: coordinate.longitude))
                }
            )
            .onMapCameraChange(frequency: .onEnd) { context in
                visibleRegion = context.region
            }
        }
        .frame(height: 220)
        .frame(maxWidth: .infinity)
        .background(Theme.mapBackground)
        .clipShape(RoundedRectangle(cornerRadius: 16, style: .continuous))
        .overlay(alignment: .bottomLeading) {
            Text(model.editingPin == .end ? "Press and hold to drop the End pin" : "Press and hold to drop the Start pin")
                .sqFont(12, .semibold)
                .foregroundStyle(Theme.ink)
                .createLine(12)
                .contentTransition(.opacity)
                .padding(.horizontal, 10)
                .padding(.vertical, 5)
                .background(.white, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                .padding(10)
                .allowsHitTesting(false)
                .accessibilityHidden(true)
                .animation(Motion.standard, value: model.editingPin)
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel(model.editingPin == .end ? "Map: press and hold to drop the end pin" : "Map: press and hold to drop the start pin")
        .accessibilityAction(named: model.editingPin == .end ? "Drop the end pin at the map's center" : "Drop the start pin at the map's center") {
            let center = (visibleRegion ?? PlaceSearch.defaultRegion).center
            drops += 1
            model.dropPin(at: Coordinate(lat: center.latitude, lng: center.longitude))
        }
        .sensoryFeedback(.impact(weight: .medium), trigger: drops)
        .onChange(of: model.start) { revealPins() }
        .onChange(of: model.end) { revealPins() }
    }

    /// A marker keyed by where it stands: moving it builds a new one, which drops in.
    private func pin(_ kind: RouteMarker.Kind, at coordinate: Coordinate) -> some View {
        let key = Self.key(coordinate)
        return CreateMapPin(kind: kind, drops: !landedPins.contains(key)) {
            landedPins.insert(key)
        }
        .id(key)
    }

    nonisolated private static func key(_ coordinate: Coordinate) -> String {
        "\(coordinate.lat),\(coordinate.lng)"
    }

    /// When a searched place lands outside the visible map, move the camera to show both pins.
    private func revealPins() {
        guard let fitted = Self.region(showing: Self.pinCoordinates(model), from: visibleRegion ?? PlaceSearch.defaultRegion) else { return }
        withMotion(Motion.gentle) { camera = .region(fitted) }
    }

    private static func pinCoordinates(_ model: CreateFlowModel) -> [Coordinate] {
        [model.start?.coordinate, model.endSameAsStart ? nil : model.end?.coordinate].compactMap { $0 }
    }

    /// A region that shows every coordinate with some margin, or nil when `region` already does
    /// (or there's nothing to show).
    private static func region(showing coordinates: [Coordinate], from region: MKCoordinateRegion) -> MKCoordinateRegion? {
        guard !coordinates.isEmpty else { return nil }
        let inset = 0.12
        let allVisible = coordinates.allSatisfy { c in
            abs(c.lat - region.center.latitude) <= region.span.latitudeDelta * (0.5 - inset)
                && abs(c.lng - region.center.longitude) <= region.span.longitudeDelta * (0.5 - inset)
        }
        guard !allVisible else { return nil }
        let lats = coordinates.map(\.lat), lngs = coordinates.map(\.lng)
        guard let minLat = lats.min(), let maxLat = lats.max(), let minLng = lngs.min(), let maxLng = lngs.max() else { return nil }
        return MKCoordinateRegion(
            center: CLLocationCoordinate2D(latitude: (minLat + maxLat) / 2, longitude: (minLng + maxLng) / 2),
            span: MKCoordinateSpan(latitudeDelta: max(0.02, (maxLat - minLat) * 1.8), longitudeDelta: max(0.03, (maxLng - minLng) * 1.8))
        )
    }
}

/// A 28pt route marker on the map (white halo + shadow). When it `drops`, it falls in from above
/// with a spring the moment it appears; otherwise it's simply there.
private struct CreateMapPin: View {
    let kind: RouteMarker.Kind
    let onLand: () -> Void

    @State private var landed: Bool
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    init(kind: RouteMarker.Kind, drops: Bool, onLand: @escaping () -> Void) {
        self.kind = kind
        self.onLand = onLand
        _landed = State(initialValue: !drops)
    }

    var body: some View {
        RouteMarker(kind: kind, size: 28, onMap: true)
            .scaleEffect(landed || reduceMotion ? 1 : 0.55, anchor: .bottom)
            .offset(y: landed || reduceMotion ? 0 : -24)
            .opacity(landed ? 1 : 0)
            .accessibilityLabel(kind == .start ? "Start pin" : "End pin")
            .onAppear {
                guard !landed else { return }
                withAnimation(reduceMotion ? Motion.reduced : .spring(duration: 0.5, bounce: 0.42)) { landed = true }
                onLand()
            }
    }
}

private extension Coordinate {
    var clLocation: CLLocationCoordinate2D { CLLocationCoordinate2D(latitude: lat, longitude: lng) }
}

// MARK: - Open seats

/// "Open seats" 1–4 (38×34 buttons, radius 10, sage when selected, cream otherwise).
private struct CreateSeatPicker: View {
    @Binding var seats: Int

    var body: some View {
        HStack(spacing: 10) {
            Text("Open seats")
                .sqFont(14, .semibold)
                .foregroundStyle(Theme.ink)
                .createLine(14)
            Spacer(minLength: 0)
            HStack(spacing: 6) {
                ForEach(1...4, id: \.self) { n in
                    Button { withMotion(Motion.quick) { seats = n } } label: {
                        Text("\(n)")
                            .sqFont(15, .semibold)
                            .foregroundStyle(Theme.ink)
                            .frame(width: 38, height: 34)
                            .background(n == seats ? Theme.sage : Theme.cream, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                            .contentShape(Rectangle().inset(by: -5))
                    }
                    .buttonStyle(.plain)
                    .sqBounce(when: n == seats, scale: 1.1)
                    .accessibilityLabel("\(n) open seat\(n > 1 ? "s" : "")")
                    .accessibilityAddTraits(n == seats ? .isSelected : [])
                }
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .background(.white, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .sensoryFeedback(.selection, trigger: seats)
    }
}

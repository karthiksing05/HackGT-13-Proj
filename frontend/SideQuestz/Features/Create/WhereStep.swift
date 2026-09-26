import CoreLocation
import MapKit
import SwiftUI

/// Create › Where: start (A) and end (B) pins, search + suggestions, map, travel range and ride.
///
/// Loading: until the default places arrive the pin rows and the suggestion pills shimmer; later
/// searches keep the pills and show three dots in the search field. Picks and map taps drop the
/// pin with a spring; the place names cross-fade.
struct CreateWhereStep: View {
    @Bindable var model: CreateFlowModel
    var focus: FocusState<CreateField?>.Binding

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            CreateStepTitle("Where do you start and end?")
            pinCard
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
            suggestionPills
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
            CreateCrossfade(value: model.ride) {
                CreateWrapText(text: model.ride.note, size: 13, color: Theme.text2)
            }
        }
        // New results pop in and everything below eases to the pills' new height.
        .animation(Motion.standard, value: model.suggestions)
        .animation(Motion.standard, value: model.suggestionsLoaded)
        .task(id: SuggestionKey(search: model.search, pin: model.editingPin)) {
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
        let loading = placeText == nil && !model.placesLoaded
        let label = isStart ? "Start" : "End · where you need to be by the end time"
        return Button {
            focus.wrappedValue = nil
            withMotion(Motion.quick) { model.selectPin(pin) }
        } label: {
            HStack(spacing: 12) {
                RouteMarker(kind: isStart ? .start : .end, size: 26)
                VStack(alignment: .leading, spacing: 0) {
                    CreateWrapText(text: label, size: 12, textStyle: .caption1, color: Theme.text3)
                    Text(placeText ?? (model.placesLoaded ? (isStart ? "Search for your start location" : "Search for your end location") : " "))
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

/// 220pt MapKit map (full content width, radius 16). Tap drops the pin being edited; pins are the
/// logo's markers (start = ring, end = diamond). No line between them.
///
/// The tap is a `SpatialTapGesture` recognized *alongside* MapKit's own pan/zoom recognizers: on a
/// device a real tap moves a few points, and an exclusive tap gesture loses it to MapKit. A pan
/// still moves the map (it moves too far to count as a tap). A pin that lands somewhere new drops
/// in with a spring; tapping also gives a light haptic.
private struct CreateMapCard: View {
    let model: CreateFlowModel

    @State private var camera: MapCameraPosition = .region(PlaceSearch.defaultRegion)
    @State private var visibleRegion: MKCoordinateRegion?
    /// Pin positions that have already landed, so a pin only drops when it lands somewhere new
    /// (not every time the map is rebuilt, e.g. coming back to this step).
    @State private var landedPins: Set<String>
    /// Bumped by every tap that drops a pin (light haptic).
    @State private var taps = 0

    init(model: CreateFlowModel) {
        self.model = model
        let shown = [model.start?.coordinate, model.endSameAsStart ? nil : model.end?.coordinate].compactMap { $0 }
        _landedPins = State(initialValue: Set(shown.map(Self.key)))
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
            .simultaneousGesture(SpatialTapGesture(coordinateSpace: .local).onEnded { value in
                guard let coordinate = proxy.convert(value.location, from: .local) else { return }
                taps += 1
                model.dropPin(at: Coordinate(lat: coordinate.latitude, lng: coordinate.longitude))
            })
            .onMapCameraChange(frequency: .onEnd) { context in
                visibleRegion = context.region
            }
        }
        .frame(height: 220)
        .frame(maxWidth: .infinity)
        .background(Theme.mapBackground)
        .clipShape(RoundedRectangle(cornerRadius: 16, style: .continuous))
        .overlay(alignment: .bottomLeading) {
            Text(model.editingPin == .end ? "Tap the map to drop the End pin" : "Tap the map to drop the Start pin")
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
        .accessibilityLabel(model.editingPin == .end ? "Map: tap to drop the end pin" : "Map: tap to drop the start pin")
        .sensoryFeedback(.impact(weight: .light), trigger: taps)
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
        let coordinates = [model.start?.coordinate, model.endSameAsStart ? nil : model.end?.coordinate].compactMap { $0 }
        guard !coordinates.isEmpty else { return }
        let region = visibleRegion ?? PlaceSearch.defaultRegion
        let inset = 0.12
        let allVisible = coordinates.allSatisfy { c in
            abs(c.lat - region.center.latitude) <= region.span.latitudeDelta * (0.5 - inset)
                && abs(c.lng - region.center.longitude) <= region.span.longitudeDelta * (0.5 - inset)
        }
        guard !allVisible else { return }
        let lats = coordinates.map(\.lat), lngs = coordinates.map(\.lng)
        guard let minLat = lats.min(), let maxLat = lats.max(), let minLng = lngs.min(), let maxLng = lngs.max() else { return }
        let fitted = MKCoordinateRegion(
            center: CLLocationCoordinate2D(latitude: (minLat + maxLat) / 2, longitude: (minLng + maxLng) / 2),
            span: MKCoordinateSpan(latitudeDelta: max(0.02, (maxLat - minLat) * 1.8), longitudeDelta: max(0.03, (maxLng - minLng) * 1.8))
        )
        withMotion(Motion.gentle) { camera = .region(fitted) }
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

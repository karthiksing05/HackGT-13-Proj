import CoreLocation
import MapKit
import SwiftUI

/// Create › Where: start (A) and end (B) pins, search + suggestions, map, travel range and ride.
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
            suggestionPills
            CreateMapCard(model: model)
            CreateEyebrow(text: "HOW FAR WILL YOU GO IN BETWEEN?", topMargin: 6)
            CreateChoiceCards(options: TravelRange.allCases.map { ($0, $0.label, $0.sublabel) }, selection: model.range) {
                model.range = $0
            }
            CreateEyebrow(text: "CAN YOU PROVIDE A RIDE THIS TIME?", topMargin: 6)
            CreateChoiceCards(options: RideChoice.allCases.map { ($0, $0.label, $0.sublabel) }, selection: model.ride) { ride in
                withAnimation(.easeOut(duration: 0.2)) { model.ride = ride }
            }
            if model.ride == .drive {
                CreateSeatPicker(seats: $model.openSeats)
                    .transition(.opacity)
            }
            CreateWrapText(text: model.ride.note, size: 13, color: Theme.text2)
        }
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
        let label = isStart ? "Start" : "End · where you need to be by the end time"
        return Button {
            focus.wrappedValue = nil
            withAnimation(.easeOut(duration: 0.15)) { model.selectPin(pin) }
        } label: {
            HStack(spacing: 12) {
                Text(isStart ? "A" : "B")
                    .sqFont(12, .bold)
                    .foregroundStyle(.white)
                    .frame(width: 26, height: 26)
                    .background(isStart ? Theme.sageInk : Theme.ink, in: Circle())
                VStack(alignment: .leading, spacing: 0) {
                    CreateWrapText(text: label, size: 12, textStyle: .caption1, color: Theme.text3)
                    Text(placeText ?? (model.placesLoaded ? (isStart ? "Search for your start location" : "Search for your end location") : " "))
                        .sqFont(15, .semibold)
                        .foregroundStyle(placeText == nil ? Theme.text3 : Theme.ink)
                        .createLine(15)
                }
                .multilineTextAlignment(.leading)
                .frame(maxWidth: .infinity, alignment: .leading)
                if active {
                    Text("Editing")
                        .sqFont(12, .semibold)
                        .foregroundStyle(Theme.sageInk)
                }
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 12)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(active ? Theme.sageTint : .clear)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("\(isStart ? "Start" : "End"): \(placeText ?? "not set")")
        .accessibilityValue(active ? "Editing" : "")
        .accessibilityAddTraits(active ? .isSelected : [])
    }

    // MARK: Suggestions

    private var suggestionPills: some View {
        FlowLayout(spacing: 8) {
            ForEach(model.suggestions) { suggestion in
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
        }
        .animation(.easeOut(duration: 0.15), value: model.suggestions)
    }

    private func pickFirstSuggestion() {
        guard let first = model.suggestions.first else { return }
        focus.wrappedValue = nil
        Task { await model.pick(first) }
    }
}

// MARK: - Map

/// 220pt MapKit map (full content width, radius 16). Tap drops the pin being edited; pins are
/// A (sage, ink letter) and B (ink, white letter). No line between them.
private struct CreateMapCard: View {
    let model: CreateFlowModel

    @State private var camera: MapCameraPosition = .region(PlaceSearch.defaultRegion)
    @State private var visibleRegion: MKCoordinateRegion?

    var body: some View {
        MapReader { proxy in
            Map(position: $camera, interactionModes: [.pan, .zoom]) {
                if !model.endSameAsStart, let end = model.end?.coordinate {
                    Annotation("End", coordinate: end.clLocation, anchor: .center) {
                        CreateMapPin(letter: "B", fill: Theme.ink, foreground: .white)
                    }
                    .annotationTitles(.hidden)
                }
                if let start = model.start?.coordinate {
                    Annotation("Start", coordinate: start.clLocation, anchor: .center) {
                        CreateMapPin(letter: "A", fill: Theme.sage, foreground: Theme.ink)
                    }
                    .annotationTitles(.hidden)
                }
            }
            .mapStyle(.standard(elevation: .flat, emphasis: .muted, pointsOfInterest: .excludingAll, showsTraffic: false))
            .mapControls {}
            .safeAreaPadding(.bottom, 34)
            .onTapGesture { location in
                guard let coordinate = proxy.convert(location, from: .local) else { return }
                Task { await model.dropPin(at: Coordinate(lat: coordinate.latitude, lng: coordinate.longitude)) }
            }
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
                .padding(.horizontal, 10)
                .padding(.vertical, 5)
                .background(.white, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                .padding(10)
                .allowsHitTesting(false)
                .accessibilityHidden(true)
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel(model.editingPin == .end ? "Map: tap to drop the end pin" : "Map: tap to drop the start pin")
        .onChange(of: model.start) { revealPins() }
        .onChange(of: model.end) { revealPins() }
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
        withAnimation(.easeInOut(duration: 0.35)) { camera = .region(fitted) }
    }
}

/// 28pt pin: colored circle with a 3pt white border, a 12pt bold letter and a soft shadow.
private struct CreateMapPin: View {
    let letter: String
    let fill: Color
    let foreground: Color

    var body: some View {
        Text(letter)
            .sqFont(12, .bold)
            .foregroundStyle(foreground)
            .frame(width: 22, height: 22)
            .background(fill, in: Circle())
            .padding(3)
            .background(.white, in: Circle())
            .shadow(color: .black.opacity(0.3), radius: 3, x: 0, y: 2)
            .accessibilityLabel(letter == "A" ? "Start pin" : "End pin")
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
                    Button { seats = n } label: {
                        Text("\(n)")
                            .sqFont(15, .semibold)
                            .foregroundStyle(Theme.ink)
                            .frame(width: 38, height: 34)
                            .background(n == seats ? Theme.sage : Theme.cream, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                            .contentShape(Rectangle().inset(by: -5))
                    }
                    .buttonStyle(.plain)
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

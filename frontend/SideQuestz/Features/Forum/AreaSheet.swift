import MapKit
import SwiftUI
import UIKit

/// Forum › area pill → "Forum area" sheet (GUI_PLAN.md §7.8): search, a map preview with the radius
/// circle, the area list and radius chips. Changes apply to the feed right away, like the prototype
/// (the feed reloads behind the sheet). The check, the chips and the map glide to each new pick.
///
/// Search finds any place (`env.places`: Apple Maps, then the server's `/places/search`); a place
/// picked from a search stays in the list, checked. "Current location" asks the phone first and
/// only switches once it has a coordinate; if the phone can't say, a note explains and the area
/// stays as it was.
struct ForumAreaSheet: View {
    @Binding var area: ForumArea
    @Binding var radiusMi: Int
    let done: () -> Void

    @Environment(AppEnvironment.self) private var env
    @State private var search = ""
    @State private var camera: MapCameraPosition
    /// Places found for the current search (nil until the first answer).
    @State private var results: [ForumArea]?
    @State private var searching = false
    /// Waiting for the phone's location ("Current location" shows dots).
    @State private var locating = false
    @State private var locationError: String?
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    init(area: Binding<ForumArea>, radiusMi: Binding<Int>, done: @escaping () -> Void) {
        _area = area
        _radiusMi = radiusMi
        self.done = done
        _camera = State(initialValue: ForumAreaMap.camera(area: area.wrappedValue, radiusMi: radiusMi.wrappedValue))
    }

    private var term: String { search.trimmingCharacters(in: .whitespaces) }

    var body: some View {
        SheetScaffold(spacing: 12) {
            Text("Forum area")
                .socialText(20, .bold)
                .foregroundStyle(Theme.ink)
                .accessibilityAddTraits(.isHeader)
            SearchField(text: $search, placeholder: "Search a neighborhood or city", fill: Theme.cream,
                        accessibilityLabel: "Search an area")
                .submitLabel(.search)
            mapPreview
            areaList
            if let locationError {
                Text(locationError)
                    .socialText(12)
                    .foregroundStyle(Theme.dangerText)
                    .fixedSize(horizontal: false, vertical: true)
                    .sqTransition(.rise)
            }
            radiusRow
            Button("Done", action: done)
                .buttonStyle(.sq(fill: Theme.sage, foreground: Theme.ink, height: 50))
        }
        .animation(Motion.standard, value: locationError)
        .onChange(of: area) { updateCamera() }
        .onChange(of: radiusMi) { updateCamera() }
        .task(id: term) { await runSearch() }
    }

    // MARK: Map

    private var mapPreview: some View {
        let center = ForumAreaMap.center(for: area)
        return Map(position: $camera, interactionModes: []) {
            MapCircle(center: center, radius: CLLocationDistance(radiusMi) * ForumAreaMap.metersPerMile)
                .foregroundStyle(Theme.sage.opacity(0.12))
                .stroke(Theme.sageInk.opacity(0.6), lineWidth: 1.5)
            Annotation("", coordinate: center, anchor: .center) {
                Circle()
                    .fill(Theme.sage)
                    .frame(width: 14, height: 14)
                    .overlay(Circle().stroke(.white, lineWidth: 3))
            }
            .annotationTitles(.hidden)
        }
        .mapStyle(.standard(emphasis: .muted, pointsOfInterest: .excludingAll))
        .frame(height: 140)
        .background(Theme.mapBackground)
        .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
        .allowsHitTesting(false)
        .accessibilityElement()
        .accessibilityLabel("Map of \(area.name) with a \(radiusMi) mile radius")
    }

    private func updateCamera() {
        let target = ForumAreaMap.camera(area: area, radiusMi: radiusMi)
        if reduceMotion {
            camera = target
        } else {
            withAnimation(Motion.gentle) { camera = target }
        }
    }

    // MARK: Areas

    /// No search: the suggestions ("Current location", the home base, the demo city's areas), plus
    /// a place picked from an earlier search (after "Current location"). Searching: the
    /// suggestions that match, then the places found.
    private var rows: [ForumArea] {
        let suggestions = ForumArea.suggestions(homeBase: env.user?.homeBase, isMock: env.isMock)
        guard !term.isEmpty else {
            var list = suggestions
            if !list.contains(where: { $0.id == area.id }) { list.insert(area, at: min(1, list.count)) }
            return list
        }
        let local = suggestions.filter { $0.name.localizedCaseInsensitiveContains(term) }
        let found = (results ?? []).filter { place in
            !local.contains { $0.name.caseInsensitiveCompare(place.name) == .orderedSame }
        }
        return local + found
    }

    private var areaList: some View {
        let rows = rows
        return VStack(spacing: 0) {
            ForEach(rows) { option in
                row(option)
                    .transition(.opacity)
            }
            if rows.isEmpty {
                Group {
                    if searching {
                        HStack(spacing: 8) {
                            Text("Searching")
                            LoadingDots(color: Theme.text3, dotSize: 5)
                        }
                        .accessibilityElement(children: .ignore)
                        .accessibilityLabel("Searching")
                    } else {
                        Text("No areas match that search.")
                    }
                }
                .socialText(15)
                .foregroundStyle(Theme.text3)
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 14)
                .frame(height: 44)
                .transition(.opacity)
            }
        }
        .background(Theme.cream)
        .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
        .animation(Motion.quick, value: area)
        .animation(Motion.quick, value: locating)
        .animation(Motion.standard, value: rows)
        .animation(Motion.standard, value: searching)
        .sensoryFeedback(.selection, trigger: area)
    }

    private func row(_ option: ForumArea) -> some View {
        let selected = option.id == area.id
        let finding = option.isCurrentLocation && locating
        return VStack(spacing: 0) {
            Button {
                pick(option)
            } label: {
                HStack(spacing: 10) {
                    Text(option.name)
                        .socialText(15, selected ? .semibold : .regular)
                        .foregroundStyle(Theme.ink)
                        .lineLimit(1)
                    Spacer(minLength: 0)
                    if finding {
                        LoadingDots(color: Theme.sageInk, dotSize: 5)
                            .sqTransition(.pop)
                    } else if selected {
                        SocialGlyph(kind: .check, size: 18, lineWidth: 2.6).foregroundStyle(Theme.sageInk)
                            .sqTransition(.pop)
                    }
                }
                .padding(.horizontal, 14)
                .frame(height: 43)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .disabled(locating)
            .accessibilityAddTraits(selected ? .isSelected : [])
            .accessibilityValue(finding ? "Finding your location" : "")
            RowDivider()
        }
    }

    private var radiusRow: some View {
        HStack(spacing: 8) {
            ForEach(ForumQuery.radii, id: \.self) { radius in
                SQChip(label: "\(radius) mi", isOn: radiusMi == radius, style: .creamGrid) { radiusMi = radius }
                    .accessibilityLabel("\(radius) mile\(radius == 1 ? "" : "s")")
            }
        }
    }

    // MARK: Actions

    private func pick(_ option: ForumArea) {
        if locationError != nil { withMotion(Motion.quick) { locationError = nil } }
        guard !option.isCurrentLocation else {
            locate()
            return
        }
        withMotion(Motion.quick) { area = option }
        if !term.isEmpty {
            // Back to the list, where the pick stays (checked).
            UIApplication.shared.sendAction(#selector(UIResponder.resignFirstResponder), to: nil, from: nil, for: nil)
            withMotion { search = "" }
        }
    }

    /// "Current location": switches only once the phone says where it is.
    private func locate() {
        guard !locating else { return }
        withMotion(Motion.quick) { locating = true }
        Task {
            let place = await env.location.currentLocation()
            withMotion(Motion.quick) {
                locating = false
                if let coordinate = place?.coordinate {
                    area = ForumArea(name: ForumArea.currentLocation.name, coordinate: coordinate, isCurrentLocation: true)
                } else {
                    locationError = "Couldn't get your location. Allow location access in Settings, or pick an area."
                }
            }
        }
    }

    /// Any place, through Apple Maps and the server (a short pause first, so typing doesn't search
    /// every letter). The rows already found stay until the new ones arrive.
    private func runSearch() async {
        let query = term
        guard !query.isEmpty else {
            results = nil
            searching = false
            return
        }
        searching = true
        try? await Task.sleep(for: .milliseconds(300))
        guard !Task.isCancelled else { return }
        // Around the area, else the home base, else the demo city.
        let near = area.coordinate ?? env.user?.homeBase?.coordinate ?? ForumArea.midtown.coordinate
        let places = await env.places.suggestions(for: query, near: near, limit: 5)
        guard !Task.isCancelled else { return }
        var seen = Set<String>()
        let found = places.compactMap { place -> ForumArea? in
            // "Current location" has its own row; a place without a center can't be an area.
            guard let coordinate = place.coordinate, !place.name.localizedCaseInsensitiveContains("current location"),
                  seen.insert(place.name.lowercased()).inserted else { return nil }
            return ForumArea(name: place.name, coordinate: coordinate)
        }
        withMotion {
            results = found
            searching = false
        }
    }
}

/// Map framing for the Forum area preview.
enum ForumAreaMap {
    static let metersPerMile: CLLocationDistance = 1609.344

    /// An area without a center yet falls back to Midtown for the preview (the sheet only picks
    /// "Current location" once it has one).
    static func center(for area: ForumArea) -> CLLocationCoordinate2D {
        let c = area.coordinate ?? ForumArea.midtown.coordinate ?? Coordinate(lat: 33.7838, lng: -84.3833)
        return CLLocationCoordinate2D(latitude: c.lat, longitude: c.lng)
    }

    /// Share of the 140pt map height the circle's diameter fills, as drawn in the prototype
    /// (1/2/5/10 mi → 40/68/104/136pt), so bigger radii read bigger while the map stays to scale.
    private static func heightShare(radiusMi: Int) -> Double {
        switch radiusMi {
        case ...1: 40.0 / 140
        case 2: 68.0 / 140
        case 3...5: 104.0 / 140
        default: 136.0 / 140
        }
    }

    static func camera(area: ForumArea, radiusMi: Int) -> MapCameraPosition {
        let diameter = 2 * CLLocationDistance(max(radiusMi, 1)) * metersPerMile
        let span = diameter / heightShare(radiusMi: radiusMi)
        // A small longitudinal span lets the map's height (the short side) set the zoom.
        return .region(MKCoordinateRegion(center: center(for: area), latitudinalMeters: span, longitudinalMeters: span * 0.5))
    }
}

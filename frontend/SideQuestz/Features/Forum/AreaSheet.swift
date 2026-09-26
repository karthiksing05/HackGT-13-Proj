import MapKit
import SwiftUI

/// Forum › area pill → "Forum area" sheet (GUI_PLAN.md §7.8): search, a map preview with the radius
/// circle, the area list and radius chips. Changes apply to the feed right away, like the prototype.
struct ForumAreaSheet: View {
    @Binding var area: String
    @Binding var radiusMi: Int
    let done: () -> Void

    @State private var search = ""
    @State private var camera: MapCameraPosition
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    init(area: Binding<String>, radiusMi: Binding<Int>, done: @escaping () -> Void) {
        _area = area
        _radiusMi = radiusMi
        self.done = done
        _camera = State(initialValue: ForumAreaMap.camera(area: area.wrappedValue, radiusMi: radiusMi.wrappedValue))
    }

    var body: some View {
        SheetScaffold(spacing: 12) {
            VStack(alignment: .leading, spacing: 0) {
                Text("Forum area")
                    .socialText(20, .bold)
                    .foregroundStyle(Theme.ink)
                    .accessibilityAddTraits(.isHeader)
                Text("See posts and plans from this area.")
                    .socialText(13)
                    .foregroundStyle(Theme.text3)
            }
            SearchField(text: $search, placeholder: "Search a neighborhood or city", fill: Theme.cream,
                        accessibilityLabel: "Search an area")
            mapPreview
            areaList
            radiusRow
            Button("Done", action: done)
                .buttonStyle(.sq(fill: Theme.sage, foreground: Theme.ink, height: 50))
        }
        .onChange(of: area) { updateCamera() }
        .onChange(of: radiusMi) { updateCamera() }
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
        .accessibilityLabel("Map of \(area) with a \(radiusMi) mile radius")
    }

    private func updateCamera() {
        let target = ForumAreaMap.camera(area: area, radiusMi: radiusMi)
        if reduceMotion {
            camera = target
        } else {
            withAnimation(.easeInOut(duration: 0.35)) { camera = target }
        }
    }

    // MARK: Areas

    private var matchingAreas: [String] {
        let term = search.trimmingCharacters(in: .whitespaces)
        guard !term.isEmpty else { return ForumQuery.areas }
        return ForumQuery.areas.filter { $0.localizedCaseInsensitiveContains(term) }
    }

    private var areaList: some View {
        VStack(spacing: 0) {
            ForEach(matchingAreas, id: \.self) { name in
                let selected = name == area
                Button {
                    area = name
                } label: {
                    HStack(spacing: 10) {
                        Text(name)
                            .socialText(15, selected ? .semibold : .regular)
                            .foregroundStyle(Theme.ink)
                        Spacer(minLength: 0)
                        if selected {
                            SocialGlyph(kind: .check, size: 18, lineWidth: 2.6).foregroundStyle(Theme.sageInk)
                        }
                    }
                    .padding(.horizontal, 14)
                    .frame(height: 43)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityAddTraits(selected ? .isSelected : [])
                RowDivider()
            }
            if matchingAreas.isEmpty {
                Text("No areas match that search.")
                    .socialText(15)
                    .foregroundStyle(Theme.text3)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 14)
                    .frame(height: 44)
            }
        }
        .background(Theme.cream)
        .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
        .sensoryFeedback(.selection, trigger: area)
    }

    private var radiusRow: some View {
        HStack(spacing: 8) {
            ForEach(ForumQuery.radii, id: \.self) { radius in
                SQChip(label: "\(radius) mi", isOn: radiusMi == radius, style: .creamGrid) { radiusMi = radius }
                    .accessibilityLabel("\(radius) mile\(radius == 1 ? "" : "s")")
            }
        }
    }
}

/// Map framing for the Forum area preview.
enum ForumAreaMap {
    static let metersPerMile: CLLocationDistance = 1609.344

    static func center(for area: String) -> CLLocationCoordinate2D {
        let c = ForumQuery.areaCenters[area] ?? Coordinate(lat: 33.7838, lng: -84.3833)
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

    static func camera(area: String, radiusMi: Int) -> MapCameraPosition {
        let diameter = 2 * CLLocationDistance(max(radiusMi, 1)) * metersPerMile
        let span = diameter / heightShare(radiusMi: radiusMi)
        // A small longitudinal span lets the map's height (the short side) set the zoom.
        return .region(MKCoordinateRegion(center: center(for: area), latitudinalMeters: span, longitudinalMeters: span * 0.5))
    }
}

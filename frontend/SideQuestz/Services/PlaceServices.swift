import CoreLocation
import Foundation
import MapKit

/// Place search for the Create › Where suggestion pills and reverse geocoding for dropped pins.
///
/// Mock mode serves the prototype's places first (so the demo matches), then fills with
/// `MKLocalSearch` results when a search has no mock matches. Live mode uses `MKLocalSearch`
/// and falls back to the backend's `/places/search`.
final class PlaceSearch {
    private let api: any APIClient
    private let isMock: Bool
    /// Midtown Atlanta
    static let defaultRegion = MKCoordinateRegion(
        center: CLLocationCoordinate2D(latitude: 33.7700, longitude: -84.3800),
        span: MKCoordinateSpan(latitudeDelta: 0.045, longitudeDelta: 0.06)
    )

    init(api: any APIClient, isMock: Bool) {
        self.api = api
        self.isMock = isMock
    }

    /// Up to `limit` places for a query. With an empty query the first is "Current location".
    func suggestions(for query: String, near: Coordinate?, limit: Int = 4) async -> [Place] {
        var results: [Place] = []
        if isMock {
            results = (try? await api.searchPlaces(query: query, near: near)) ?? []
        }
        let trimmed = query.trimmingCharacters(in: .whitespaces)
        if results.count < limit, !trimmed.isEmpty {
            results += await mapKitSearch(trimmed, near: near)
        }
        if results.isEmpty, !isMock {
            results = (try? await api.searchPlaces(query: query, near: near)) ?? []
        }
        var seen = Set<String>()
        return results.filter { seen.insert($0.name).inserted }.prefix(limit).map { $0 }
    }

    private func mapKitSearch(_ query: String, near: Coordinate?) async -> [Place] {
        let request = MKLocalSearch.Request()
        request.naturalLanguageQuery = query
        if let near {
            request.region = MKCoordinateRegion(center: CLLocationCoordinate2D(latitude: near.lat, longitude: near.lng),
                                                span: MKCoordinateSpan(latitudeDelta: 0.1, longitudeDelta: 0.1))
        } else {
            request.region = Self.defaultRegion
        }
        guard let response = try? await MKLocalSearch(request: request).start() else { return [] }
        return response.mapItems.prefix(4).map { item in
            let c = item.placemark.coordinate
            return Place(name: item.name ?? query, coordinate: Coordinate(lat: c.latitude, lng: c.longitude))
        }
    }

    /// A readable label for a dropped pin ("Dropped pin · 5th St NW"); never fails.
    func place(for coordinate: Coordinate) async -> Place {
        let location = CLLocation(latitude: coordinate.lat, longitude: coordinate.lng)
        if let placemark = try? await CLGeocoder().reverseGeocodeLocation(location).first {
            let label = placemark.name ?? placemark.thoroughfare
            if let label, !label.isEmpty { return Place(name: label, coordinate: coordinate) }
        }
        if !isMock, let place = try? await api.reverseGeocode(coordinate) { return place }
        return Place(name: "Dropped pin", coordinate: coordinate)
    }
}

/// One-shot current location. Mock mode returns Tech Square (the demo's "current location").
final class LocationService: NSObject, CLLocationManagerDelegate {
    private let isMock: Bool
    private let manager = CLLocationManager()
    private var continuation: CheckedContinuation<Coordinate?, Never>?

    init(isMock: Bool) {
        self.isMock = isMock
        super.init()
        manager.delegate = self
        manager.desiredAccuracy = kCLLocationAccuracyHundredMeters
    }

    func currentLocation() async -> Place {
        if isMock { return MockPlaces.techSquare.place }
        guard let coordinate = await requestCoordinate() else { return MockPlaces.techSquare.place }
        return Place(name: "Current location", coordinate: coordinate)
    }

    private func requestCoordinate() async -> Coordinate? {
        if manager.authorizationStatus == .notDetermined { manager.requestWhenInUseAuthorization() }
        return await withCheckedContinuation { continuation in
            self.continuation = continuation
            manager.requestLocation()
        }
    }

    nonisolated func locationManager(_ manager: CLLocationManager, didUpdateLocations locations: [CLLocation]) {
        let c = locations.last?.coordinate
        DispatchQueue.main.async {
            MainActor.assumeIsolated {
                self.continuation?.resume(returning: c.map { Coordinate(lat: $0.latitude, lng: $0.longitude) })
                self.continuation = nil
            }
        }
    }

    nonisolated func locationManager(_ manager: CLLocationManager, didFailWithError error: any Error) {
        DispatchQueue.main.async {
            MainActor.assumeIsolated {
                self.continuation?.resume(returning: nil)
                self.continuation = nil
            }
        }
    }
}

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
        } else if isMock {
            // The demo lives in Midtown. Live searches without a coordinate aren't biased anywhere.
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
///
/// Requests that overlap share one fix: everyone waiting gets the same answer. The first request
/// asks for permission if it was never asked, and a fix that doesn't come within 15 s counts as none.
final class LocationService: NSObject, CLLocationManagerDelegate {
    private let isMock: Bool
    private let manager = CLLocationManager()
    /// Everyone waiting for the fix in flight.
    private var waiters: [CheckedContinuation<Coordinate?, Never>] = []
    private var timeout: Task<Void, Never>?

    init(isMock: Bool) {
        self.isMock = isMock
        super.init()
        manager.delegate = self
        manager.desiredAccuracy = kCLLocationAccuracyHundredMeters
    }

    /// nil when the phone can't or won't say (no permission, no fix): screens then ask the user to
    /// pick a place instead of guessing one. The demo is always at Tech Square.
    func currentLocation() async -> Place? {
        if isMock { return MockPlaces.techSquare.place }
        guard let coordinate = await requestCoordinate() else { return nil }
        return Place(name: "Current location", coordinate: coordinate)
    }

    private func requestCoordinate() async -> Coordinate? {
        switch manager.authorizationStatus {
        case .denied, .restricted: return nil
        default: break
        }
        return await withCheckedContinuation { continuation in
            waiters.append(continuation)
            // A fix is already on its way: this caller gets it too.
            guard waiters.count == 1 else { return }
            if manager.authorizationStatus == .notDetermined {
                // The fix is requested once the person answers (`locationManagerDidChangeAuthorization`).
                manager.requestWhenInUseAuthorization()
            } else {
                manager.requestLocation()
            }
            timeout = Task { [weak self] in
                try? await Task.sleep(for: .seconds(15))
                guard !Task.isCancelled else { return }
                self?.finish(nil)
            }
        }
    }

    /// Answers everyone waiting.
    private func finish(_ coordinate: Coordinate?) {
        timeout?.cancel()
        timeout = nil
        let waiting = waiters
        waiters = []
        for waiter in waiting { waiter.resume(returning: coordinate) }
    }

    nonisolated func locationManagerDidChangeAuthorization(_ manager: CLLocationManager) {
        let status = manager.authorizationStatus
        DispatchQueue.main.async {
            MainActor.assumeIsolated {
                guard !self.waiters.isEmpty else { return }
                switch status {
                case .authorizedWhenInUse, .authorizedAlways: self.manager.requestLocation()
                case .denied, .restricted: self.finish(nil)
                default: break // still waiting for the answer
                }
            }
        }
    }

    nonisolated func locationManager(_ manager: CLLocationManager, didUpdateLocations locations: [CLLocation]) {
        let c = locations.last?.coordinate
        DispatchQueue.main.async {
            MainActor.assumeIsolated {
                self.finish(c.map { Coordinate(lat: $0.latitude, lng: $0.longitude) })
            }
        }
    }

    nonisolated func locationManager(_ manager: CLLocationManager, didFailWithError error: any Error) {
        DispatchQueue.main.async {
            MainActor.assumeIsolated {
                self.finish(nil)
            }
        }
    }
}

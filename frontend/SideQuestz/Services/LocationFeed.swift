import CoreLocation
import Foundation
import Observation

/// Where you are, live, from Apple's location services: Home's sidequests use it for the distances
/// on their progress strips and the "you are here" dot on their maps.
///
/// Updates run only while someone asks for them (`start()`, while Home's sidequests are on screen
/// and the app is in use; `stop()` otherwise). The feed never asks for permission by itself: only
/// the map's "Show my location" button does (`requestPermission()`). `LocationService` (a one-shot
/// fix for Create and the Forum) is separate.
@Observable
final class LocationFeed: NSObject, CLLocationManagerDelegate {
    /// The latest fix; nil until there is one, and without permission.
    private(set) var coordinate: Coordinate?
    /// What the person allowed.
    private(set) var authorization: CLAuthorizationStatus

    /// nil in the offline demo.
    @ObservationIgnored private let manager: CLLocationManager?
    /// Someone wants updates; they run once permission allows.
    @ObservationIgnored private var wanted = false
    @ObservationIgnored private var updating = false

    init(isMock: Bool) {
        if isMock {
            // Simulated: the offline demo stands at Tech Square, with permission granted, so the
            // dot and the distances show without the phone's location.
            manager = nil
            coordinate = MockPlaces.techSquare.coordinate
            authorization = .authorizedWhenInUse
            super.init()
            return
        }
        let manager = CLLocationManager()
        manager.desiredAccuracy = kCLLocationAccuracyNearestTenMeters
        manager.distanceFilter = 25
        self.manager = manager
        authorization = manager.authorizationStatus
        super.init()
        manager.delegate = self
    }

    /// Permission was never asked: the map offers "Show my location".
    var canAsk: Bool { authorization == .notDetermined }

    var isAllowed: Bool { authorization == .authorizedWhenInUse || authorization == .authorizedAlways }

    /// Keep the location coming (if permission allows; this never asks).
    func start() {
        wanted = true
        update()
    }

    func stop() {
        wanted = false
        update()
    }

    /// "Show my location": the system asks once. Updates start when it's allowed and wanted.
    func requestPermission() {
        guard let manager, manager.authorizationStatus == .notDetermined else { return }
        manager.requestWhenInUseAuthorization()
    }

    private func update() {
        guard let manager else { return }
        let run = wanted && isAllowed
        guard run != updating else { return }
        updating = run
        if run {
            manager.startUpdatingLocation()
        } else {
            manager.stopUpdatingLocation()
        }
    }

    private func authorizationChanged(_ status: CLAuthorizationStatus) {
        authorization = status
        if !isAllowed { coordinate = nil }
        update()
    }

    // MARK: CLLocationManagerDelegate (called on the main thread, where the manager was made)

    nonisolated func locationManagerDidChangeAuthorization(_ manager: CLLocationManager) {
        let status = manager.authorizationStatus
        DispatchQueue.main.async {
            MainActor.assumeIsolated {
                self.authorizationChanged(status)
            }
        }
    }

    nonisolated func locationManager(_ manager: CLLocationManager, didUpdateLocations locations: [CLLocation]) {
        // A negative accuracy means the fix isn't valid.
        guard let fix = locations.last(where: { $0.horizontalAccuracy >= 0 })?.coordinate else { return }
        DispatchQueue.main.async {
            MainActor.assumeIsolated {
                guard self.isAllowed else { return }
                self.coordinate = Coordinate(lat: fix.latitude, lng: fix.longitude)
            }
        }
    }

    nonisolated func locationManager(_ manager: CLLocationManager, didFailWithError error: any Error) {
        // Keep the last fix: updates carry on, and a denial arrives as an authorization change.
    }
}

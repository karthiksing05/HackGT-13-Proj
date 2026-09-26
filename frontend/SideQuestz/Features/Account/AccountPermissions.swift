import AVFoundation
import CoreLocation
import Observation
import Speech
import UIKit

/// Where one of the phone's permissions stands, as the Connected card shows it.
enum AccountPermissionState: Equatable {
    /// Allowed; the text says how ("While using the app", "On").
    case allowed(String)
    /// Never asked: tapping shows the system prompt (the app has no Settings switch for it yet).
    case notAsked
    /// Turned off: only the Settings app can turn it back on.
    case denied
    /// Blocked by Screen Time or a device profile; nothing the app can offer.
    case restricted
}

/// Location (CoreLocation) and voice input (Speech + microphone) permissions for Account › Connected.
///
/// Reads the real system state, asks for it when it was never asked, and otherwise opens the Settings
/// app. Call `start()` when the rows appear; the location state then updates itself (CoreLocation
/// reports every change), and `refresh()` picks up voice changes made in Settings.
@Observable
final class AccountPermissions: NSObject, CLLocationManagerDelegate {
    private(set) var location: AccountPermissionState = .notAsked
    private(set) var voice: AccountPermissionState = .notAsked
    /// A system prompt is up (the row shows dots meanwhile).
    private(set) var asking: Kind?

    enum Kind { case location, voice }

    /// Made on `start()`, so building this object costs nothing.
    @ObservationIgnored private var manager: CLLocationManager?

    func start() {
        if manager == nil {
            let manager = CLLocationManager()
            manager.delegate = self
            self.manager = manager
        }
        refresh()
    }

    func refresh() {
        if let manager { location = Self.locationState(manager.authorizationStatus) }
        voice = Self.voiceState(speech: SFSpeechRecognizer.authorizationStatus(),
                                microphone: AVAudioApplication.shared.recordPermission)
        if asking == .location && location != .notAsked { asking = nil }
    }

    /// The row's tap: ask when it was never asked, otherwise open Settings (where it can be changed).
    func handleTap(_ kind: Kind, openSettings: () -> Void) {
        let state = kind == .location ? location : voice
        switch state {
        case .notAsked:
            ask(kind)
        case .allowed, .denied:
            openSettings()
        case .restricted:
            break
        }
    }

    private func ask(_ kind: Kind) {
        guard asking == nil else { return }
        asking = kind
        switch kind {
        case .location:
            // The answer arrives in `locationManagerDidChangeAuthorization`.
            start()
            manager?.requestWhenInUseAuthorization()
        case .voice:
            Task {
                let speech = await Self.requestSpeech()
                if speech == .authorized { _ = await AVAudioApplication.requestRecordPermission() }
                asking = nil
                refresh()
            }
        }
    }

    nonisolated func locationManagerDidChangeAuthorization(_ manager: CLLocationManager) {
        let status = manager.authorizationStatus
        DispatchQueue.main.async {
            MainActor.assumeIsolated {
                self.location = Self.locationState(status)
                // Called once on creation too: only a real answer ends the prompt.
                if status != .notDetermined, self.asking == .location { self.asking = nil }
            }
        }
    }

    // MARK: Mapping

    private static func locationState(_ status: CLAuthorizationStatus) -> AccountPermissionState {
        switch status {
        case .authorizedAlways: .allowed("Always")
        case .authorizedWhenInUse: .allowed("While using the app")
        case .denied: .denied
        case .restricted: .restricted
        case .notDetermined: .notAsked
        @unknown default: .notAsked
        }
    }

    /// Voice input needs both: speech recognition and the microphone.
    private static func voiceState(speech: SFSpeechRecognizerAuthorizationStatus,
                                   microphone: AVAudioApplication.recordPermission) -> AccountPermissionState {
        if speech == .restricted { return .restricted }
        if speech == .denied || microphone == .denied { return .denied }
        if speech == .authorized && microphone == .granted { return .allowed("On") }
        return .notAsked
    }

    nonisolated private static func requestSpeech() async -> SFSpeechRecognizerAuthorizationStatus {
        await withCheckedContinuation { continuation in
            SFSpeechRecognizer.requestAuthorization { status in continuation.resume(returning: status) }
        }
    }
}

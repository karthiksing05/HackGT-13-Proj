import AuthenticationServices
import SafariServices
import SwiftUI
import UIKit

/// In-app browser (`SFSafariViewController`), shown as a sheet so you never leave SideQuests.
struct SafariView: UIViewControllerRepresentable {
    let url: URL

    func makeUIViewController(context: Context) -> SFSafariViewController {
        let controller = SFSafariViewController(url: url)
        controller.preferredControlTintColor = UIColor(Theme.sageInk)
        controller.dismissButtonStyle = .done
        return controller
    }

    func updateUIViewController(_ controller: SFSafariViewController, context: Context) {}
}

/// Camera capture (`UIImagePickerController`, `.camera`). Check `CameraPicker.isAvailable` first —
/// the simulator has no camera.
struct CameraPicker: UIViewControllerRepresentable {
    let onImage: (UIImage) -> Void
    @Environment(\.dismiss) private var dismiss

    static var isAvailable: Bool { UIImagePickerController.isSourceTypeAvailable(.camera) }

    func makeCoordinator() -> Coordinator { Coordinator(parent: self) }

    func makeUIViewController(context: Context) -> UIImagePickerController {
        let picker = UIImagePickerController()
        picker.sourceType = .camera
        picker.cameraDevice = .front
        picker.delegate = context.coordinator
        return picker
    }

    func updateUIViewController(_ controller: UIImagePickerController, context: Context) {}

    final class Coordinator: NSObject, UIImagePickerControllerDelegate, UINavigationControllerDelegate {
        let parent: CameraPicker
        init(parent: CameraPicker) { self.parent = parent }

        func imagePickerController(_ picker: UIImagePickerController, didFinishPickingMediaWithInfo info: [UIImagePickerController.InfoKey: Any]) {
            if let image = info[.originalImage] as? UIImage { parent.onImage(image) }
            parent.dismiss()
        }

        func imagePickerControllerDidCancel(_ picker: UIImagePickerController) {
            parent.dismiss()
        }
    }
}

extension UIImage {
    /// Downscaled JPEG for uploads.
    func uploadJPEG(maxDimension: CGFloat = 1400, quality: CGFloat = 0.82) -> Data? {
        let longest = max(size.width, size.height)
        guard longest > maxDimension else { return jpegData(compressionQuality: quality) }
        let scale = maxDimension / longest
        let target = CGSize(width: size.width * scale, height: size.height * scale)
        let resized = UIGraphicsImageRenderer(size: target).image { _ in draw(in: CGRect(origin: .zero, size: target)) }
        return resized.jpegData(compressionQuality: quality)
    }
}

/// Calendar connect via `ASWebAuthenticationSession`, pointed at the backend's OAuth URL.
/// The app never reads event details, only free/busy blocks (the backend does the syncing).
@MainActor
enum CalendarConnector {
    static func connect(_ provider: CalendarProvider, env: AppEnvironment) async throws {
        if env.isMock {
            try await env.api.completeIntegration(provider)
            return
        }
        let url = try await env.api.connectIntegration(provider)
        _ = try await OAuthSession().start(url: url, callbackScheme: "sidequestz")
        try await env.api.completeIntegration(provider)
    }
}

final class OAuthSession: NSObject, ASWebAuthenticationPresentationContextProviding {
    private var session: ASWebAuthenticationSession?

    func start(url: URL, callbackScheme: String) async throws -> URL {
        try await withCheckedThrowingContinuation { continuation in
            let session = ASWebAuthenticationSession(url: url, callbackURLScheme: callbackScheme) { callback, error in
                if let callback { continuation.resume(returning: callback) } else {
                    continuation.resume(throwing: error ?? APIError.network("Sign-in was cancelled."))
                }
            }
            session.presentationContextProvider = self
            session.prefersEphemeralWebBrowserSession = false
            self.session = session
            session.start()
        }
    }

    nonisolated func presentationAnchor(for session: ASWebAuthenticationSession) -> ASPresentationAnchor {
        MainActor.assumeIsolated {
            UIApplication.shared.connectedScenes
                .compactMap { $0 as? UIWindowScene }
                .flatMap(\.windows)
                .first { $0.isKeyWindow } ?? ASPresentationAnchor()
        }
    }
}

/// Mic button: sage or tint at rest, red `#B91C1C` with a soft red halo while listening.
struct MicButton: View {
    var size: CGFloat = 40
    var isListening: Bool
    /// Vibe step: sage fill + ink icon + 8pt tint halo. Setup answers: tint fill + sageInk icon, no halo.
    var idleFill: Color = Theme.sageTint
    var idleIcon: Color = Theme.sageInk
    var idleHalo: Color? = nil
    var haloWidth: CGFloat = 6
    var iconSize: CGFloat = 20
    var label: String = "Answer by voice"
    var listeningLabel: String = "Stop voice input"
    /// Mic loudness 0…1 while listening (`env.voice.level`): the halo swells with the voice.
    var level: Double = 0
    let action: () -> Void
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        Button(action: action) {
            Image(systemName: "mic")
                .font(.system(size: iconSize, weight: .regular))
                .foregroundStyle(isListening ? .white : idleIcon)
                .frame(width: size, height: size)
                .background(isListening ? Theme.danger : idleFill, in: Circle())
                .background {
                    if isListening {
                        // Breathes gently, and swells with the voice when there's sound.
                        TimelineView(.animation(minimumInterval: 1.0 / 30, paused: reduceMotion)) { timeline in
                            let breathe = reduceMotion ? 0 : (sin(timeline.date.timeIntervalSinceReferenceDate * 2 * .pi / 1.4) + 1) / 2
                            let extra = max(CGFloat(level) * size * 0.2, CGFloat(breathe) * 4)
                            Circle().fill(Theme.danger.opacity(0.15)).padding(-(haloWidth + extra))
                        }
                    } else if let idleHalo {
                        Circle().fill(idleHalo).padding(-haloWidth)
                    }
                }
                .frame(minWidth: Metrics.minTouch, minHeight: Metrics.minTouch)
                .contentShape(Circle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(isListening ? listeningLabel : label)
        .accessibilityAddTraits(isListening ? .isSelected : [])
        .animation(.easeInOut(duration: 0.2), value: isListening)
    }
}

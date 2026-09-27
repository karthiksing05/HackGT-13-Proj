import SwiftUI
import UIKit

/// The line with a mic (Create › Vibe, Setup › Tell us more) that says what voice input is doing:
/// a pulsing red dot and "Listening…" while the mic is on, rising dots and "Transcribing…" after
/// you stop, or why nothing came of it, with "Open Settings" when a permission is off. Idle (and
/// done) shows `idleText`, or nothing.
///
/// The caller sets the font and line box (`.sqFont(14, .semibold)`); states cross-fade in place.
struct VoiceStatusLine: View {
    let phase: VoicePhase
    var idleText: String?
    var listeningText = "Listening… tap to stop"
    var listeningColor: Color = Theme.ink
    var alignment: HorizontalAlignment = .leading
    @Environment(\.openURL) private var openURL

    var body: some View {
        ZStack {
            content
                .id(line)
                // The old line is gone before the new one fades in, so two never overlap.
                .transition(.asymmetric(insertion: .opacity.animation(.easeOut(duration: 0.18).delay(0.07)),
                                        removal: .opacity.animation(.easeIn(duration: 0.07))))
        }
        .frame(maxWidth: .infinity, alignment: Alignment(horizontal: alignment, vertical: .center))
        .animation(Motion.standard, value: line)
    }

    /// Which line shows (the words change with it; nothing else moves).
    private enum Line: Hashable { case idle, listening, transcribing, failed(VoiceFailure) }

    private var line: Line {
        switch phase {
        case .idle, .done: .idle
        case .starting, .listening: .listening
        case .transcribing: .transcribing
        case .failed(let failure): .failed(failure)
        }
    }

    @ViewBuilder private var content: some View {
        switch line {
        case .listening:
            HStack(spacing: 7) {
                VoiceRecordingDot()
                Text(listeningText).foregroundStyle(listeningColor)
            }
            .accessibilityElement(children: .combine)
        case .transcribing:
            HStack(spacing: 8) {
                LoadingDots(color: Theme.sageInk, dotSize: 5)
                Text("Transcribing…").foregroundStyle(Theme.sageInk)
            }
            .accessibilityElement(children: .ignore)
            .accessibilityLabel("Transcribing")
        case .failed(let failure):
            VStack(alignment: alignment, spacing: 0) {
                Text(failure.message)
                    .foregroundStyle(Theme.danger)
                    .multilineTextAlignment(alignment == .center ? .center : .leading)
                    .fixedSize(horizontal: false, vertical: true)
                    .onAppear { AccessibilityNotification.Announcement(failure.message).post() }
                if failure.opensSettings {
                    Button("Open Settings", action: openSettings)
                        .buttonStyle(.plain)
                        .foregroundStyle(Theme.sageInk)
                        // A 44pt target that lays out at the text's height.
                        .frame(minHeight: Metrics.minTouch)
                        .contentShape(Rectangle())
                        .padding(.vertical, -10)
                        .accessibilityHint("Opens SideQuests in the Settings app")
                }
            }
        case .idle:
            if let idleText {
                Text(idleText).foregroundStyle(Theme.ink)
            }
        }
    }

    private func openSettings() {
        guard let url = URL(string: UIApplication.openSettingsURLString) else { return }
        openURL(url)
    }
}

/// The red "recording" dot, pulsing gently (steady with Reduce Motion).
private struct VoiceRecordingDot: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        TimelineView(.animation(minimumInterval: 1.0 / 30, paused: reduceMotion)) { timeline in
            let wave = (sin(timeline.date.timeIntervalSinceReferenceDate * 2 * .pi / 1.2) + 1) / 2
            Circle()
                .fill(Theme.danger)
                .frame(width: 7, height: 7)
                .opacity(reduceMotion ? 1 : 0.45 + 0.55 * wave)
        }
        .frame(width: 7, height: 7)
        .accessibilityHidden(true)
    }
}

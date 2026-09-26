import AVFoundation
import Foundation
import Observation
import Speech

/// Voice turns into text and fills a text field. Nothing else changes.
///
/// Speech-to-text is Apple's Speech framework (`SFSpeechRecognizer`), on device when the phone
/// supports it. In mock mode, if it can't run (no permission, simulator without a mic), a canned
/// transcript keeps the demo working.
///
/// Usage from a view:
/// ```
/// Button { Task { if let text = await env.voice.toggle("vibe", demoTranscript: sample) { mood += text } } }
/// env.voice.isListening("vibe")   // drive the red mic state
/// env.voice.partial               // live partial transcript
/// env.voice.level                 // 0…1 mic loudness, for the pulsing halo
/// ```
@Observable
final class VoiceInput {
    /// Which field is currently recording.
    private(set) var activeKey: String?
    /// Live partial transcript while listening.
    private(set) var partial = ""
    /// Smoothed mic loudness while listening, 0…1 (0 when idle or in the canned demo).
    private(set) var level: Double = 0

    @ObservationIgnored private let allowDemoFallback: Bool
    @ObservationIgnored private let forceDemo: Bool
    @ObservationIgnored private var apple: AppleSpeechTranscriber?
    @ObservationIgnored private var usingDemo = false

    init(allowDemoFallback: Bool, forceDemo: Bool = false) {
        self.allowDemoFallback = allowDemoFallback
        self.forceDemo = forceDemo
    }

    var isListening: Bool { activeKey != nil }
    func isListening(_ key: String) -> Bool { activeKey == key }

    /// Tap on a mic: starts listening for `key`, or — if it's already listening — stops and
    /// returns the transcript (nil when nothing was heard). Tapping another mic switches fields.
    func toggle(_ key: String, demoTranscript: String) async -> String? {
        if activeKey == key { return await stop(demoTranscript: demoTranscript) }
        if activeKey != nil { cancel() }
        await start(key)
        return nil
    }

    /// Stops without returning text (e.g. leaving the screen).
    func cancel() {
        apple?.cancel()
        apple = nil
        usingDemo = false
        activeKey = nil
        partial = ""
        level = 0
    }

    private func start(_ key: String) async {
        activeKey = key
        partial = ""
        level = 0
        if forceDemo {
            usingDemo = true
            return
        }
        do {
            let transcriber = AppleSpeechTranscriber()
            transcriber.onPartial = { [weak self] text in self?.partial = text }
            transcriber.onLevel = { [weak self] loudness in
                guard let self else { return }
                // Rise fast, fall slowly, so the halo breathes with the voice.
                self.level = loudness > self.level ? loudness : self.level * 0.8 + loudness * 0.2
            }
            try await transcriber.start()
            apple = transcriber
            usingDemo = false
        } catch {
            usingDemo = allowDemoFallback
            if !usingDemo { activeKey = nil }
        }
    }

    private func stop(demoTranscript: String) async -> String? {
        defer {
            activeKey = nil
            partial = ""
            level = 0
        }
        if usingDemo {
            usingDemo = false
            return demoTranscript.isEmpty ? nil : demoTranscript
        }
        var text = ""
        if let apple {
            text = await apple.finish()
            self.apple = nil
        }
        text = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if text.isEmpty, allowDemoFallback, !demoTranscript.isEmpty { return demoTranscript }
        return text.isEmpty ? nil : text
    }
}

enum VoiceError: Error {
    case permissionDenied
    case unavailable
}

// MARK: - Apple Speech

final class AppleSpeechTranscriber {
    var onPartial: ((String) -> Void)?
    var onLevel: ((Double) -> Void)?
    private let recognizer = SFSpeechRecognizer(locale: .current) ?? SFSpeechRecognizer(locale: Locale(identifier: "en-US"))
    private var engine: AVAudioEngine?
    private var request: SFSpeechAudioBufferRecognitionRequest?
    private var task: SFSpeechRecognitionTask?
    private var latest = ""
    private var isFinal = false

    func start() async throws {
        guard await Self.speechAuthorization() == .authorized else { throw VoiceError.permissionDenied }
        guard await AVAudioApplication.requestRecordPermission() else { throw VoiceError.permissionDenied }
        guard let recognizer, recognizer.isAvailable else { throw VoiceError.unavailable }

        let session = AVAudioSession.sharedInstance()
        try session.setCategory(.record, mode: .measurement, options: .duckOthers)
        try session.setActive(true, options: .notifyOthersOnDeactivation)

        let engine = AVAudioEngine()
        let request = SFSpeechAudioBufferRecognitionRequest()
        request.shouldReportPartialResults = true
        request.addsPunctuation = true
        // Private and works offline when the phone has the on-device model for this language.
        if recognizer.supportsOnDeviceRecognition { request.requiresOnDeviceRecognition = true }
        let input = engine.inputNode
        let format = input.outputFormat(forBus: 0)
        guard format.sampleRate > 0 else { throw VoiceError.unavailable }
        Self.installTap(on: input, format: format, request: request, level: Self.levelHandler { [weak self] loudness in
            self?.onLevel?(loudness)
        })
        engine.prepare()
        try engine.start()

        self.engine = engine
        self.request = request
        task = recognizer.recognitionTask(with: request, resultHandler: Self.resultHandler { [weak self] text, final in
            guard let self else { return }
            if !text.isEmpty {
                self.latest = text
                self.onPartial?(text)
            }
            if final { self.isFinal = true }
        })
    }

    /// Stops the mic and waits briefly for the final result.
    func finish() async -> String {
        engine?.stop()
        engine?.inputNode.removeTap(onBus: 0)
        request?.endAudio()
        for _ in 0..<15 where !isFinal {
            try? await Task.sleep(nanoseconds: 100_000_000)
        }
        task?.cancel()
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
        return latest
    }

    func cancel() {
        engine?.stop()
        engine?.inputNode.removeTap(onBus: 0)
        task?.cancel()
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
    }

    // These run on audio / recognition threads, so they're built outside the main actor.
    nonisolated private static func installTap(on input: AVAudioInputNode, format: AVAudioFormat,
                                               request: SFSpeechAudioBufferRecognitionRequest,
                                               level: @escaping @Sendable (Double) -> Void) {
        input.installTap(onBus: 0, bufferSize: 1024, format: format) { buffer, _ in
            request.append(buffer)
            level(Self.loudness(of: buffer))
        }
    }

    /// RMS of the first channel mapped from about −50…0 dB to 0…1.
    nonisolated private static func loudness(of buffer: AVAudioPCMBuffer) -> Double {
        guard let samples = buffer.floatChannelData?[0], buffer.frameLength > 0 else { return 0 }
        let count = Int(buffer.frameLength)
        var sum: Float = 0
        for i in 0..<count { sum += samples[i] * samples[i] }
        let rms = (sum / Float(count)).squareRoot()
        let decibels = 20 * log10(max(rms, 0.000_01))
        return Double(min(max((decibels + 50) / 50, 0), 1))
    }

    nonisolated private static func levelHandler(_ deliver: @escaping @MainActor (Double) -> Void) -> @Sendable (Double) -> Void {
        { loudness in
            DispatchQueue.main.async {
                MainActor.assumeIsolated { deliver(loudness) }
            }
        }
    }

    nonisolated private static func resultHandler(_ deliver: @escaping @MainActor (String, Bool) -> Void) -> (SFSpeechRecognitionResult?, (any Error)?) -> Void {
        { result, error in
            let text = result?.bestTranscription.formattedString ?? ""
            let final = (result?.isFinal ?? false) || error != nil
            DispatchQueue.main.async {
                MainActor.assumeIsolated { deliver(text, final) }
            }
        }
    }

    nonisolated private static func speechAuthorization() async -> SFSpeechRecognizerAuthorizationStatus {
        await withCheckedContinuation { continuation in
            SFSpeechRecognizer.requestAuthorization { status in continuation.resume(returning: status) }
        }
    }
}

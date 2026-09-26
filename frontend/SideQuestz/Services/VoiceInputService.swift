import AVFoundation
import Foundation
import Observation
import Speech

/// Voice turns into text and fills a text field. Nothing else changes.
///
/// Provider order: Wispr Flow (REST, when `SQWisprFlowAPIKey` is set) → Apple Speech
/// (`SFSpeechRecognizer`). In mock mode, if neither can run (no permission, simulator without a
/// mic), a canned transcript keeps the demo working.
///
/// Usage from a view:
/// ```
/// Button { Task { if let text = await env.voice.toggle("vibe", demoTranscript: sample) { mood += text } } }
/// env.voice.isListening("vibe")   // drive the red mic state
/// env.voice.partial               // live partial transcript (Apple Speech only)
/// ```
@Observable
final class VoiceInput {
    /// Which field is currently recording.
    private(set) var activeKey: String?
    /// Live partial transcript while listening (Apple Speech only).
    private(set) var partial = ""

    @ObservationIgnored private let wisprAPIKey: String?
    @ObservationIgnored private let allowDemoFallback: Bool
    @ObservationIgnored private let forceDemo: Bool
    @ObservationIgnored private var apple: AppleSpeechTranscriber?
    @ObservationIgnored private var wispr: WisprFlowTranscriber?
    @ObservationIgnored private var usingDemo = false

    init(wisprAPIKey: String?, allowDemoFallback: Bool, forceDemo: Bool = false) {
        self.wisprAPIKey = wisprAPIKey?.isEmpty == false ? wisprAPIKey : nil
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
        wispr?.cancel()
        apple = nil
        wispr = nil
        usingDemo = false
        activeKey = nil
        partial = ""
    }

    private func start(_ key: String) async {
        activeKey = key
        partial = ""
        if forceDemo {
            usingDemo = true
            return
        }
        do {
            if let wisprAPIKey {
                let transcriber = WisprFlowTranscriber(apiKey: wisprAPIKey)
                try await transcriber.start()
                wispr = transcriber
            } else {
                let transcriber = AppleSpeechTranscriber()
                transcriber.onPartial = { [weak self] text in self?.partial = text }
                try await transcriber.start()
                apple = transcriber
            }
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
        }
        if usingDemo {
            usingDemo = false
            return demoTranscript.isEmpty ? nil : demoTranscript
        }
        var text = ""
        if let wispr {
            text = (try? await wispr.finish()) ?? ""
            self.wispr = nil
        } else if let apple {
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
    case badResponse
}

// MARK: - Apple Speech

final class AppleSpeechTranscriber {
    var onPartial: ((String) -> Void)?
    private let recognizer = SFSpeechRecognizer(locale: Locale(identifier: "en-US"))
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
        let input = engine.inputNode
        let format = input.outputFormat(forBus: 0)
        guard format.sampleRate > 0 else { throw VoiceError.unavailable }
        Self.installTap(on: input, format: format, request: request)
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
    nonisolated private static func installTap(on input: AVAudioInputNode, format: AVAudioFormat, request: SFSpeechAudioBufferRecognitionRequest) {
        input.installTap(onBus: 0, bufferSize: 1024, format: format) { buffer, _ in
            request.append(buffer)
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

// MARK: - Wispr Flow (REST)

/// Records 16 kHz mono WAV and sends it to Wispr Flow's transcription API
/// (https://api-docs.wisprflow.ai/rest_api_transcribe).
final class WisprFlowTranscriber {
    static let endpoint = URL(string: "https://platform-api.wisprflow.ai/api/v1/dash/api")!

    private let apiKey: String
    private var recorder: AVAudioRecorder?
    private let fileURL = FileManager.default.temporaryDirectory.appendingPathComponent("sq-voice.wav")

    init(apiKey: String) { self.apiKey = apiKey }

    func start() async throws {
        guard await AVAudioApplication.requestRecordPermission() else { throw VoiceError.permissionDenied }
        let session = AVAudioSession.sharedInstance()
        try session.setCategory(.record, mode: .spokenAudio)
        try session.setActive(true)
        let settings: [String: Any] = [
            AVFormatIDKey: Int(kAudioFormatLinearPCM),
            AVSampleRateKey: 16_000,
            AVNumberOfChannelsKey: 1,
            AVLinearPCMBitDepthKey: 16,
            AVLinearPCMIsFloatKey: false,
            AVLinearPCMIsBigEndianKey: false,
        ]
        let recorder = try AVAudioRecorder(url: fileURL, settings: settings)
        guard recorder.record() else { throw VoiceError.unavailable }
        self.recorder = recorder
    }

    func finish() async throws -> String {
        recorder?.stop()
        recorder = nil
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
        let audio = try Data(contentsOf: fileURL)

        struct Body: Encodable {
            struct Context: Encodable { struct App: Encodable { var type = "other" }; var app = App() }
            var audio: String
            var language = ["en"]
            var context = Context()
        }
        struct Response: Decodable { var text: String }

        var request = URLRequest(url: Self.endpoint)
        request.httpMethod = "POST"
        request.setValue("Bearer \(apiKey)", forHTTPHeaderField: "Authorization")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.httpBody = try JSONEncoder().encode(Body(audio: audio.base64EncodedString()))
        let (data, response) = try await URLSession.shared.data(for: request)
        guard (response as? HTTPURLResponse)?.statusCode == 200 else { throw VoiceError.badResponse }
        return try JSONDecoder().decode(Response.self, from: data).text
    }

    func cancel() {
        recorder?.stop()
        recorder = nil
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
    }
}

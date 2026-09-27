import AVFoundation
import Foundation
import Observation
import Speech

/// Voice turns into text and fills a text field. Nothing else changes.
///
/// Speech-to-text is Apple's Speech framework (`SFSpeechRecognizer`), on device when the phone
/// supports it. The words stream in while you talk (`partial`), and `phase(for:)` says what the mic
/// is doing so the field can show it: starting, listening (the halo follows `level`), transcribing
/// after you stop, then done, or failed with a reason the field can say (`VoiceFailure`).
///
/// In mock mode, when Apple Speech can't run (no permission, simulator without a mic) or with
/// `-SQVoiceDemo YES`, a scripted transcript streams in word by word instead (`ScriptedSpeech`), so
/// the offline demo shows the same thing.
///
/// Usage from a view:
/// ```
/// env.voice.toggle("vibe", demoTranscript: sample) { text in mood += text }   // a tap on the mic
/// env.voice.phase(for: "vibe")   // .listening, .transcribing, .failed(…) … for this field
/// env.voice.partial              // the words so far while listening and transcribing
/// env.voice.level                // 0…1 mic loudness, for the pulsing halo
/// ```
@Observable
final class VoiceInput {
    /// The field the mic is working for (its state is `phase`); every other field is idle.
    private(set) var activeKey: String?
    private(set) var phase: VoicePhase = .idle
    /// The words recognized so far while listening and transcribing; the final text once done.
    private(set) var partial = ""
    /// Smoothed mic loudness while listening, 0…1.
    private(set) var level: Double = 0

    @ObservationIgnored private let allowDemoFallback: Bool
    @ObservationIgnored private let forceDemo: Bool
    @ObservationIgnored private let minimumTranscribing: Duration
    @ObservationIgnored private let makeSpeech: () -> any SpeechSource
    @ObservationIgnored private let makeDemo: (String) -> any SpeechSource
    @ObservationIgnored private var source: (any SpeechSource)?
    @ObservationIgnored private var demoTranscript = ""
    @ObservationIgnored private var onText: ((String) -> Void)?
    /// Bumped by every start and cancel, so callbacks from an older session are ignored.
    @ObservationIgnored private var session = 0

    /// `minimumTranscribing`: "Transcribing…" stays up at least this long, so it reads as a step
    /// instead of a flash. `makeSpeech` / `makeDemo` build the word sources (tests pass their own).
    init(allowDemoFallback: Bool, forceDemo: Bool = false, minimumTranscribing: Duration = .milliseconds(450),
         makeSpeech: @escaping () -> any SpeechSource = { AppleSpeechTranscriber() },
         makeDemo: @escaping (String) -> any SpeechSource = { ScriptedSpeech(transcript: $0) }) {
        self.allowDemoFallback = allowDemoFallback
        self.forceDemo = forceDemo
        self.minimumTranscribing = minimumTranscribing
        self.makeSpeech = makeSpeech
        self.makeDemo = makeDemo
    }

    /// What the mic is doing for `key`'s field.
    func phase(for key: String) -> VoicePhase { activeKey == key ? phase : .idle }

    /// A tap on `key`'s mic: starts listening, or stops if it's listening; the final text then goes
    /// to `onText` (in the same update that ends the session, so the field and the mic change
    /// together). Taps while transcribing are ignored; a tap while the mic starts cancels it.
    /// Tapping another field's mic cancels this one.
    func toggle(_ key: String, demoTranscript: String, onText: @escaping (String) -> Void) {
        if activeKey == key {
            switch phase {
            case .starting:
                cancel()
                return
            case .listening:
                Task { await stop() }
                return
            case .transcribing:
                return
            case .idle, .done, .failed:
                break
            }
        }
        let id = begin(key, demoTranscript: demoTranscript, onText: onText)
        Task { await run(id) }
    }

    /// Starts listening for `key` (ending any other field's session) and returns once the mic is
    /// on, or the start failed (`phase` then says why).
    func start(_ key: String, demoTranscript: String, onText: @escaping (String) -> Void) async {
        await run(begin(key, demoTranscript: demoTranscript, onText: onText))
    }

    /// Stops listening; the final text lands after "Transcribing…". Nothing happens when idle.
    func stop() async {
        guard phase.isRecording, let source else { return }
        let id = session
        phase = .transcribing
        level = 0
        let clock = ContinuousClock()
        let started = clock.now
        let result: Result<String, VoiceFailure>
        do {
            result = .success(try await source.finish())
        } catch {
            result = .failure(error as? VoiceFailure ?? .unavailable)
        }
        let shown = clock.now - started
        if shown < minimumTranscribing { try? await Task.sleep(for: minimumTranscribing - shown) }
        guard id == session else { return }
        self.source = nil
        switch result {
        case .success(let heard):
            var text = heard.trimmingCharacters(in: .whitespacesAndNewlines)
            // Mock mode: a quiet room still gets the demo's words.
            if text.isEmpty, allowDemoFallback { text = demoTranscript }
            guard !text.isEmpty else { return fail(.nothingHeard, id) }
            partial = text
            onText?(text)
            onText = nil
            phase = .done
        case .failure(let failure):
            fail(failure, id)
        }
    }

    /// Stops without text (leaving the screen, switching fields); the field goes back to idle.
    func cancel() {
        session += 1
        source?.cancel()
        source = nil
        onText = nil
        activeKey = nil
        phase = .idle
        partial = ""
        level = 0
    }

    // MARK: Session

    /// Makes `key` the active field in the starting phase and returns the new session's id.
    private func begin(_ key: String, demoTranscript: String, onText: @escaping (String) -> Void) -> Int {
        cancel()
        activeKey = key
        phase = .starting
        self.demoTranscript = demoTranscript
        self.onText = onText
        return session
    }

    /// Turns the mic on for session `id`: Apple Speech, or the scripted demo (forced, or in mock
    /// mode when Apple Speech can't start).
    private func run(_ id: Int) async {
        let first = forceDemo ? makeDemo(demoTranscript) : makeSpeech()
        do {
            try await connect(first, id)
        } catch {
            guard id == session else { return }
            let failure = error as? VoiceFailure ?? .unavailable
            guard allowDemoFallback, !forceDemo else { return fail(failure, id) }
            do {
                try await connect(makeDemo(demoTranscript), id)
            } catch {
                return fail(error as? VoiceFailure ?? .unavailable, id)
            }
        }
        guard id == session else { return }
        phase = .listening
    }

    /// Wires `speech` to this session and starts it. A session cancelled meanwhile stops it again.
    private func connect(_ speech: any SpeechSource, _ id: Int) async throws {
        speech.onPartial = { [weak self] text in
            guard let self, id == self.session, self.phase.isActive else { return }
            self.partial = text
        }
        speech.onLevel = { [weak self] loudness in
            guard let self, id == self.session, self.phase == .listening else { return }
            // Rise fast, fall slowly, so the halo breathes with the voice.
            self.level = loudness > self.level ? loudness : self.level * 0.8 + loudness * 0.2
        }
        speech.onEnd = { [weak self] in
            // The recognizer stopped by itself (an error, a phone call): finish with what it heard.
            guard let self, id == self.session else { return }
            Task { await self.stop() }
        }
        try await speech.start()
        guard id == session else {
            speech.cancel()
            return
        }
        source = speech
    }

    private func fail(_ failure: VoiceFailure, _ id: Int) {
        guard id == session else { return }
        source?.cancel()
        source = nil
        onText = nil
        partial = ""
        level = 0
        phase = .failed(failure)
    }
}

/// What the mic is doing for one field.
enum VoicePhase: Equatable {
    case idle
    /// Asking for permission and turning the mic on.
    case starting
    /// Recording: `partial` updates as words are recognized.
    case listening
    /// Stopped; the final text is on its way.
    case transcribing
    /// The final text went into the field.
    case done
    /// Nothing came of it, and why.
    case failed(VoiceFailure)

    /// The mic is on or turning on (drawn red, a tap stops it).
    var isRecording: Bool { self == .starting || self == .listening }
    /// Between the tap that starts and the final text.
    var isActive: Bool { isRecording || self == .transcribing }

    var failure: VoiceFailure? {
        if case .failed(let failure) = self { return failure }
        return nil
    }
}

/// Why voice input didn't produce text, in words the field shows.
enum VoiceFailure: Error, Hashable {
    /// Speech recognition or the microphone is turned off for the app.
    case permissionDenied
    /// Screen Time or a device profile blocks speech recognition.
    case restricted
    /// No recognizer right now: offline without the on-device model, or the language isn't supported.
    case unavailable
    /// The microphone couldn't start (a call or another app has it, or there's no input).
    case microphone
    /// Stopped before any words were recognized.
    case nothingHeard

    var message: String {
        switch self {
        case .permissionDenied: "Turn on microphone and speech recognition for SideQuests in Settings."
        case .restricted: "Speech recognition is restricted on this iPhone. Type it instead."
        case .unavailable: "Speech recognition isn't available right now. Check your connection, or type it instead."
        case .microphone: "Couldn't start the microphone. Try again in a moment."
        case .nothingHeard: "Didn't catch that. Tap the mic and try again."
        }
    }

    /// Only the Settings app can fix it (the field offers "Open Settings").
    var opensSettings: Bool { self == .permissionDenied }
}

/// Where the words come from: Apple Speech on the phone's mic, or the demo's scripted stream.
/// Callbacks arrive on the main actor.
protocol SpeechSource: AnyObject {
    /// The whole transcript so far, each time it changes.
    var onPartial: ((String) -> Void)? { get set }
    /// Mic loudness, 0…1.
    var onLevel: ((Double) -> Void)? { get set }
    /// The source stopped by itself (the recognizer failed, the audio was interrupted); `finish()`
    /// collects the text.
    var onEnd: (() -> Void)? { get set }
    /// Turns the mic on; throws a `VoiceFailure`.
    func start() async throws
    /// Turns the mic off and returns the final text (after waiting briefly for the recognizer);
    /// throws a `VoiceFailure` when recognition failed before any words.
    func finish() async throws -> String
    /// Turns the mic off, no text.
    func cancel()
}

/// Joins Apple Speech's results into one transcript. Each result carries the whole utterance so
/// far, but on-device recognition can start over after a pause, sending only the words since;
/// those go after what was heard before instead of replacing it.
struct SpeechTranscript: Equatable {
    /// Utterances the recognizer has moved on from.
    private(set) var settled = ""
    /// The open utterance, as the latest result has it.
    private(set) var current = ""

    var text: String { Self.join(settled, current) }

    mutating func receive(_ result: String) {
        let result = result.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !result.isEmpty else { return }
        if Self.startsOver(from: current, to: result) { settled = text }
        current = result
    }

    /// A result that isn't a revision of the open utterance: fewer words, starting with a different
    /// one (revisions keep the start and grow).
    static func startsOver(from current: String, to result: String) -> Bool {
        let old = words(current)
        let new = words(result)
        guard old.count >= 2, let first = new.first, new.count < old.count else { return false }
        return first != old.first
    }

    private static func words(_ text: String) -> [String] {
        text.lowercased().split(whereSeparator: \.isWhitespace).map { $0.trimmingCharacters(in: .punctuationCharacters) }
    }

    private static func join(_ first: String, _ second: String) -> String {
        first.isEmpty ? second : second.isEmpty ? first : first + " " + second
    }
}

// MARK: - Apple Speech

/// Apple Speech on the phone's microphone: an `AVAudioEngine` tap feeds an
/// `SFSpeechAudioBufferRecognitionRequest` with partial results on, recognized on device when the
/// phone has the model for this language (private, and works offline). When on-device recognition
/// fails before any words (its model missing or still downloading), the same mic gets one more try
/// on Apple's servers.
final class AppleSpeechTranscriber: SpeechSource {
    var onPartial: ((String) -> Void)?
    var onLevel: ((Double) -> Void)?
    var onEnd: (() -> Void)?
    private let recognizer = SFSpeechRecognizer(locale: .current) ?? SFSpeechRecognizer(locale: Locale(identifier: "en-US"))
    private var engine: AVAudioEngine?
    /// Where the mic's buffers go: the current request.
    private let feed = SpeechFeed()
    private var request: SFSpeechAudioBufferRecognitionRequest?
    private var task: SFSpeechRecognitionTask?
    /// Counts recognition tasks, so a replaced one's late callbacks are ignored.
    private var attempt = 0
    private var transcript = SpeechTranscript()
    /// The recognition task is over (final result or error).
    private var ended = false
    /// It ended with an error before any words.
    private var failure: VoiceFailure?
    /// `finish()` or `cancel()` was called.
    private var stopping = false
    private var finalWaiter: CheckedContinuation<Void, Never>?
    private var interruptions: NSObjectProtocol?

    func start() async throws {
        try await Self.requestPermissions()
        guard let recognizer, recognizer.isAvailable else { throw VoiceFailure.unavailable }

        let session = AVAudioSession.sharedInstance()
        do {
            try session.setCategory(.record, mode: .measurement, options: .duckOthers)
            try session.setActive(true, options: .notifyOthersOnDeactivation)
        } catch {
            throw VoiceFailure.microphone
        }

        let engine = AVAudioEngine()
        let input = engine.inputNode
        let format = input.outputFormat(forBus: 0)
        guard format.sampleRate > 0, format.channelCount > 0 else {
            Self.deactivate()
            throw VoiceFailure.microphone
        }
        Self.installTap(on: input, format: format, feed: feed, level: Self.levelHandler { [weak self] loudness in
            self?.onLevel?(loudness)
        })
        engine.prepare()
        do {
            try engine.start()
        } catch {
            input.removeTap(onBus: 0)
            Self.deactivate()
            throw VoiceFailure.microphone
        }

        self.engine = engine
        recognize(with: recognizer, onDevice: recognizer.supportsOnDeviceRecognition)
        // A call or Siri takes the mic: finish with what was heard.
        interruptions = NotificationCenter.default.addObserver(forName: AVAudioSession.interruptionNotification, object: session,
                                                               queue: .main) { [weak self] _ in
            MainActor.assumeIsolated {
                guard let self, !self.stopping else { return }
                self.onEnd?()
            }
        }
    }

    /// Stops the mic and waits (up to 2 s) for the final result.
    func finish() async throws -> String {
        stopping = true
        stopAudio()
        request?.endAudio()
        if !ended {
            await withCheckedContinuation { continuation in
                finalWaiter = continuation
                Task {
                    try? await Task.sleep(for: .seconds(2))
                    self.resumeFinalWaiter()
                }
            }
        }
        task?.cancel()
        task = nil
        Self.deactivate()
        if transcript.text.isEmpty, let failure { throw failure }
        return transcript.text
    }

    func cancel() {
        stopping = true
        stopAudio()
        task?.cancel()
        task = nil
        resumeFinalWaiter()
        Self.deactivate()
    }

    /// Starts a recognition task on the mic's buffers, with partial results (and punctuation).
    private func recognize(with recognizer: SFSpeechRecognizer, onDevice: Bool) {
        let request = SFSpeechAudioBufferRecognitionRequest()
        request.shouldReportPartialResults = true
        request.addsPunctuation = true
        request.requiresOnDeviceRecognition = onDevice
        attempt += 1
        let current = attempt
        feed.request = request
        self.request = request
        task = recognizer.recognitionTask(with: request, resultHandler: Self.resultHandler { [weak self] text, isFinal, error in
            guard let self, current == self.attempt else { return }
            self.receive(text, ended: isFinal || error != nil, error: error)
        })
    }

    /// A result (the transcript so far) or the task's end: its final result, or an error.
    private func receive(_ text: String, ended: Bool, error: VoiceFailure?) {
        transcript.receive(text)
        if !text.isEmpty { onPartial?(transcript.text) }
        guard ended else { return }
        if error == .unavailable, transcript.text.isEmpty, !stopping, request?.requiresOnDeviceRecognition == true,
           let recognizer, recognizer.isAvailable {
            recognize(with: recognizer, onDevice: false)
            return
        }
        if let error, transcript.text.isEmpty { failure = error }
        self.ended = true
        resumeFinalWaiter()
        // The recognizer finished by itself (its time limit, an error): wrap up with what it heard.
        if !stopping { onEnd?() }
    }

    private func resumeFinalWaiter() {
        finalWaiter?.resume()
        finalWaiter = nil
    }

    private func stopAudio() {
        if let interruptions { NotificationCenter.default.removeObserver(interruptions) }
        interruptions = nil
        guard let engine else { return }
        engine.stop()
        engine.inputNode.removeTap(onBus: 0)
        self.engine = nil
    }

    // MARK: Permissions

    /// Asks for speech recognition, then the microphone, when they were never asked; throws when
    /// either is off.
    private static func requestPermissions() async throws {
        var speech = SFSpeechRecognizer.authorizationStatus()
        if speech == .notDetermined { speech = await speechAuthorization() }
        switch speech {
        case .authorized: break
        case .restricted: throw VoiceFailure.restricted
        default: throw VoiceFailure.permissionDenied
        }
        switch AVAudioApplication.shared.recordPermission {
        case .granted: break
        case .denied: throw VoiceFailure.permissionDenied
        default:
            guard await AVAudioApplication.requestRecordPermission() else { throw VoiceFailure.permissionDenied }
        }
    }

    private static func deactivate() {
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
    }

    // These run on audio / recognition threads, so they're built outside the main actor.
    nonisolated private static func installTap(on input: AVAudioInputNode, format: AVAudioFormat, feed: SpeechFeed,
                                               level: @escaping @Sendable (Double) -> Void) {
        input.installTap(onBus: 0, bufferSize: 1024, format: format) { buffer, _ in
            feed.append(buffer)
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

    /// Delivers the transcript so far, whether it's final, and the error that ended the task, if
    /// any ("No speech detected" means nothing was heard; anything else, that recognition failed).
    nonisolated private static func resultHandler(_ deliver: @escaping @MainActor (String, Bool, VoiceFailure?) -> Void)
        -> (SFSpeechRecognitionResult?, (any Error)?) -> Void {
        { result, error in
            let text = result?.bestTranscription.formattedString ?? ""
            let isFinal = result?.isFinal ?? false
            let failed = error != nil
            let noSpeech = (error as NSError?).map { $0.domain == "kAFAssistantErrorDomain" && $0.code == 1110 } ?? false
            DispatchQueue.main.async {
                MainActor.assumeIsolated {
                    deliver(text, isFinal, failed ? (noSpeech ? .nothingHeard : .unavailable) : nil)
                }
            }
        }
    }

    nonisolated private static func speechAuthorization() async -> SFSpeechRecognizerAuthorizationStatus {
        await withCheckedContinuation { continuation in
            SFSpeechRecognizer.requestAuthorization { status in continuation.resume(returning: status) }
        }
    }
}

/// The request the mic's buffers go to. The tap runs on the audio thread while the request can be
/// replaced from the main one (on device → Apple's servers), so it sits behind a lock.
private nonisolated final class SpeechFeed: @unchecked Sendable {
    private let lock = NSLock()
    private var current: SFSpeechAudioBufferRecognitionRequest?

    var request: SFSpeechAudioBufferRecognitionRequest? {
        get { lock.withLock { current } }
        set { lock.withLock { current = newValue } }
    }

    func append(_ buffer: AVAudioPCMBuffer) {
        request?.append(buffer)
    }
}

// MARK: - Demo

/// Simulated: the demo's stand-in for the microphone and recognizer. It "hears" `transcript` word by
/// word at a speaking pace (about 150 words a minute, a beat longer after commas and periods), with
/// a mic level that swells on each word, then stays quiet until stopped; stopping settles on the
/// whole transcript after a short transcribing pause.
final class ScriptedSpeech: SpeechSource {
    var onPartial: ((String) -> Void)?
    var onLevel: ((Double) -> Void)?
    var onEnd: (() -> Void)?
    private let words: [String]
    private let wordTime: Duration
    private let settleTime: Duration
    private var playback: Task<Void, Never>?

    init(transcript: String, wordTime: Duration = .milliseconds(360), settleTime: Duration = .milliseconds(1100)) {
        words = transcript.split(whereSeparator: \.isWhitespace).map(String.init)
        self.wordTime = wordTime
        self.settleTime = settleTime
    }

    func start() async throws {
        playback = Task { [weak self] in await self?.play() }
    }

    func finish() async throws -> String {
        playback?.cancel()
        playback = nil
        try? await Task.sleep(for: settleTime)
        return words.joined(separator: " ")
    }

    func cancel() {
        playback?.cancel()
        playback = nil
    }

    /// Four level readings per word (three while it's said, then a gap), three more quiet ones after
    /// punctuation, and the words so far after each.
    private func play() async {
        let tick = wordTime / 4
        for index in words.indices {
            for step in 0..<4 {
                onLevel?(step < 3 ? 0.55 + 0.1 * Double((index * 7 + step * 3) % 4) : 0.15)
                try? await Task.sleep(for: tick)
                if Task.isCancelled { return }
            }
            onPartial?(words[...index].joined(separator: " "))
            if words[index].last.map({ ",.;!?".contains($0) }) == true {
                for _ in 0..<3 {
                    onLevel?(0.08)
                    try? await Task.sleep(for: tick)
                    if Task.isCancelled { return }
                }
            }
        }
        while !Task.isCancelled {
            onLevel?(0.04)
            try? await Task.sleep(for: tick)
        }
    }
}

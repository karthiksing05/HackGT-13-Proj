import Foundation
import Testing
@testable import SideQuestz

/// Voice input's states (idle → starting → listening → transcribing → done, or failed with a
/// reason), the words that stream in while you talk, the demo's word-by-word stream, and Apple
/// Speech starting over after a pause.
@MainActor
struct VoiceFeedbackTests {
    // MARK: Listening → transcribing → done

    @Test func wordsStreamInThenTheFinalTextLands() async throws {
        let speech = FakeSpeech()
        let voice = VoiceInput(allowDemoFallback: false, minimumTranscribing: .zero, makeSpeech: { speech })
        var delivered: [String] = []
        #expect(voice.phase(for: "q1") == .idle)

        await voice.start("q1", demoTranscript: "", onText: { delivered.append($0) })
        #expect(voice.phase(for: "q1") == .listening)
        #expect(voice.phase(for: "q2") == .idle, "other fields stay idle")

        speech.say("Walking")
        #expect(voice.partial == "Walking")
        speech.say("Walking somewhere green")
        #expect(voice.partial == "Walking somewhere green")
        speech.level(0.9)
        #expect(voice.level == 0.9)
        #expect(delivered.isEmpty, "nothing lands in the field before you stop")

        speech.holdsFinish = true
        let stopping = Task { await voice.stop() }
        try await until { speech.finishing }
        #expect(voice.phase(for: "q1") == .transcribing)
        #expect(voice.level == 0)
        // A late result still updates the words on screen.
        speech.say("Walking somewhere green,")
        #expect(voice.partial == "Walking somewhere green,")
        #expect(delivered.isEmpty)

        speech.release("Walking somewhere green, then tacos.")
        await stopping.value
        #expect(voice.phase(for: "q1") == .done)
        #expect(delivered == ["Walking somewhere green, then tacos."])
        #expect(voice.partial == "Walking somewhere green, then tacos.")
    }

    /// The mic shows "starting" while permissions are asked, then listening.
    @Test func startsBeforeItListens() async throws {
        let speech = FakeSpeech()
        speech.holdsStart = true
        let voice = VoiceInput(allowDemoFallback: false, minimumTranscribing: .zero, makeSpeech: { speech })
        let starting = Task { await voice.start("vibe", demoTranscript: "", onText: { _ in }) }
        try await until { speech.starting }
        #expect(voice.phase(for: "vibe") == .starting)
        #expect(voice.phase(for: "vibe").isRecording)
        speech.releaseStart()
        await starting.value
        #expect(voice.phase(for: "vibe") == .listening)
    }

    /// Two taps on the same mic: listen, then stop; the text arrives through the first tap's callback.
    @Test func togglingStartsAndStops() async throws {
        let speech = FakeSpeech()
        speech.holdsFinish = true
        let voice = VoiceInput(allowDemoFallback: false, minimumTranscribing: .zero, makeSpeech: { speech })
        var delivered: [String] = []

        voice.toggle("vibe", demoTranscript: "", onText: { delivered.append($0) })
        #expect(voice.phase(for: "vibe") == .starting, "the tap shows at once")
        try await until { voice.phase(for: "vibe") == .listening }

        voice.toggle("vibe", demoTranscript: "", onText: { delivered.append("second tap: \($0)") })
        try await until { speech.finishing }
        #expect(voice.phase(for: "vibe") == .transcribing)
        // Taps while transcribing wait.
        voice.toggle("vibe", demoTranscript: "", onText: { delivered.append("third tap: \($0)") })
        #expect(voice.phase(for: "vibe") == .transcribing)
        speech.release("Something chill and outside.")
        try await until { voice.phase(for: "vibe") == .done }
        #expect(delivered == ["Something chill and outside."])
    }

    /// "Transcribing…" stays up long enough to read even when the text is back at once.
    @Test func transcribingIsAStepNotAFlash() async throws {
        let speech = FakeSpeech()
        speech.finalText = "Tacos"
        let voice = VoiceInput(allowDemoFallback: false, minimumTranscribing: .milliseconds(200), makeSpeech: { speech })
        await voice.start("q1", demoTranscript: "", onText: { _ in })
        let clock = ContinuousClock()
        let started = clock.now
        await voice.stop()
        #expect(clock.now - started >= .milliseconds(200))
        #expect(voice.phase(for: "q1") == .done)
    }

    // MARK: Failures

    @Test func permissionOffSaysSoAndOffersSettings() async {
        let speech = FakeSpeech()
        speech.startFailure = .permissionDenied
        let voice = VoiceInput(allowDemoFallback: false, minimumTranscribing: .zero, makeSpeech: { speech })
        var delivered: [String] = []
        await voice.start("q2", demoTranscript: "", onText: { delivered.append($0) })
        #expect(voice.phase(for: "q2") == .failed(.permissionDenied))
        #expect(voice.phase(for: "q2").failure?.opensSettings == true)
        #expect(VoiceFailure.permissionDenied.message.contains("microphone and speech recognition"))
        #expect(VoiceFailure.permissionDenied.message.contains("Settings"))
        #expect(delivered.isEmpty)
        // Only permissions send you to Settings.
        for failure in [VoiceFailure.restricted, .unavailable, .microphone, .nothingHeard] {
            #expect(!failure.opensSettings)
            #expect(!failure.message.isEmpty)
        }
    }

    @Test func silenceSaysNothingWasHeard() async {
        let speech = FakeSpeech()
        let voice = VoiceInput(allowDemoFallback: false, minimumTranscribing: .zero, makeSpeech: { speech })
        var delivered: [String] = []
        await voice.start("q1", demoTranscript: "", onText: { delivered.append($0) })
        await voice.stop()
        #expect(voice.phase(for: "q1") == .failed(.nothingHeard))
        #expect(delivered.isEmpty)
    }

    @Test func recognitionFailureSaysSo() async {
        let speech = FakeSpeech()
        speech.finishFailure = .unavailable
        let voice = VoiceInput(allowDemoFallback: false, minimumTranscribing: .zero, makeSpeech: { speech })
        await voice.start("q1", demoTranscript: "", onText: { _ in })
        await voice.stop()
        #expect(voice.phase(for: "q1") == .failed(.unavailable))
    }

    /// The recognizer stopping by itself (its time limit, a phone call) wraps up with what it heard.
    @Test func recognizerEndingByItselfDeliversTheWords() async throws {
        let speech = FakeSpeech()
        let voice = VoiceInput(allowDemoFallback: false, minimumTranscribing: .zero, makeSpeech: { speech })
        var delivered: [String] = []
        await voice.start("q3", demoTranscript: "", onText: { delivered.append($0) })
        speech.say("No car, vegetarian")
        speech.finalText = "No car, vegetarian."
        speech.end()
        try await until { voice.phase(for: "q3") == .done }
        #expect(delivered == ["No car, vegetarian."])
    }

    // MARK: Switching and leaving

    @Test func cancelGoesBackToIdleWithoutText() async {
        let speech = FakeSpeech()
        let voice = VoiceInput(allowDemoFallback: false, minimumTranscribing: .zero, makeSpeech: { speech })
        var delivered: [String] = []
        await voice.start("q1", demoTranscript: "", onText: { delivered.append($0) })
        speech.say("Walking")
        voice.cancel()
        #expect(voice.phase(for: "q1") == .idle)
        #expect(voice.partial.isEmpty)
        #expect(speech.cancelled)
        // A late result from the cancelled session doesn't come back.
        speech.say("Walking somewhere")
        #expect(voice.partial.isEmpty)
        #expect(delivered.isEmpty)
    }

    @Test func anotherMicTakesOver() async {
        let first = FakeSpeech()
        let second = FakeSpeech()
        var sources = [first, second]
        let voice = VoiceInput(allowDemoFallback: false, minimumTranscribing: .zero, makeSpeech: { sources.removeFirst() })
        await voice.start("q1", demoTranscript: "", onText: { _ in })
        first.say("Walking")
        await voice.start("q2", demoTranscript: "", onText: { _ in })
        #expect(first.cancelled)
        #expect(voice.phase(for: "q1") == .idle)
        #expect(voice.phase(for: "q2") == .listening)
        #expect(voice.partial.isEmpty)
        first.say("Walking somewhere")
        #expect(voice.partial.isEmpty, "the first mic's late words stay out")
        second.say("Crowds")
        #expect(voice.partial == "Crowds")
    }

    // MARK: Demo (mock mode)

    /// Mock mode without Apple Speech (no permission, a simulator without a mic): the demo's
    /// words stream in instead, then the whole transcript lands.
    @Test func mockModeStreamsTheDemoTranscript() async throws {
        let speech = FakeSpeech()
        speech.startFailure = .permissionDenied
        let voice = VoiceInput(allowDemoFallback: true, minimumTranscribing: .zero, makeSpeech: { speech },
                               makeDemo: { ScriptedSpeech(transcript: $0, wordTime: .milliseconds(40), settleTime: .zero) })
        var delivered: [String] = []
        await voice.start("vibe", demoTranscript: "Something chill and outside.", onText: { delivered.append($0) })
        #expect(voice.phase(for: "vibe") == .listening)
        try await until { !voice.partial.isEmpty }
        #expect(voice.partial.hasPrefix("Something"))
        #expect(voice.partial != "Something chill and outside.", "the words arrive one at a time")
        try await until { voice.partial == "Something chill and outside." }
        #expect(delivered.isEmpty)
        await voice.stop()
        #expect(voice.phase(for: "vibe") == .done)
        #expect(delivered == ["Something chill and outside."])
    }

    /// Mock mode: a quiet room still gets the demo's words.
    @Test func mockModeFallsBackToTheDemoWordsWhenNothingWasHeard() async {
        let speech = FakeSpeech()
        let voice = VoiceInput(allowDemoFallback: true, minimumTranscribing: .zero, makeSpeech: { speech })
        var delivered: [String] = []
        await voice.start("q1", demoTranscript: "Tacos with friends.", onText: { delivered.append($0) })
        await voice.stop()
        #expect(delivered == ["Tacos with friends."])
    }

    @Test func scriptedSpeechSaysOneWordAtATime() async throws {
        let demo = ScriptedSpeech(transcript: "Anything with huge crowds.", wordTime: .milliseconds(8), settleTime: .zero)
        var partials: [String] = []
        var levels: [Double] = []
        demo.onPartial = { partials.append($0) }
        demo.onLevel = { levels.append($0) }
        try await demo.start()
        try await until { partials.count == 4 }
        #expect(partials == ["Anything", "Anything with", "Anything with huge", "Anything with huge crowds."])
        #expect(levels.contains { $0 > 0.5 }, "the level swells on words")
        #expect(levels.allSatisfy { (0...1).contains($0) })
        #expect(try await demo.finish() == "Anything with huge crowds.")
    }

    /// The demo pauses a beat after punctuation, like someone talking.
    @Test func scriptedSpeechPausesAfterPunctuation() async throws {
        let demo = ScriptedSpeech(transcript: "One, two", wordTime: .milliseconds(80), settleTime: .zero)
        let clock = ContinuousClock()
        var times: [ContinuousClock.Instant] = []
        demo.onPartial = { _ in times.append(clock.now) }
        let started = clock.now
        try await demo.start()
        try await until { times.count == 2 }
        demo.cancel()
        let first = times[0] - started
        let second = times[1] - times[0]
        #expect(second > first, "the word after a comma comes later than a word's own time")
    }

    // MARK: Apple Speech results

    @Test func revisionsReplaceTheUtterance() {
        var transcript = SpeechTranscript()
        transcript.receive("Some")
        transcript.receive("Something chill")
        transcript.receive("Something chill and outside")
        transcript.receive("Something chill and outside.")
        #expect(transcript.text == "Something chill and outside.")
        // Same length, different first word: still a revision.
        transcript.receive("Nothing chill and outside.")
        #expect(transcript.text == "Nothing chill and outside.")
    }

    /// On-device recognition can start over after a pause with only the new words: they go after
    /// what was said before instead of replacing it.
    @Test func aRestartAfterAPauseAppends() {
        var transcript = SpeechTranscript()
        transcript.receive("Something chill")
        transcript.receive("Something chill and outside")
        transcript.receive("then")
        #expect(transcript.text == "Something chill and outside then")
        transcript.receive("then cheap food after")
        #expect(transcript.text == "Something chill and outside then cheap food after")
        transcript.receive("")
        #expect(transcript.text == "Something chill and outside then cheap food after")
    }

    @Test func restartRule() {
        #expect(SpeechTranscript.startsOver(from: "Something chill and outside", to: "then"))
        #expect(!SpeechTranscript.startsOver(from: "Something chill", to: "Something chill and"))
        #expect(!SpeechTranscript.startsOver(from: "Some", to: "Something"), "one word can't tell a restart from a revision")
        #expect(!SpeechTranscript.startsOver(from: "Something chill and", to: "something"), "same first word")
        #expect(!SpeechTranscript.startsOver(from: "", to: "Walking"))
    }

    // MARK: Helpers

    /// Waits (up to 2 s) for `condition`, letting other main-actor work run meanwhile.
    private func until(_ condition: () -> Bool, sourceLocation: SourceLocation = #_sourceLocation) async throws {
        for _ in 0..<400 where !condition() {
            try await Task.sleep(for: .milliseconds(5))
        }
        #expect(condition(), "timed out", sourceLocation: sourceLocation)
    }
}

/// A word source the tests drive: `say` a partial, `end` the recognizer, hold `start()` or
/// `finish()` until released.
private final class FakeSpeech: SpeechSource {
    var onPartial: ((String) -> Void)?
    var onLevel: ((Double) -> Void)?
    var onEnd: (() -> Void)?
    var startFailure: VoiceFailure?
    var finishFailure: VoiceFailure?
    var finalText = ""
    var holdsStart = false
    var holdsFinish = false
    private(set) var starting = false
    private(set) var finishing = false
    private(set) var cancelled = false
    private var heldStart: CheckedContinuation<Void, Never>?
    private var heldFinish: CheckedContinuation<Void, Never>?

    func start() async throws {
        starting = true
        if holdsStart { await withCheckedContinuation { heldStart = $0 } }
        if let startFailure { throw startFailure }
    }

    func finish() async throws -> String {
        finishing = true
        if holdsFinish { await withCheckedContinuation { heldFinish = $0 } }
        if let finishFailure { throw finishFailure }
        return finalText
    }

    func cancel() {
        cancelled = true
    }

    func say(_ text: String) { onPartial?(text) }
    func level(_ value: Double) { onLevel?(value) }
    func end() { onEnd?() }

    func releaseStart() {
        heldStart?.resume()
        heldStart = nil
    }

    func release(_ text: String) {
        finalText = text
        heldFinish?.resume()
        heldFinish = nil
    }
}

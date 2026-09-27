import SwiftUI

/// Setup · 5 "Tell us more": three open questions, typed or spoken (Apple Speech). Spoken words
/// appear in the box as they're recognized, after whatever is already there; the line under the
/// question says "Listening…", then "Transcribing…" until the final text lands, or what went wrong.
struct SetupMoreStep: View {
    @Environment(AppEnvironment.self) private var env
    @Bindable var draft: SetupDraft

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            SetupHeading(title: "Tell us more",
                         subtitle: "Optional. The more you share, the better your first picks.")
            ForEach(SetupQuestion.all) { question in
                SetupQuestionCard(question: question,
                                  answer: Binding(get: { draft.answer(question.answerKey) },
                                                  set: { draft.setAnswer(question.answerKey, $0) }))
            }
        }
        .onDisappear { env.voice.cancel() }
    }
}

/// The three questions. `demoTranscript` is what the mock voice service "hears" (prototype copy).
struct SetupQuestion: Identifiable {
    let id: String
    let answerKey: String
    let prompt: String
    let placeholder: String
    let demoTranscript: String

    static let all = [
        SetupQuestion(id: "q1", answerKey: "perfectAfternoon", prompt: "Describe your perfect free afternoon.",
                      placeholder: "e.g. a long walk somewhere green, then tacos with a couple friends",
                      demoTranscript: "Walking somewhere green with a coffee, then trying a new food spot with one or two friends. Nothing too loud."),
        SetupQuestion(id: "q2", answerKey: "neverDo", prompt: "What's something you'd never want to do on a sidequest?",
                      placeholder: "e.g. anything with huge crowds or long lines",
                      demoTranscript: "Anything with huge crowds or long lines. Also not a fan of super early mornings."),
        SetupQuestion(id: "q3", answerKey: "planAround", prompt: "Anything we should plan around?",
                      placeholder: "Budget, dietary needs, accessibility, no car, etc.",
                      demoTranscript: "I don't have a car, I'm vegetarian, and I usually try to keep it under twenty bucks."),
    ]
}

private struct SetupQuestionCard: View {
    @Environment(AppEnvironment.self) private var env
    let question: SetupQuestion
    @Binding var answer: String
    /// What the box held when the mic started: the words you say go after it as they're recognized.
    @State private var base: String?
    @FocusState private var typing: Bool

    private var phase: VoicePhase { env.voice.phase(for: question.id) }
    /// Listening, transcribing or a failure: the line under the question says so.
    private var showsStatus: Bool { phase != .idle && phase != .done }

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .top, spacing: 10) {
                Text(question.prompt)
                    .sqFont(15, .semibold)
                    .foregroundStyle(Theme.ink)
                    .authLineHeight(1.35, size: 15)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityAddTraits(.isHeader)
                // The halo swells with your voice while listening; dots while transcribing.
                MicButton(size: 40, isListening: phase.isRecording, isBusy: phase == .transcribing,
                          idleFill: Theme.sageTint, idleIcon: Theme.sageInk, haloWidth: 6, iconSize: 19,
                          level: phase.isRecording ? env.voice.level : 0, action: toggleVoice)
                    .padding(-2)
                    .accessibilityIdentifier("setup.voice.\(question.id)")
            }
            if showsStatus {
                VoiceStatusLine(phase: phase, listeningText: "Listening… tap the mic again to stop", listeningColor: Theme.danger)
                    .sqFont(13, .semibold)
                    .authLineHeight(1.35, size: 13)
                    .sqTransition(.rise)
            }
            SetupAnswerField(text: $answer, placeholder: question.placeholder, label: question.prompt,
                             live: phase.isActive, pending: phase == .transcribing, typing: $typing)
                .accessibilityIdentifier("setup.answer.\(question.id)")
        }
        .padding(14)
        .background(.white, in: RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
        .authMotion(value: showsStatus)
        .onChange(of: env.voice.partial) { _, words in follow(words) }
        .onChange(of: phase.isActive) { _, active in
            if !active { base = nil }
        }
    }

    /// While the mic is on, the box shows the words so far after what was already in it.
    private func follow(_ words: String) {
        guard let base, phase.isActive else { return }
        answer = Self.join(base, words)
    }

    private func toggleVoice() {
        typing = false
        if !phase.isActive { base = answer }
        env.voice.toggle(question.id, demoTranscript: question.demoTranscript) { text in
            // The final text takes the place of the live words.
            answer = Self.join(base ?? answer, text)
            base = nil
        }
    }

    /// Spoken words go after typed ones, with a space.
    static func join(_ typed: String, _ spoken: String) -> String {
        let typed = typed.trimmingCharacters(in: .whitespacesAndNewlines)
        let spoken = spoken.trimmingCharacters(in: .whitespacesAndNewlines)
        return typed.isEmpty ? spoken : spoken.isEmpty ? typed : typed + " " + spoken
    }
}

/// 3-line text area: `field` fill, 1pt `line` border, radius 12, 15pt with a 1.35 line height.
/// While voice input fills it (`live`), the border turns sage and typing waits; while the words
/// are transcribed (`pending`) they're dimmed, then settle when the final text lands.
private struct SetupAnswerField: View {
    @Binding var text: String
    let placeholder: String
    let label: String
    var live = false
    var pending = false
    var typing: FocusState<Bool>.Binding

    /// Extra leading for a 1.35 line height at 15pt.
    private let leading = 15 * (1.35 - AuthFontMetrics.sf)

    var body: some View {
        TextField("", text: $text, prompt: Text(placeholder).foregroundStyle(Theme.text3), axis: .vertical)
            .lineLimit(3...8)
            .sqFont(15)
            .foregroundStyle(Theme.ink)
            .lineSpacing(leading)
            .focused(typing)
            .opacity(pending ? 0.55 : 1)
            // rows="3": three 20.25pt lines (the reserved empty lines don't get line spacing).
            .frame(minHeight: 3 * 15 * 1.35 - leading, alignment: .topLeading)
            // CSS: 10pt padding + 1pt border + half-leading.
            .padding(.horizontal, 13)
            .padding(.vertical, 11 + leading / 2)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Theme.field, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .strokeBorder(live ? Theme.sage : Theme.line, lineWidth: live ? 1.5 : 1)
            }
            .allowsHitTesting(!live)
            .animation(Motion.standard, value: pending)
            .animation(Motion.quick, value: live)
            .accessibilityLabel(label)
    }
}

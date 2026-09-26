import SwiftUI

/// Setup · 5 "Tell us more": three open questions, typed or spoken (Apple Speech). Transcripts are
/// appended to whatever is already in the box.
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
    /// Counts transcripts that landed in this card's box (each one fades in).
    @State private var transcripts = 0
    /// Whether the last transcript was added after text that was already there.
    @State private var appended = false

    private var listening: Bool { env.voice.isListening(question.id) }

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
                // The halo swells with your voice while listening.
                MicButton(size: 40, isListening: listening, idleFill: Theme.sageTint, idleIcon: Theme.sageInk,
                          haloWidth: 6, iconSize: 19, level: listening ? env.voice.level : 0, action: toggleVoice)
                    .padding(-2)
            }
            if listening {
                Text("Listening… tap the mic again to stop")
                    .sqFont(13, .semibold)
                    .foregroundStyle(Theme.danger)
                    .authLineHeight(1.35, size: 13)
                    .sqTransition(.rise)
            }
            SetupAnswerField(text: $answer, placeholder: question.placeholder, label: question.prompt,
                             transcripts: transcripts, appended: appended)
        }
        .padding(14)
        .background(.white, in: RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
        .authMotion(value: listening)
    }

    private func toggleVoice() {
        Task {
            if let text = await env.voice.toggle(question.id, demoTranscript: question.demoTranscript) {
                let current = answer
                // The box grows to fit and the words fade in.
                withMotion {
                    answer = current.isEmpty ? text : current + " " + text
                    appended = !current.isEmpty
                    transcripts += 1
                }
            }
        }
    }
}

/// 3-line text area: `field` fill, 1pt `line` border, radius 12, 15pt with a 1.35 line height.
private struct SetupAnswerField: View {
    @Binding var text: String
    let placeholder: String
    let label: String
    /// Bumped when a transcript lands: the text fades in (from dim when it was added to earlier text).
    var transcripts = 0
    var appended = false

    /// Extra leading for a 1.35 line height at 15pt.
    private let leading = 15 * (1.35 - AuthFontMetrics.sf)

    var body: some View {
        TextField("", text: $text, prompt: Text(placeholder).foregroundStyle(Theme.text3), axis: .vertical)
            .lineLimit(3...8)
            .sqFont(15)
            .foregroundStyle(Theme.ink)
            .lineSpacing(leading)
            // Only the words fade (the box stays put).
            .keyframeAnimator(initialValue: 1.0, trigger: transcripts) { field, opacity in
                field.opacity(opacity)
            } keyframes: { _ in
                MoveKeyframe(appended ? 0.35 : 0)
                CubicKeyframe(1, duration: 0.5)
            }
            // rows="3": three 20.25pt lines (the reserved empty lines don't get line spacing).
            .frame(minHeight: 3 * 15 * 1.35 - leading, alignment: .topLeading)
            // CSS: 10pt padding + 1pt border + half-leading.
            .padding(.horizontal, 13)
            .padding(.vertical, 11 + leading / 2)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(Theme.field, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: 12, style: .continuous).strokeBorder(Theme.line, lineWidth: 1)
            }
            .accessibilityLabel(label)
    }
}

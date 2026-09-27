import SwiftUI

/// Create › Vibe: voice (Apple Speech) or typed mood, quick picks, budget, who's coming, and at the
/// bottom the must-see picks every option has to include (`CreateMustSeeSection`).
///
/// Motion: the mic's halo swells with your voice; the transcript rises in and its words settle as
/// they update; quick picks pop when chosen; the who's-coming note cross-fades.
struct CreateVibeStep: View {
    @Bindable var model: CreateFlowModel
    var focus: FocusState<CreateField?>.Binding

    @Environment(AppEnvironment.self) private var env

    private static let voiceKey = "vibe"

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            CreateStepTitle("What are you in the mood for?")
            voiceCard
            moodField
            CreateEyebrow(text: "QUICK PICKS", topMargin: 4)
            FlowLayout(spacing: 8) {
                ForEach(CreateFlowModel.quickPicks, id: \.self) { tag in
                    let isOn = model.tags.contains(tag)
                    SQChip(label: tag, isOn: isOn, style: .tag) { model.toggleTag(tag) }
                        .sqBounce(when: isOn, scale: 1.08)
                }
            }
            CreateEyebrow(text: "BUDGET", topMargin: 4)
            CreateChoiceGrid(options: CreateFlowModel.budgetLabels.enumerated().map { ($0.offset, $0.element) },
                             selection: model.budget, fontSize: 15) { model.budget = $0 }
            CreateEyebrow(text: "WHO'S COMING", topMargin: 4)
            CreateChoiceGrid(options: Visibility.allCases.map { ($0, $0.label) }, selection: model.who) { model.who = $0 }
            CreateCrossfade(value: model.who) {
                CreateWrapText(text: model.who.note, size: 13, color: Theme.text2)
            }
            CreateMustSeeSection(model: model, focus: focus)
        }
    }

    /// The prototype's textarea: 2 rows of 16pt text in 1.35 line boxes.
    @ScaledMetric private var moodMinHeight: CGFloat = 43.2

    // MARK: Voice

    private var voicePhase: VoicePhase { env.voice.phase(for: Self.voiceKey) }

    /// The words so far while you talk and while they're transcribed, then the transcript that
    /// went into "Or type it".
    private var quote: String? {
        guard voicePhase.isActive else { return model.transcript }
        let partial = env.voice.partial.trimmingCharacters(in: .whitespacesAndNewlines)
        return partial.isEmpty ? nil : partial
    }

    private var voiceCard: some View {
        VStack(spacing: 10) {
            MicButton(size: 76, isListening: voicePhase.isRecording, isBusy: voicePhase == .transcribing,
                      idleFill: Theme.sage, idleIcon: Theme.ink, idleHalo: Theme.sageTint, haloWidth: 8, iconSize: 26,
                      label: "Start voice input", listeningLabel: "Stop voice input",
                      level: env.voice.level, action: toggleVoice)
                .accessibilityIdentifier("create.voice")
            VoiceStatusLine(phase: voicePhase, idleText: "Tap and say what you want", alignment: .center)
                .sqFont(14, .semibold)
                .createLine(14)
            if let quote {
                // Wraps like the prototype; new words settle in as the transcript updates (dimmed
                // until the final text lands). VoiceOver reads the words so far, even mid-roll.
                CreateLiveText(text: "“\(quote)”", size: 15, color: voicePhase == .transcribing ? Theme.text3 : Theme.text2,
                               lineHeight: 1.4, alignment: .center)
                    .accessibilityElement(children: .ignore)
                    .accessibilityLabel("“\(quote)”")
                    .accessibilityAddTraits(voicePhase.isActive ? [.isStaticText, .updatesFrequently] : .isStaticText)
                    .sqTransition(.rise)
            }
        }
        .padding(.vertical, 18)
        .padding(.horizontal, 14)
        .frame(maxWidth: .infinity)
        .background(.white, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
        // The card eases to the transcript's height as it grows.
        .animation(Motion.standard, value: quote)
        .animation(Motion.standard, value: voicePhase)
    }

    private func toggleVoice() {
        focus.wrappedValue = nil
        env.voice.toggle(Self.voiceKey, demoTranscript: CreateFlowModel.voiceDemoTranscript) { text in
            model.applyTranscript(text)
        }
    }

    // MARK: Typed mood

    private var moodField: some View {
        CreateFieldCard(label: "Or type it", spacing: 4) {
            TextField("", text: $model.moodText,
                      prompt: Text("e.g. low-key, outdoors, under $20").foregroundStyle(Theme.text3),
                      axis: .vertical)
                .lineLimit(2...5)
                .sqFont(16)
                .foregroundStyle(Theme.ink)
                .createLine(16)
                .frame(minHeight: moodMinHeight, alignment: .topLeading)
                .focused(focus, equals: .mood)
                .submitLabel(.done)
                .accessibilityLabel("Or type it")
                .onChange(of: model.moodText) { _, text in
                    // Return finishes typing instead of adding a new line.
                    guard text.contains("\n") else { return }
                    model.moodText = text.replacingOccurrences(of: "\n", with: " ").trimmingCharacters(in: .whitespaces)
                    focus.wrappedValue = nil
                }
        }
    }
}

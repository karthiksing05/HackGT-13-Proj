import SwiftUI

/// Create › Vibe: voice (Wispr Flow) or typed mood, quick picks, budget and who's coming.
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
                    SQChip(label: tag, isOn: model.tags.contains(tag), style: .tag) { model.toggleTag(tag) }
                }
            }
            CreateEyebrow(text: "BUDGET", topMargin: 4)
            CreateChoiceGrid(options: CreateFlowModel.budgetLabels.enumerated().map { ($0.offset, $0.element) },
                             selection: model.budget, fontSize: 15) { model.budget = $0 }
            CreateEyebrow(text: "WHO'S COMING", topMargin: 4)
            CreateChoiceGrid(options: Visibility.allCases.map { ($0, $0.label) }, selection: model.who) { model.who = $0 }
            CreateWrapText(text: model.who.note, size: 13, color: Theme.text2)
        }
    }

    /// The prototype's textarea: 2 rows of 16pt text in 1.35 line boxes.
    @ScaledMetric private var moodMinHeight: CGFloat = 43.2

    // MARK: Voice

    private var isListening: Bool { env.voice.isListening(Self.voiceKey) }

    /// Live partial while listening, then the returned transcript.
    private var quote: String? {
        if isListening {
            let partial = env.voice.partial.trimmingCharacters(in: .whitespacesAndNewlines)
            return partial.isEmpty ? nil : partial
        }
        return model.transcript
    }

    private var voiceCard: some View {
        VStack(spacing: 10) {
            MicButton(size: 76, isListening: isListening, idleFill: Theme.sage, idleIcon: Theme.ink,
                      idleHalo: Theme.sageTint, haloWidth: 8, iconSize: 26,
                      label: "Start voice input", listeningLabel: "Stop voice input", action: toggleVoice)
            Text(isListening ? "Listening… tap to stop" : "Tap and say what you want")
                .sqFont(14, .semibold)
                .foregroundStyle(Theme.ink)
                .createLine(14)
            if let quote {
                CreateWrapText(text: "“\(quote)”", size: 15, color: Theme.text2, lineHeight: 1.4, alignment: .center)
                    .transition(.opacity)
            }
            Text("Voice input · Wispr Flow")
                .sqFont(12, relativeTo: .caption)
                .foregroundStyle(Theme.text3)
                .createLine(12, relativeTo: .caption)
        }
        .padding(.vertical, 18)
        .padding(.horizontal, 14)
        .frame(maxWidth: .infinity)
        .background(.white, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
        .animation(.easeOut(duration: 0.2), value: quote)
    }

    private func toggleVoice() {
        focus.wrappedValue = nil
        Task {
            if let text = await env.voice.toggle(Self.voiceKey, demoTranscript: CreateFlowModel.voiceDemoTranscript) {
                model.applyTranscript(text)
            }
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

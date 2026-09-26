import SwiftUI

/// Rate a past event (GUI_PLAN.md §7.5c): stars, "What stood out?" tags, an optional note.
/// Saving calls `PUT /ratings/{itemId}`; the button shows loading dots while it runs, then the
/// sheet closes and the Past row turns its "Rate" pill into the stars.
struct HomeRateSheet: View {
    let event: PastEvent
    let close: () -> Void
    /// Called with the rating after the server saved it.
    let saved: (Rating) -> Void

    @Environment(AppEnvironment.self) private var env
    @State private var stars: Int
    @State private var tags: [String]
    @State private var note: String
    @State private var isSaving = false
    @State private var errorMessage: String?
    @FocusState private var noteFocused: Bool
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    init(event: PastEvent, close: @escaping () -> Void, saved: @escaping (Rating) -> Void) {
        self.event = event
        self.close = close
        self.saved = saved
        _stars = State(initialValue: event.rating?.stars ?? 0)
        _tags = State(initialValue: event.rating?.tags ?? [])
        _note = State(initialValue: event.rating?.note ?? "")
    }

    var body: some View {
        SheetScaffold {
            VStack(alignment: .leading, spacing: 2) {
                Text(event.subtitle)
                    .sqFont(13, relativeTo: .footnote)
                    .foregroundStyle(Theme.text3)
                    .homeLine(13)
                Text("How was \(event.title)?")
                    .sqFont(22, .bold, relativeTo: .title2)
                    .foregroundStyle(Theme.ink)
                    .homeLine(22)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityAddTraits(.isHeader)
            }
            VStack(spacing: 6) {
                StarPicker(rating: $stars, size: 40, target: CGSize(width: 52, height: 52), spacing: 6)
                    .accessibilityElement(children: .contain)
                    .accessibilityLabel("Star rating")
                Text(Rating.starWords[min(max(stars, 0), 5)])
                    .sqFont(15, .semibold)
                    .foregroundStyle(stars > 0 ? Theme.sageInk : Theme.text3)
                    .homeLine(15)
                    .contentTransition(.opacity)
                    .animation(reduceMotion ? Motion.reduced : Motion.quick, value: stars)
            }
            .frame(maxWidth: .infinity)
            Text("WHAT STOOD OUT? (OPTIONAL)")
                .sqFont(13, .semibold, relativeTo: .footnote)
                .tracking(0.4)
                .foregroundStyle(Theme.text3)
                .homeLine(13)
                .accessibilityAddTraits(.isHeader)
            FlowLayout(spacing: 8) {
                ForEach(Rating.tagOptions, id: \.self) { tag in
                    SQChip(label: tag, isOn: tags.contains(tag), style: .rateTag) { toggle(tag) }
                }
            }
            noteField
            if let errorMessage {
                Text(errorMessage)
                    .sqFont(13)
                    .foregroundStyle(Theme.danger)
                    .accessibilityAddTraits(.isStaticText)
                    .sqTransition(.rise)
            }
            Button(action: save) {
                // Loading dots replace the label while the rating saves (same size, no jump).
                ZStack {
                    Text(stars > 0 ? "Save rating" : "Pick a star rating")
                        .contentTransition(.opacity)
                        .opacity(isSaving ? 0 : 1)
                    if isSaving {
                        LoadingDots(color: Theme.ink, dotSize: 7)
                            .transition(.opacity.combined(with: .scale(scale: 0.8)))
                    }
                }
            }
            // Disabled: ink on grey (white on #A9AFA6 is only 2.2:1), same as Add expense.
            .buttonStyle(stars > 0 ? .sqPrimary : .sqDisabled(Theme.mutedStar))
            .disabled(stars == 0 || isSaving)
            .animation(reduceMotion ? Motion.reduced : Motion.quick, value: stars > 0)
            .animation(reduceMotion ? Motion.reduced : Motion.standard, value: isSaving)
            .accessibilityLabel(stars > 0 ? "Save rating" : "Pick a star rating")
            .accessibilityValue(isSaving ? "Saving" : "")
        }
    }

    private var noteField: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text("Anything else? (optional)")
                .sqFont(12, relativeTo: .caption)
                .foregroundStyle(Theme.text3)
                .homeLine(12)
            TextField("", text: $note, prompt: Text("e.g. great music, but the line was long").foregroundStyle(Theme.text3), axis: .vertical)
                .lineLimit(2...5)
                .sqFont(15)
                .foregroundStyle(Theme.ink)
                .homeLine(15)
                .focused($noteFocused)
                .frame(minHeight: 2 * 15 * 1.35, alignment: .topLeading)
                .accessibilityLabel("Anything else? (optional)")
        }
        // 1pt border + the prototype's 10 / 12 padding.
        .padding(.vertical, 11)
        .padding(.horizontal, 13)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Theme.field, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        .overlay { RoundedRectangle(cornerRadius: 12, style: .continuous).strokeBorder(Theme.line, lineWidth: 1) }
        .contentShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
        .onTapGesture { noteFocused = true }
    }

    private func toggle(_ tag: String) {
        if let index = tags.firstIndex(of: tag) { tags.remove(at: index) } else { tags.append(tag) }
    }

    private func save() {
        guard stars > 0, !isSaving else { return }
        isSaving = true
        withMotion { errorMessage = nil }
        noteFocused = false
        let trimmed = note.trimmingCharacters(in: .whitespacesAndNewlines)
        let rating = Rating(stars: stars, tags: tags, note: trimmed.isEmpty ? nil : trimmed)
        Task {
            do {
                try await env.api.rate(itemId: event.id, rating: rating)
                saved(rating)
                close()
            } catch {
                withMotion(Motion.arrive) {
                    errorMessage = (error as? LocalizedError)?.errorDescription ?? "Couldn't save your rating. Try again."
                    isSaving = false
                }
            }
        }
    }
}

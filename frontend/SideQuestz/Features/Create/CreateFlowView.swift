import SwiftUI

/// Create flow (+ button, full screen): Where → When → Vibe → Review (GUI_PLAN.md §7.7).
/// Presented by `MainShell` while `router.createDraft` is set.
struct CreateFlowView: View {
    let draft: CreateDraft

    @Environment(AppEnvironment.self) private var env
    @State private var model: CreateFlowModel?

    var body: some View {
        ZStack {
            Theme.cream.ignoresSafeArea()
            if let model {
                CreateFlowScreen(model: model)
            }
        }
        .onAppear {
            if model == nil { model = CreateFlowModel(draft: draft, env: env) }
        }
    }
}

/// Text inputs in the flow (Return / keyboard handling).
enum CreateField: Hashable {
    case search, mood
}

/// Header · stepper · the current step (scrolling) · footer, plus the More options sheet.
private struct CreateFlowScreen: View {
    @Bindable var model: CreateFlowModel

    @Environment(AppEnvironment.self) private var env
    @Environment(Router.self) private var router
    @Environment(\.safeAreaBottom) private var safeBottom
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @FocusState private var focus: CreateField?

    var body: some View {
        VStack(spacing: 0) {
            header
            CreateStepper(step: model.step) { go(to: $0) }
                .frame(height: 68, alignment: .top)
            ZStack {
                ScrollView {
                    stepContent
                        .padding(.horizontal, Metrics.side)
                        .padding(.bottom, 20)
                }
                .scrollDismissesKeyboard(.interactively)
                .scrollDisabled(model.draggingStopId != nil)
                .id(model.step)
                .transition(.opacity)
            }
            .frame(maxHeight: .infinity)
            if focus == nil {
                footer
            }
        }
        .ignoresSafeArea(.container, edges: .bottom)
        .background(Theme.cream.ignoresSafeArea())
        .sqSheet(isPresented: $model.moreOpen, style: .cream(.fitted(max: 740))) {
            CreateMoreOptionsSheet(model: model) { step in
                model.moreOpen = false
                go(to: step)
            }
        }
        .onAppear(perform: start)
        .onChange(of: env.preferences) { model.applyPreferenceDefaults() }
        .onDisappear { env.voice.cancel() }
    }

    // MARK: Header

    /// "Cancel" · "New sidequest" in a 90 / flexible / 90 grid, 50pt from the top of the screen.
    private var header: some View {
        HStack(spacing: 0) {
            Button(action: cancel) {
                Text("Cancel")
                    .sqFont(17)
                    .foregroundStyle(Theme.sageInk)
                    .lineLimit(1)
                    .minimumScaleFactor(0.6)
                    .padding(.leading, 8)
                    .frame(width: 90, height: 44, alignment: .leading)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.sqPressable)
            Text("New sidequest")
                .sqFont(17, .semibold)
                .foregroundStyle(Theme.ink)
                .lineLimit(1)
                .minimumScaleFactor(0.6)
                .frame(maxWidth: .infinity)
                .accessibilityAddTraits(.isHeader)
            Color.clear.frame(width: 90, height: 1)
        }
        .frame(height: 44)
        .padding(.horizontal, 12)
        .designTopPadding(50, minimum: 3)
    }

    // MARK: Steps

    @ViewBuilder private var stepContent: some View {
        switch model.step {
        case 1: CreateWhereStep(model: model, focus: $focus)
        case 2: CreateWhenStep(model: model)
        case 3: CreateVibeStep(model: model, focus: $focus)
        default: CreateReviewStep(model: model)
        }
    }

    // MARK: Footer

    private var footer: some View {
        VStack(spacing: 10) {
            if model.step == 4, let error = model.createError {
                ErrorBox(messages: [error])
            }
            HStack(spacing: 10) {
                if model.step > 1 {
                    Button("Back") { go(to: model.step - 1) }
                        .buttonStyle(.sqSecondary)
                        .frame(width: 110)
                }
                primaryButton
            }
        }
        .padding(.top, 13)
        .padding(.horizontal, Metrics.side)
        .padding(.bottom, max(safeBottom - 3, 12))
        .background(Theme.cream)
        .overlay(alignment: .top) { RowDivider() }
    }

    @ViewBuilder private var primaryButton: some View {
        if model.step < 4 {
            Button("Next") { go(to: model.step + 1) }
                .buttonStyle(model.canGoNext ? .sqPrimary : .sqDisabled())
                .disabled(!model.canGoNext)
        } else {
            Button(action: startSidequest) {
                HStack(spacing: 8) {
                    if model.creating {
                        ProgressView().tint(Theme.ink)
                    }
                    Text("Start this sidequest")
                }
            }
            .buttonStyle(model.selectedOption != nil ? .sqPrimary : .sqDisabled())
            .disabled(!model.canStart)
        }
    }

    // MARK: Actions

    private func go(to step: Int) {
        focus = nil
        withAnimation(.easeInOut(duration: reduceMotion ? 0.15 : 0.2)) { model.go(to: step) }
    }

    private func cancel() {
        env.voice.cancel()
        router.createDraft = nil
    }

    private func startSidequest() {
        Task {
            guard let itinerary = await model.startSidequest() else { return }
            router.selectedItineraryId = itinerary.id
            router.homeSegment = .itineraries
            router.select(.home)
            router.createDraft = nil
        }
    }

    /// Demo deep links (`create/2/calendar`, `create/4/more`) + first loads.
    private func start() {
        if let parts = router.consumeLaunch("create") {
            switch parts.dropFirst().first {
            case "calendar":
                model.calendarOpen = true
            case "more":
                // Wait for the cover's own presentation to finish before stacking the sheet on it.
                Task {
                    try? await Task.sleep(nanoseconds: 700_000_000)
                    if model.options.value != nil { model.moreOpen = true } else { model.openMoreAfterLoad = true }
                }
            default:
                break
            }
        }
        Task { await model.loadDefaultPlacesIfNeeded() }
        if model.step == 4 {
            Task { await model.enterReview() }
        }
    }
}

// MARK: - Stepper

/// 4 circles (28pt) labeled Where · When · Vibe · Review with 2pt connectors. Done = sage + ink
/// check, current = sage + ink number + bold label, future = white + `mutedBorder` ring.
/// Tapping a step jumps to it.
private struct CreateStepper: View {
    let step: Int
    let select: (Int) -> Void

    private static let labels = ["Where", "When", "Vibe", "Review"]

    var body: some View {
        HStack(spacing: 0) {
            ForEach(1...4, id: \.self) { n in
                stepButton(n)
                if n < 4 {
                    Rectangle()
                        .fill(n < step ? Theme.sage : Theme.lineStrong)
                        .frame(height: 2)
                        .padding(.horizontal, -6)
                        .frame(maxWidth: .infinity)
                        .padding(.top, 13)
                        .frame(maxHeight: .infinity, alignment: .top)
                        .accessibilityHidden(true)
                }
            }
        }
        .fixedSize(horizontal: false, vertical: true)
        .padding(.horizontal, 28)
        .padding(.top, 10)
    }

    private func stepButton(_ n: Int) -> some View {
        let done = n < step
        let current = n == step
        let label = Self.labels[n - 1]
        return Button { select(n) } label: {
            VStack(spacing: 5) {
                ZStack {
                    Circle().fill(done || current ? Theme.sage : .white)
                    Circle().strokeBorder(done || current ? Theme.sage : Theme.mutedBorder, lineWidth: 1.5)
                    if done {
                        CheckGlyph(lineWidth: 2.6)
                            .foregroundStyle(Theme.ink)
                            .frame(width: 14, height: 14)
                    } else {
                        Text("\(n)")
                            .sqFont(13, .bold)
                            .foregroundStyle(current ? Theme.ink : Theme.text3)
                    }
                }
                .frame(width: 28, height: 28)
                Text(label)
                    .sqFont(12, current ? .bold : .medium, relativeTo: .caption)
                    .foregroundStyle(current ? Theme.ink : Theme.text3)
                    .lineLimit(1)
                    .fixedSize()
            }
            .frame(width: 48)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Step \(n): \(label)")
        .accessibilityValue(done ? "Done" : current ? "Current step" : "")
        .accessibilityAddTraits(current ? .isSelected : [])
    }
}

#Preview("Create · Where") {
    CreateFlowView(draft: CreateDraft())
        .environment(AppEnvironment.preview())
        .environment(Router(phase: .main))
}

#Preview("Create · Review") {
    CreateFlowView(draft: CreateDraft(step: 4))
        .environment(AppEnvironment.preview())
        .environment(Router(phase: .main))
}

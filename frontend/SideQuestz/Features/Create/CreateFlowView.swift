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
    @FocusState private var focus: CreateField?
    /// Which way the last step change went: steps slide in from that side.
    @State private var forward = true
    /// A step change waiting for the leaving step to pick up a new direction (see `go(to:)`).
    @State private var pendingStep: Int?

    var body: some View {
        VStack(spacing: 0) {
            header
            CreateStepper(step: model.step, forward: forward) { go(to: $0) }
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
                .sqTransition(.step(forward: forward))
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
        .onChange(of: forward) {
            // The leaving step now carries the new direction; move on in this next update.
            guard let target = pendingStep else { return }
            pendingStep = nil
            withMotion(Motion.gentle) { model.go(to: target) }
        }
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
                    .sqTransition(.rise)
            }
            HStack(spacing: 10) {
                if model.step > 1 {
                    Button("Back") { go(to: model.step - 1) }
                        .buttonStyle(.sqSecondary)
                        .frame(width: 110)
                        .transition(.opacity)
                }
                primaryButton
            }
        }
        .padding(.top, 13)
        .padding(.horizontal, Metrics.side)
        .padding(.bottom, max(safeBottom - 3, 12))
        .background(Theme.cream)
        .overlay(alignment: .top) { RowDivider() }
        .animation(Motion.standard, value: model.createError)
        // "Next" / "Start this sidequest" turn sage when they become available.
        .animation(Motion.quick, value: primaryEnabled)
    }

    private var primaryEnabled: Bool {
        model.step < 4 ? model.canGoNext : model.selectedOption != nil
    }

    @ViewBuilder private var primaryButton: some View {
        if model.step < 4 {
            Button("Next") { go(to: model.step + 1) }
                .buttonStyle(model.canGoNext ? .sqPrimary : .sqDisabled())
                .disabled(!model.canGoNext)
        } else {
            // While POST /itineraries runs the label steps aside for three dots (same size, same name).
            Button(action: startSidequest) {
                Text("Start this sidequest")
                    .opacity(model.creating ? 0 : 1)
                    .overlay {
                        if model.creating {
                            LoadingDots(color: Theme.ink, dotSize: 7)
                                .sqTransition(.pop)
                        }
                    }
                    .animation(Motion.quick, value: model.creating)
            }
            .buttonStyle(model.selectedOption != nil ? .sqPrimary : .sqDisabled())
            .disabled(!model.canStart)
            .accessibilityLabel("Start this sidequest")
            .accessibilityValue(model.creating ? "Loading" : "")
        }
    }

    // MARK: Actions

    /// Steps slide in from the side you're heading to. A direction change is applied one update
    /// before the step changes, so the leaving step also exits toward the correct side.
    private func go(to step: Int) {
        focus = nil
        let target = min(4, max(1, step))
        guard target != model.step else { return }
        let isForward = target > model.step
        if isForward == forward {
            withMotion(Motion.gentle) { model.go(to: target) }
        } else {
            pendingStep = target
            forward = isForward
        }
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
///
/// Motion: moving forward, the connector fills toward the next step and that circle fills (with a
/// small pop) as the line reaches it, while the finished step's check draws in. Moving back, the
/// line drains toward the step you return to.
private struct CreateStepper: View {
    let step: Int
    /// The last step change went forward.
    let forward: Bool
    let select: (Int) -> Void

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private static let labels = ["Where", "When", "Vibe", "Review"]

    var body: some View {
        HStack(spacing: 0) {
            ForEach(1...4, id: \.self) { n in
                stepButton(n)
                if n < 4 {
                    CreateStepConnector(filled: n < step)
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
                        AnimatedCheck(lineWidth: 2.6, delay: 0.08)
                            .foregroundStyle(Theme.ink)
                            .frame(width: 14, height: 14)
                            .transition(.opacity)
                    } else {
                        Text("\(n)")
                            .sqFont(13, .bold)
                            .foregroundStyle(current ? Theme.ink : Theme.text3)
                            .transition(.opacity)
                    }
                }
                .frame(width: 28, height: 28)
                .sqBounce(when: current, scale: 1.12)
                Text(label)
                    .sqFont(12, current ? .bold : .medium, relativeTo: .caption)
                    .foregroundStyle(current ? Theme.ink : Theme.text3)
                    .contentTransition(.interpolate)
                    .lineLimit(1)
                    .fixedSize()
            }
            .animation(circleAnimation(n), value: step)
            .frame(width: 48)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Step \(n): \(label)")
        .accessibilityValue(done ? "Done" : current ? "Current step" : "")
        .accessibilityAddTraits(current ? .isSelected : [])
    }

    /// Moving forward, the new current step fills as the connector reaches it; everything else
    /// changes right away.
    private func circleAnimation(_ n: Int) -> Animation {
        if reduceMotion { return Motion.reduced }
        return forward && n == step ? Motion.quick.delay(0.2) : Motion.quick
    }
}

/// A stepper connector: sage up to the fill point, `lineStrong` after it. The sage part grows from
/// the left (and drains back to it), animated by the step change's transaction. At rest it's a
/// single solid bar, exactly like a plain filled rectangle.
private struct CreateStepConnector: View {
    let filled: Bool

    var body: some View {
        GeometryReader { proxy in
            HStack(spacing: 0) {
                Rectangle()
                    .fill(Theme.sage)
                    .frame(width: filled ? proxy.size.width : 0)
                Rectangle()
                    .fill(Theme.lineStrong)
            }
        }
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

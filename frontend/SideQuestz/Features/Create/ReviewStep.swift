import SwiftUI

/// Create › Review ("Pick a sidequest"): option cards + "Load more options", the timed route for
/// the selected option (drag ☰ to reorder; hold a stop to swap or remove it; the server re-times
/// it), and "More options".
///
/// Loading and motion: while the planner works (as long as `POST /plans/generate` takes), ghost
/// option cards sit where the real ones will land, over the drawing logo and status lines; then
/// the cards arrive one after another and the route rises in. Selecting a card springs its ring
/// and cross-fades the route. "Load more options" shows dots in its tile and the new cards slide
/// in. After a reorder the legs shimmer until the new timing rolls into place, and "Transit times
/// updated" fades in, then out.
struct CreateReviewStep: View {
    @Bindable var model: CreateFlowModel
    @Environment(AppEnvironment.self) private var env
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    /// The route status on screen: "Transit times updated" fades out a few seconds after it shows
    /// ("Some stops would be late" stays a little longer; the route card keeps marking the stop).
    @State private var shownStatus: CreateTransitStatus = .idle

    private static let updatedLingerSeconds = 3.0
    private static let lateLingerSeconds = 5.0

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            VStack(alignment: .leading, spacing: 12) {
                CreateStepTitle("Pick a sidequest")
                CreateLiveText(text: summary, size: 14, color: Theme.text3)
            }
            // Loader ⇄ options ⇄ error cross-fade here (a VStack, so the content keeps an open height).
            VStack(alignment: .leading, spacing: 0) {
                switch model.options {
                case .loading:
                    CreatePlanLoader(lines: loadingLines)
                        .transition(.opacity)
                case .failed(let message):
                    ErrorStateView(message: message, minHeight: 300) {
                        Task { await model.regenerate() }
                    }
                    .padding(.top, 12)
                    .transition(.opacity)
                case .loaded:
                    // As edited here: swapped stops in their slots, removed ones gone.
                    loaded(model.optionList)
                        .transition(.opacity)
                }
            }
            moreOptionsRow
        }
        // Loader ⇄ options, and the option count, animate the whole step (the rows below ease too).
        .animation(Motion.standard, value: model.options.phase)
        .animation(Motion.standard, value: model.optionList.count)
        .onChange(of: model.transitStatus) { _, status in
            switch status {
            case .updated: AccessibilityNotification.Announcement("Transit times updated").post()
            case .updatedLate: AccessibilityNotification.Announcement("Some stops would be late").post()
            case .idle, .recalculating: break
            }
            shownStatus = status
        }
        .task(id: shownStatus) {
            let linger: Double
            switch shownStatus {
            case .updated: linger = Self.updatedLingerSeconds
            case .updatedLate: linger = Self.lateLingerSeconds
            case .idle, .recalculating: return
            }
            try? await Task.sleep(for: .seconds(linger))
            guard !Task.isCancelled else { return }
            shownStatus = .idle
        }
    }

    /// "Fri, Sep 25 · 2:10 PM–6:30 PM · 3 options" (the count rolls).
    private var summary: String {
        let window = "\(env.format.shortDate(model.date)) · \(env.format.fullRange(model.startTime, model.backBy))"
        guard let count = model.options.value?.count, count > 0 else { return window }
        return "\(window) · \(count) option\(count == 1 ? "" : "s")"
    }

    /// Honest, generic steps of what the planner does; they stay true with the real backend.
    private var loadingLines: [String] {
        let near = model.start.map { Self.shortName($0.name) } ?? "you"
        return ["Checking your free window…", "Finding spots near \(near)…",
                "Timing transit between stops…", "Ranking by what you like…"]
    }

    /// "Tech Square (current location)" → "Tech Square"; the phone's location → "you"; a dropped
    /// pin → "your start".
    private static func shortName(_ name: String) -> String {
        if name == "Dropped pin" { return "your start" }
        if name == "Current location" { return "you" }
        return name.components(separatedBy: " (").first ?? name
    }

    @ViewBuilder private func loaded(_ options: [PlanOption]) -> some View {
        if options.isEmpty {
            // The server says why when it can (nothing in the window, nothing fits, a bad request).
            EmptyStateView(message: model.emptyReason?.message ?? PlanEmptyReason.defaultMessage)
                .padding(.top, 12)
        } else {
            VStack(alignment: .leading, spacing: 0) {
                CreateOptionCarousel(model: model, options: options, loadMore: loadMoreState)
                if let option = model.selectedOption {
                    routeHeader
                        .sqAppear(2)
                    if let removal = model.lastRemoval, removal.optionId == option.id {
                        CreateUndoRow(removal: removal) {
                            withMotion { model.undoRemoval() }
                        } dismiss: {
                            withMotion { model.dismissRemoval(removal) }
                        }
                        .padding(.top, 8)
                        .sqTransition(.rise)
                    }
                    CreateRouteCard(model: model, option: option)
                        .padding(.top, 8)
                        .sqAppear(3)
                }
            }
        }
    }

    private var loadMoreState: CreateLoadMoreTile.Phase {
        if model.loadingMore { return .loading }
        if let error = model.loadMoreError { return .failed(error) }
        if model.noMoreOptions { return .done }
        return .idle
    }

    // MARK: Route

    private var routeHeader: some View {
        HStack(spacing: 8) {
            HStack(spacing: 4) {
                Text("Drag")
                DragHandleGlyph(width: 8.2, gap: 3.5, lineWidth: 1.4)
                    .frame(width: 14, height: 14)
                // The hold hint steps aside while a transit status needs the room.
                Text(shownStatus == .idle ? "to reorder · hold a stop to swap" : "to reorder")
                    .contentTransition(.opacity)
            }
            .sqFont(13)
            .foregroundStyle(Theme.text3)
            .lineLimit(1)
            .minimumScaleFactor(0.85)
            .createLine(13)
            .accessibilityElement(children: .ignore)
            .accessibilityLabel("Drag to reorder stops. Press and hold a stop to swap or remove it.")
            Spacer(minLength: 0)
            ZStack(alignment: .trailing) {
                switch shownStatus {
                case .idle:
                    EmptyView()
                case .recalculating:
                    Text("Recalculating transit…")
                        .sqFont(12, .semibold)
                        .foregroundStyle(Theme.text3)
                        .lineLimit(1)
                        .createPendingShimmer(true)
                        .transition(.opacity)
                case .updated:
                    HStack(spacing: 4) {
                        AnimatedCheck(lineWidth: 3, delay: 0.12)
                            .frame(width: 10, height: 10)
                        Text("Transit times updated")
                            .sqFont(12, .semibold)
                            .lineLimit(1)
                    }
                    .foregroundStyle(Theme.success)
                    .transition(reduceMotion ? .opacity : .opacity.combined(with: .offset(y: 4)))
                case .updatedLate:
                    // This order reaches a fixed start too late; the route card marks the stop.
                    Text("Some stops would be late")
                        .sqFont(12, .semibold)
                        .foregroundStyle(Theme.danger)
                        .lineLimit(1)
                        .transition(reduceMotion ? .opacity : .opacity.combined(with: .offset(y: 4)))
                }
            }
            .animation(Motion.standard, value: shownStatus)
        }
        .padding(.top, 14)
    }

    // MARK: More options

    private var moreOptionsRow: some View {
        Button { model.moreOpen = true } label: {
            HStack(spacing: 10) {
                CreateSlidersGlyph(size: 20)
                    .foregroundStyle(Theme.sageInk)
                Text("More options")
                    .sqFont(15, .semibold)
                    .foregroundStyle(Theme.ink)
                    .frame(maxWidth: .infinity, alignment: .leading)
                CreateChevron(direction: .right, size: 16)
                    .foregroundStyle(Theme.mutedStar)
            }
            .lineLimit(1)
            .padding(.horizontal, 14)
            .frame(height: 50)
            .background(.white, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
            .contentShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
        }
        .buttonStyle(.sqPressable)
        .padding(.top, 12)
        .accessibilityLabel("More options")
    }
}

// MARK: - Plan loader

/// Plan generation takes as long as the planner does. Ghost option cards
/// shimmer where the real ones will land; under them the logo draws and erases its S on a loop
/// over status lines that stay true whatever the backend is doing. VoiceOver reads "Loading" and
/// the current line.
private struct CreatePlanLoader: View {
    let lines: [String]

    var body: some View {
        VStack(spacing: 0) {
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(alignment: .top, spacing: 10) {
                    CreateGhostOptionCard(titleWidth: 150, lastLineWidth: 96)
                    CreateGhostOptionCard(titleWidth: 132, lastLineWidth: 120)
                }
                .padding(.horizontal, Metrics.side)
                .padding(.top, 4)
                .padding(.bottom, 6)
                .sqShimmer()
            }
            .scrollDisabled(true)
            .padding(.horizontal, -Metrics.side)
            .padding(.top, 12)
            .accessibilityHidden(true)
            VStack(spacing: 12) {
                LogoLoadingView(size: 44)
                CyclingStatusText(lines: lines, interval: .seconds(2))
                    .sqFont(14, .semibold)
                    .foregroundStyle(Theme.text2)
                    .multilineTextAlignment(.center)
                    .frame(maxWidth: .infinity)
            }
            .padding(.top, 30)
            .padding(.bottom, 22)
            .accessibilityElement(children: .combine)
        }
    }
}

/// A 240pt option card in outline: "OPTION A" + tag chip, the name, three lines of stops, meta.
private struct CreateGhostOptionCard: View {
    let titleWidth: CGFloat
    let lastLineWidth: CGFloat

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 6) {
                SkeletonBlock(width: 70, height: 10)
                Spacer(minLength: 0)
                SkeletonBlock(width: 80, height: 20, radius: 8)
            }
            SkeletonBlock(width: titleWidth, height: 16)
                .padding(.top, 12)
            VStack(alignment: .leading, spacing: 8) {
                SkeletonBlock(height: 11)
                SkeletonBlock(height: 11)
                SkeletonBlock(width: lastLineWidth, height: 11)
            }
            .padding(.top, 14)
            SkeletonBlock(width: 168, height: 10)
                .padding(.top, 14)
        }
        .padding(14)
        .padding(.vertical, 4)
        .frame(width: 240, alignment: .topLeading)
        .background(.white, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }
}

// MARK: - Options carousel

/// The options row. The batch on screen when it appears arrives card by card; cards added later
/// by "Load more options" slide in from the tile's side while the tile moves over.
private struct CreateOptionCarousel: View {
    @Bindable var model: CreateFlowModel
    let options: [PlanOption]
    let loadMore: CreateLoadMoreTile.Phase

    /// Option ids on screen when the carousel appeared.
    @State private var firstBatch: Set<String>
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    init(model: CreateFlowModel, options: [PlanOption], loadMore: CreateLoadMoreTile.Phase) {
        self.model = model
        self.options = options
        self.loadMore = loadMore
        _firstBatch = State(initialValue: Set(options.map(\.id)))
    }

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(alignment: .top, spacing: 10) {
                    ForEach(Array(options.enumerated()), id: \.element.id) { index, option in
                        card(option, index: index)
                    }
                    CreateLoadMoreTile(state: loadMore) {
                        Task { await model.loadMoreOptions() }
                    }
                    .frame(maxHeight: .infinity)
                    .sqAppear(min(options.count, 4))
                }
                .fixedSize(horizontal: false, vertical: true)
                .padding(.horizontal, Metrics.side)
                .padding(.top, 4)
                .padding(.bottom, 6)
                .animation(reduceMotion ? Motion.reduced : Motion.arrive, value: options.map(\.id))
            }
            .padding(.horizontal, -Metrics.side)
            .padding(.top, 12)
            .onChange(of: model.selectedOptionId) { _, id in
                guard let id else { return }
                // A just-loaded card needs one layout pass before it can be scrolled to.
                Task {
                    try? await Task.sleep(nanoseconds: 60_000_000)
                    withMotion(Motion.gentle) { proxy.scrollTo(id) }
                }
            }
        }
    }

    @ViewBuilder private func card(_ option: PlanOption, index: Int) -> some View {
        let card = CreateOptionCard(
            letter: Self.letter(index), option: option,
            stopsLine: model.orderedStops(option).map(\.title).joined(separator: " → "),
            isSelected: option.id == model.selectedOption?.id
        ) {
            withMotion { model.select(option.id) }
        }
        .frame(maxHeight: .infinity)
        .id(option.id)
        if firstBatch.contains(option.id) {
            card.sqAppear(index)
        } else {
            card.sqTransition(.step(forward: true))
        }
    }

    private static func letter(_ index: Int) -> String {
        guard index < 26, let scalar = UnicodeScalar(65 + index) else { return "\(index + 1)" }
        return String(Character(scalar))
    }
}

// MARK: - Option card

/// 240pt option card: "OPTION A" + tag chip (none when the server sent no tag), name, stops in the
/// current order, meta, and a "Tight timing" chip when the planner flagged a fixed start it would
/// just miss (`lateFlag`).
/// Selected: 2pt sage ring (springs in, and the card gives a small pop); others 1pt `line`.
private struct CreateOptionCard: View {
    let letter: String
    let option: PlanOption
    let stopsLine: String
    let isSelected: Bool
    let action: () -> Void

    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        let ring: CGFloat = isSelected ? 2 : 1
        Button(action: action) {
            VStack(alignment: .leading, spacing: 6) {
                HStack(spacing: 6) {
                    Text("OPTION \(letter)")
                        .sqFont(12, .bold, relativeTo: .caption)
                        .tracking(0.3)
                        .foregroundStyle(Theme.sageInk)
                        .createLine(12, relativeTo: .caption)
                        // Large text: shrink rather than break "OPTION A" mid-word.
                        .lineLimit(1)
                        .minimumScaleFactor(0.6)
                    Spacer(minLength: 0)
                    if !option.tag.isEmpty {
                        TagLabel(text: option.tag, fill: Theme.sageTint, foreground: Theme.sageInk,
                                 fontSize: 12, horizontalPadding: 8, verticalPadding: 3, radius: 8)
                            .lineLimit(1)
                            .minimumScaleFactor(0.6)
                    }
                }
                CreateWrapText(text: option.name, face: .system(.bold), size: 17)
                CreateWrapText(text: stopsLine, size: 13, color: Theme.text2)
                CreateWrapText(text: option.meta, size: 12, textStyle: .caption1, color: Theme.text3)
                if option.lateFlag {
                    TagLabel(text: "Tight timing", fill: Theme.dangerBg, foreground: Theme.dangerText,
                             fontSize: 12, horizontalPadding: 8, verticalPadding: 3, radius: 8)
                        .padding(.top, 2)
                }
            }
            .accessibilityHidden(true)
            .padding(14)
            .frame(width: 240, alignment: .topLeading)
            .frame(maxHeight: .infinity, alignment: .top)
            .background(.white, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
            .background {
                RoundedRectangle(cornerRadius: 16 + ring, style: .continuous)
                    .fill(isSelected ? Theme.sage : Theme.line)
                    .padding(-ring)
                    .animation(reduceMotion ? Motion.reduced : .spring(duration: 0.4, bounce: 0.5), value: isSelected)
            }
            .contentShape(RoundedRectangle(cornerRadius: 16, style: .continuous))
        }
        .buttonStyle(.sqPressable)
        .sqBounce(when: isSelected, scale: 1.025)
        .accessibilityLabel(accessibilityText)
        .accessibilityAddTraits(isSelected ? .isSelected : [])
        .sensoryFeedback(.selection, trigger: isSelected) { _, new in new }
    }

    /// "Option E, Social, tight timing: Games + views. A → B → C. ~$ · 1.4 mi walking · 2 transit legs"
    private var accessibilityText: String {
        var heading = ["Option \(letter)"]
        if !option.tag.isEmpty { heading.append(option.tag) }
        if option.lateFlag { heading.append("tight timing") }
        let lines = [option.name, stopsLine, option.meta].filter { !$0.isEmpty }
        return "\(heading.joined(separator: ", ")): \(lines.joined(separator: ". "))"
    }
}

// MARK: - Load more

/// Dashed 150pt tile at the end of the options row. While the next batch loads its icon becomes
/// three dots and the text cross-fades to "Finding more…". How many more the server has isn't
/// known, so the tile never promises a number.
private struct CreateLoadMoreTile: View {
    enum Phase: Equatable {
        case idle, loading, done
        case failed(String)
    }

    let state: Phase
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            VStack(spacing: 8) {
                ZStack {
                    Circle().fill(state == .done ? Theme.line : Theme.sage)
                    if state == .loading {
                        LoadingDots(color: Theme.ink, dotSize: 5)
                            .sqTransition(.pop)
                    } else {
                        CreateRefreshGlyph(size: 20)
                            .foregroundStyle(Theme.ink)
                            .transition(.opacity)
                    }
                }
                .frame(width: 40, height: 40)
                CreateCrossfade(value: title) {
                    CreateWrapText(text: title, face: .system(.semibold), size: 14, alignment: .center)
                }
                if let subtitle {
                    CreateCrossfade(value: subtitle) {
                        CreateWrapText(text: subtitle, size: 12, textStyle: .caption1, color: Theme.text3, alignment: .center)
                    }
                    .transition(.opacity)
                }
            }
            .accessibilityHidden(true)
            .padding(14)
            .frame(width: 150)
            .frame(maxHeight: .infinity)
            .overlay {
                RoundedRectangle(cornerRadius: 16, style: .continuous)
                    .strokeBorder(Theme.mutedStar, style: StrokeStyle(lineWidth: 1.5, dash: [4.5, 3]))
            }
            .contentShape(RoundedRectangle(cornerRadius: 16, style: .continuous))
        }
        .buttonStyle(.sqPressable)
        .disabled(state == .loading || state == .done)
        .accessibilityLabel(state == .done ? "No more options right now" : title)
        .accessibilityHint(subtitle ?? "")
        .accessibilityValue(state == .loading ? "Loading" : "")
        .animation(Motion.standard, value: state)
    }

    private var title: String {
        switch state {
        case .idle: "Load more options"
        case .loading: "Finding more…"
        case .done: "No more right now"
        case .failed: "Try again"
        }
    }

    /// Only when there's something to say beyond the title.
    private var subtitle: String? {
        switch state {
        case .idle, .loading: nil
        case .done: "Try changing filters in More options"
        case .failed(let message): message
        }
    }
}

// MARK: - Undo a removal

/// "Removed Krog Street Market · Undo" above the route, for a few seconds after a removal.
private struct CreateUndoRow: View {
    let removal: CreateStopRemoval
    let undo: () -> Void
    let dismiss: () -> Void

    private static let seconds = 5.0

    var body: some View {
        HStack(spacing: 10) {
            Text("Removed \(removal.stop.title)")
                .sqFont(14)
                .foregroundStyle(Theme.text2)
                .lineLimit(1)
                .truncationMode(.middle)
                .frame(maxWidth: .infinity, alignment: .leading)
            Button("Undo", action: undo)
                .buttonStyle(.sqLink(size: 14))
                .frame(minHeight: Metrics.minTouch)
        }
        .padding(.leading, 14)
        .padding(.trailing, 8)
        .frame(minHeight: 44)
        .background(.white, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        .accessibilityElement(children: .combine)
        .onAppear {
            AccessibilityNotification.Announcement("Removed \(removal.stop.title)").post()
        }
        .task(id: removal) {
            try? await Task.sleep(for: .seconds(Self.seconds))
            guard !Task.isCancelled else { return }
            dismiss()
        }
    }
}

import SwiftUI

/// Create › Review ("Pick a sidequest"): option cards + "Load more options", the timed route for
/// the selected option (drag ☰ to reorder; the server re-times it), and "More options".
struct CreateReviewStep: View {
    @Bindable var model: CreateFlowModel
    @Environment(AppEnvironment.self) private var env

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            VStack(alignment: .leading, spacing: 12) {
                CreateStepTitle("Pick a sidequest")
                CreateWrapText(text: summary, size: 14, color: Theme.text3)
            }
            switch model.options {
            case .loading:
                LoadingStateView(minHeight: 300)
                    .padding(.top, 12)
            case .failed(let message):
                ErrorStateView(message: message, minHeight: 300) {
                    Task { await model.regenerate() }
                }
                .padding(.top, 12)
            case .loaded(let options):
                if options.isEmpty {
                    EmptyStateView(message: "No options fit this window. Try changing filters in More options.")
                        .padding(.top, 12)
                } else {
                    carousel(options)
                    if let option = model.selectedOption {
                        routeHeader
                        CreateRouteCard(model: model, option: option)
                            .padding(.top, 8)
                    }
                }
            }
            moreOptionsRow
        }
        .onChange(of: model.transitStatus) { _, status in
            if status == .updated {
                AccessibilityNotification.Announcement("Transit times updated").post()
            }
        }
    }

    /// "Fri, Sep 25 · 2:10 PM–6:30 PM · 3 options · drag stops to reorder"
    private var summary: String {
        let window = "\(env.format.shortDate(model.date)) · \(env.format.fullRange(model.startTime, model.backBy))"
        guard let count = model.options.value?.count, count > 0 else { return window }
        return "\(window) · \(count) option\(count == 1 ? "" : "s") · drag stops to reorder"
    }

    // MARK: Options

    private func carousel(_ options: [PlanOption]) -> some View {
        ScrollViewReader { proxy in
            ScrollView(.horizontal, showsIndicators: false) {
                HStack(alignment: .top, spacing: 10) {
                    ForEach(Array(options.enumerated()), id: \.element.id) { index, option in
                        CreateOptionCard(
                            letter: Self.letter(index), option: option,
                            stopsLine: model.orderedStops(option).map(\.title).joined(separator: " → "),
                            isSelected: option.id == model.selectedOption?.id
                        ) {
                            withAnimation(.easeOut(duration: 0.15)) { model.select(option.id) }
                        }
                        .frame(maxHeight: .infinity)
                        .id(option.id)
                    }
                    CreateLoadMoreTile(state: loadMoreState) {
                        Task { await model.loadMoreOptions() }
                    }
                    .frame(maxHeight: .infinity)
                }
                .fixedSize(horizontal: false, vertical: true)
                .padding(.horizontal, Metrics.side)
                .padding(.top, 4)
                .padding(.bottom, 6)
            }
            .padding(.horizontal, -Metrics.side)
            .padding(.top, 12)
            .onChange(of: model.selectedOptionId) { _, id in
                guard let id else { return }
                // A just-loaded card needs one layout pass before it can be scrolled to.
                Task {
                    try? await Task.sleep(nanoseconds: 60_000_000)
                    withAnimation(.easeInOut(duration: 0.3)) { proxy.scrollTo(id) }
                }
            }
        }
    }

    private static func letter(_ index: Int) -> String {
        guard index < 26, let scalar = UnicodeScalar(65 + index) else { return "\(index + 1)" }
        return String(Character(scalar))
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
                Text("to reorder stops")
            }
            .sqFont(13)
            .foregroundStyle(Theme.text3)
            .createLine(13)
            .accessibilityElement(children: .ignore)
            .accessibilityLabel("Drag to reorder stops")
            Spacer(minLength: 0)
            Text(statusText)
                .sqFont(12, .semibold)
                .foregroundStyle(model.transitStatus == .updated ? Theme.success : Theme.text3)
                .lineLimit(1)
                .animation(.easeOut(duration: 0.2), value: model.transitStatus)
        }
        .padding(.top, 14)
    }

    private var statusText: String {
        switch model.transitStatus {
        case .idle: ""
        case .recalculating: "Recalculating transit…"
        case .updated: "Transit times updated"
        }
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
                Text("Check your settings")
                    .sqFont(13, .medium)
                    .foregroundStyle(Theme.text3)
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
        .accessibilityLabel("More options. Check your settings")
    }
}

// MARK: - Option card

/// 240pt option card: "OPTION A" + tag chip, name, stops in the current order, meta.
/// Selected: 2pt sage ring; others 1pt `line`.
private struct CreateOptionCard: View {
    let letter: String
    let option: PlanOption
    let stopsLine: String
    let isSelected: Bool
    let action: () -> Void

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
                    TagLabel(text: option.tag, fill: Theme.sageTint, foreground: Theme.sageInk,
                             fontSize: 12, horizontalPadding: 8, verticalPadding: 3, radius: 8)
                        .lineLimit(1)
                        .minimumScaleFactor(0.6)
                }
                CreateWrapText(text: option.name, face: .system(.bold), size: 17)
                CreateWrapText(text: stopsLine, size: 13, color: Theme.text2)
                CreateWrapText(text: option.meta, size: 12, textStyle: .caption1, color: Theme.text3)
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
            }
            .contentShape(RoundedRectangle(cornerRadius: 16, style: .continuous))
        }
        .buttonStyle(.sqPressable)
        .accessibilityLabel("Option \(letter), \(option.tag): \(option.name). \(stopsLine). \(option.meta)")
        .accessibilityAddTraits(isSelected ? .isSelected : [])
        .sensoryFeedback(.selection, trigger: isSelected) { _, new in new }
    }
}

// MARK: - Load more

/// Dashed 150pt tile at the end of the options row.
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
                        Text("…")
                            .font(.mono(14, .bold, relativeTo: .body))
                            .foregroundStyle(Theme.ink)
                    } else {
                        CreateRefreshGlyph(size: 20)
                            .foregroundStyle(Theme.ink)
                    }
                }
                .frame(width: 40, height: 40)
                CreateWrapText(text: title, face: .system(.semibold), size: 14, alignment: .center)
                CreateWrapText(text: subtitle, size: 12, textStyle: .caption1, color: Theme.text3, alignment: .center)
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
        .accessibilityHint(subtitle)
    }

    private var title: String {
        switch state {
        case .idle: "Load more options"
        case .loading: "Finding more…"
        case .done: "No more right now"
        case .failed: "Try again"
        }
    }

    private var subtitle: String {
        switch state {
        case .idle: "2 more that fit your window"
        case .loading: "Checking what fits your window"
        case .done: "Try changing filters in More options"
        case .failed(let message): message
        }
    }
}

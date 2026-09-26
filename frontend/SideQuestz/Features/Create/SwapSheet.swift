import SwiftUI

/// Review › hold a stop › "Swap for something similar": the server's similar alternatives for one
/// stop (`POST /plans/alternatives`). Tapping one puts it in the stop's place and closes the sheet;
/// the route re-times behind it like after a drag.
///
/// Loading: three rows shaped like the real ones shimmer. Failure: a sentence and "Try again".
struct CreateSwapSheet: View {
    let model: CreateFlowModel
    let target: CreateSwapTarget
    let close: () -> Void

    @Environment(AppEnvironment.self) private var env
    @State private var alternatives: Loadable<[PlanAlternative]> = .loading

    static let style = SQSheetStyle(height: .fitted(max: 640))

    var body: some View {
        SheetScaffold(spacing: 14, shrinksToFit: true) {
            header
            ScrollView {
                content
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.bottom, 4)
            }
            .scrollBounceBehavior(.basedOnSize)
            .sheetShrinks()
        }
        .animation(Motion.standard, value: alternatives.phase)
        .task { await load() }
    }

    // MARK: Header

    private var header: some View {
        HStack(alignment: .top, spacing: 12) {
            VStack(alignment: .leading, spacing: 1) {
                Text("Swap \(target.stop.title)")
                    .sqFont(20, .bold)
                    .foregroundStyle(Theme.ink)
                    .fixedSize(horizontal: false, vertical: true)
                    .accessibilityAddTraits(.isHeader)
                Text(subtitle)
                    .sqFont(13)
                    .foregroundStyle(Theme.text3)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            CloseCircleButton(action: close)
                .padding(.top, -6)
                .padding(.trailing, -6)
        }
    }

    /// "Similar spots for 2:24–3:44 PM", or without a time while the route is re-timing.
    private var subtitle: String {
        if let slot = model.timeSlot(of: target.stop.id, in: target.optionId) {
            return "Similar spots for \(env.format.fullRange(slot.start, slot.end))"
        }
        return "Similar spots near your route"
    }

    // MARK: Content

    @ViewBuilder private var content: some View {
        switch alternatives {
        case .loading:
            card {
                ForEach(0..<3, id: \.self) { index in
                    if index > 0 { RowDivider(color: .white) }
                    CreateSwapSkeletonRow(titleWidth: [150, 124, 168][index])
                }
            }
            .sqShimmer()
            .accessibilityElement()
            .accessibilityLabel("Finding similar spots")
            .transition(.opacity)
        case .failed(let message):
            ErrorStateView(message: message, minHeight: 200) {
                Task { await load() }
            }
            .transition(.opacity)
        case .loaded(let list):
            if list.isEmpty {
                EmptyStateView(message: "Nothing similar nearby right now.", minHeight: 140)
                    .transition(.opacity)
            } else {
                card {
                    ForEach(Array(list.enumerated()), id: \.element.id) { index, alternative in
                        if index > 0 { RowDivider(color: .white) }
                        row(alternative)
                            .sqAppear(index)
                    }
                }
                .transition(.opacity)
            }
        }
    }

    private func card<Content: View>(@ViewBuilder _ content: () -> Content) -> some View {
        VStack(spacing: 0) { content() }
            .background(Theme.cream, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
    }

    /// "Jackson Street Bridge / Skyline views · Free / Also great views · 0.4 mi away ··· Swap"
    private func row(_ alternative: PlanAlternative) -> some View {
        Button {
            choose(alternative)
        } label: {
            HStack(spacing: 12) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(alternative.stop.title)
                        .sqFont(15, .semibold)
                        .foregroundStyle(Theme.ink)
                    Text(alternative.stop.subtitle)
                        .sqFont(12)
                        .foregroundStyle(Theme.text3)
                    if !alternative.reason.isEmpty {
                        Text(alternative.reason)
                            .sqFont(12, .semibold)
                            .foregroundStyle(Theme.sageInk)
                    }
                }
                .fixedSize(horizontal: false, vertical: true)
                .frame(maxWidth: .infinity, alignment: .leading)
                Text("Swap")
                    .sqFont(13, .semibold)
                    .foregroundStyle(Theme.ink)
                    .padding(.horizontal, 14)
                    .frame(height: 32)
                    .background(Theme.sage, in: Capsule())
            }
            .padding(.vertical, 12)
            .padding(.horizontal, 14)
            .contentShape(Rectangle())
        }
        .buttonStyle(.sqPressable)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel([alternative.stop.title, alternative.stop.subtitle, alternative.reason]
            .filter { !$0.isEmpty }.joined(separator: ", "))
        .accessibilityHint("Swaps it in for \(target.stop.title)")
        .accessibilityAddTraits(.isButton)
    }

    // MARK: Actions

    private func load() async {
        if alternatives.value == nil { withMotion { alternatives = .loading } }
        let order = model.routes[target.optionId]?.order ?? []
        let result = await Loadable.run {
            try await env.api.stopAlternatives(optionId: target.optionId, stopId: target.stop.id, stopOrder: order)
        }
        withMotion { alternatives = result }
    }

    private func choose(_ alternative: PlanAlternative) {
        close()
        withMotion { model.swapStop(target.stop.id, with: alternative.stop, in: target.optionId) }
        AccessibilityNotification.Announcement("Swapped in \(alternative.stop.title)").post()
    }
}

/// A row shaped like an alternative: title, subtitle, reason, and the Swap pill.
private struct CreateSwapSkeletonRow: View {
    let titleWidth: CGFloat

    var body: some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 7) {
                SkeletonBlock(width: titleWidth, height: 13, color: Theme.skeletonOnCream)
                SkeletonBlock(width: 110, height: 10, color: Theme.skeletonOnCream)
                SkeletonBlock(width: 150, height: 10, color: Theme.skeletonOnCream)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            SkeletonBlock(width: 62, height: 32, radius: 16, color: Theme.skeletonOnCream)
        }
        .padding(.vertical, 12)
        .padding(.horizontal, 14)
    }
}

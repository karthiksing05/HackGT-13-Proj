import SwiftUI

/// Home › Itineraries (GUI_PLAN.md §7.5a): the "past events to rate" card, the itinerary cards,
/// and the swipeable timeline carousel.
struct HomeItinerariesView: View {
    @Bindable var store: HomeStore
    /// Full width of the Home screen (cards are this minus the 20pt side padding on each side).
    let pageWidth: CGFloat
    let openBlock: (ItineraryItem, Itinerary) -> Void
    let retry: () -> Void

    @Environment(Router.self) private var router
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if !store.toRate.isEmpty {
                rateCard
                    .padding(.horizontal, Metrics.side)
                    .padding(.top, 12)
            }
            SectionHeader(title: "Active itineraries")
                .padding(.horizontal, Metrics.side)
                .padding(.top, 20)
                .padding(.bottom, 10)
            LoadableView(state: store.itineraries, retry: retry) { list in
                content(list)
            }
        }
    }

    @ViewBuilder
    private func content(_ list: [Itinerary]) -> some View {
        cardsRow(list)
        if list.isEmpty {
            EmptyStateView(message: "No active itineraries yet. Plan one and its timeline shows up here.")
        } else {
            timelineHeader(list)
            carousel(list)
            Text("Swipe to switch itineraries · tap a block to open it")
                .sqFont(12, relativeTo: .caption)
                .foregroundStyle(Theme.text3)
                .homeLine(12)
                .multilineTextAlignment(.center)
                .frame(maxWidth: .infinity)
                .padding(.horizontal, Metrics.side)
                .padding(.top, 6)
                .padding(.bottom, 24)
        }
    }

    private func selectedId(in list: [Itinerary]) -> String? {
        store.selectedItineraryId ?? list.first?.id
    }

    /// Tapping a card selects it and scrolls the carousel to its timeline.
    private func select(_ id: String) {
        withAnimation(reduceMotion ? nil : .smooth(duration: 0.35)) {
            store.selectedItineraryId = id
        }
    }

    // MARK: Rate card

    private var rateCard: some View {
        let count = store.toRate.count
        let title = count == 1 ? "1 past event to rate" : "\(count) past events to rate"
        return Button {
            router.homeSegment = .past
        } label: {
            HStack(spacing: 12) {
                StarShape()
                    .stroke(Theme.sageInk, style: StrokeStyle(lineWidth: 1.8 * 22 / 24, lineJoin: .round))
                    .frame(width: 22, height: 22)
                    .frame(width: 40, height: 40)
                    .background(Theme.sageTint, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
                VStack(alignment: .leading, spacing: 0) {
                    Text(title).sqFont(15, .semibold).foregroundStyle(Theme.ink).homeLine(15)
                    Text("Ratings tune what we suggest next").sqFont(13).foregroundStyle(Theme.text3).homeLine(13)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                Text("Rate").sqFont(14, .semibold).foregroundStyle(Theme.sageInk)
            }
            .padding(.vertical, 12)
            .padding(.horizontal, 14)
            .background(.white, in: RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
        }
        .buttonStyle(.sqPressable)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(title). Ratings tune what we suggest next")
        .accessibilityHint("Opens Past to rate them")
        .accessibilityAddTraits(.isButton)
    }

    // MARK: Itinerary cards

    private func cardsRow(_ list: [Itinerary]) -> some View {
        let selected = selectedId(in: list)
        return ScrollViewReader { proxy in
            ScrollView(.horizontal) {
                HStack(spacing: 12) {
                    ForEach(list) { itinerary in
                        HomeItineraryCard(itinerary: itinerary, isSelected: itinerary.id == selected) {
                            select(itinerary.id)
                        }
                        .id(itinerary.id)
                    }
                    newSidequestTile
                }
                .fixedSize(horizontal: false, vertical: true)
                .padding(.horizontal, Metrics.side)
                .padding(.top, 4)
                .padding(.bottom, 6)
            }
            .scrollIndicators(.hidden)
            .sensoryFeedback(.selection, trigger: store.selectedItineraryId)
            .onChange(of: store.selectedItineraryId) { _, id in
                // Keep the selected card in view when the timeline is swiped.
                guard let id else { return }
                withAnimation(reduceMotion ? nil : .smooth(duration: 0.3)) { proxy.scrollTo(id, anchor: .center) }
            }
        }
    }

    private var newSidequestTile: some View {
        Button {
            router.openCreate()
        } label: {
            VStack(spacing: 6) {
                PlusGlyph(length: 14, lineWidth: 1.8)
                    .frame(width: 24, height: 24)
                Text("New sidequest")
                    .sqFont(14, .semibold)
                    .homeLine(14)
                    .lineLimit(2)
                    .multilineTextAlignment(.center)
            }
            .foregroundStyle(Theme.text2)
            .frame(width: 120)
            .frame(maxHeight: .infinity)
            .background {
                RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous)
                    .strokeBorder(Theme.mutedStar, style: StrokeStyle(lineWidth: 1.5, dash: [4.5, 4.5]))
            }
            .contentShape(RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
        }
        .buttonStyle(.sqPressable)
        .accessibilityLabel("New sidequest")
    }

    // MARK: Timeline carousel

    private func timelineHeader(_ list: [Itinerary]) -> some View {
        let selected = selectedId(in: list)
        let index = list.firstIndex { $0.id == selected } ?? 0
        return HStack(alignment: .firstTextBaseline, spacing: 8) {
            Text(list[index].title)
                .sectionStyle()
                .lineLimit(1)
                .minimumScaleFactor(0.8)
                .accessibilityAddTraits(.isHeader)
            Spacer(minLength: 0)
            HStack(spacing: 5) {
                ForEach(list) { itinerary in
                    Circle()
                        .fill(itinerary.id == selected ? Theme.sage : Theme.mutedBorder)
                        .frame(width: 7, height: 7)
                }
            }
            .alignmentGuide(.firstTextBaseline) { $0[.bottom] }
            .accessibilityElement()
            .accessibilityLabel("Itinerary \(index + 1) of \(list.count)")
        }
        .padding(.horizontal, Metrics.side)
        .padding(.top, 20)
        .padding(.bottom, 10)
    }

    private func carousel(_ list: [Itinerary]) -> some View {
        ScrollView(.horizontal) {
            HStack(alignment: .top, spacing: 12) {
                ForEach(list) { itinerary in
                    HomeTimelineCard(itinerary: itinerary) { openBlock($0, itinerary) }
                        .frame(width: max(0, pageWidth - 2 * Metrics.side))
                        .id(itinerary.id)
                }
            }
            .scrollTargetLayout()
        }
        .contentMargins(.horizontal, Metrics.side, for: .scrollContent)
        .scrollTargetBehavior(.viewAligned)
        .scrollPosition(id: $store.selectedItineraryId)
        .scrollIndicators(.hidden)
        .padding(.bottom, 8)
    }
}

/// 212pt itinerary card: title, "Today · 1–8 PM", one 6pt bar per block, "3 stops · 3 going".
struct HomeItineraryCard: View {
    let itinerary: Itinerary
    let isSelected: Bool
    let action: () -> Void
    @Environment(AppEnvironment.self) private var env

    var body: some View {
        let shape = RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous)
        Button(action: action) {
            VStack(alignment: .leading, spacing: 8) {
                Text(itinerary.title)
                    .sqFont(16, .bold)
                    .homeLine(16)
                    .lineLimit(2)
                Text(when)
                    .sqFont(13)
                    .foregroundStyle(Theme.text3)
                    .homeLine(13)
                HStack(spacing: 4) {
                    ForEach(itinerary.items) { item in
                        Capsule().fill(item.kind.palette.border).frame(height: 6)
                    }
                }
                Text(summary)
                    .sqFont(13, .semibold)
                    .homeLine(13)
            }
            .foregroundStyle(Theme.ink)
            .padding(14)
            .frame(width: 212, alignment: .topLeading)
            .frame(maxHeight: .infinity, alignment: .top)
            .background(.white, in: shape)
            .overlay {
                // box-shadow ring outside the card: 2pt sage when selected, 1pt `line` otherwise.
                let width: CGFloat = isSelected ? 2 : 1
                RoundedRectangle(cornerRadius: Metrics.cardRadius + width, style: .continuous)
                    .strokeBorder(isSelected ? Theme.sage : Theme.line, lineWidth: width)
                    .padding(-width)
            }
            .contentShape(shape)
        }
        .buttonStyle(.sqPressable)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(itinerary.title), \(when), \(summary)")
        .accessibilityHint("Shows its timeline")
        .accessibilityAddTraits(isSelected ? [.isButton, .isSelected] : .isButton)
    }

    private var when: String {
        "\(env.format.relativeDay(itinerary.date)) · \(env.format.compactRange(itinerary.start, itinerary.backBy))"
    }

    private var summary: String {
        let stops = itinerary.stopCount
        return "\(stops) \(stops == 1 ? "stop" : "stops") · \(itinerary.peopleLabel)"
    }
}

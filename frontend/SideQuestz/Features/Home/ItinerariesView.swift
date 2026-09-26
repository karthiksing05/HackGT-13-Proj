import SwiftUI

/// Home › Itineraries (GUI_PLAN.md §7.5a): the "past events to rate" card, the itinerary cards,
/// and the swipeable timeline carousel.
///
/// Motion: a skeleton while the first load runs, then cards and timeline blocks arrive one after
/// another; switching itineraries slides the header title and the active page dot; a plan you just
/// made pops its card; new or removed itineraries animate in and out.
struct HomeItinerariesView: View {
    @Bindable var store: HomeStore
    /// Full width of the Home screen (cards are this minus the 20pt side padding on each side).
    let pageWidth: CGFloat
    let openBlock: (ItineraryItem, Itinerary) -> Void
    let retry: () -> Void

    @Environment(Router.self) private var router
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    /// Cards and blocks arrive one after another only when they replace the skeleton.
    @State private var arrival = HomeArrivalWindow()
    /// The page the header showed last, so a new title slides in from the side you swiped toward.
    @State private var headerIndex = 0

    init(store: HomeStore, pageWidth: CGFloat, openBlock: @escaping (ItineraryItem, Itinerary) -> Void, retry: @escaping () -> Void) {
        self.store = store
        self.pageWidth = pageWidth
        self.openBlock = openBlock
        self.retry = retry
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            if !store.toRate.isEmpty {
                rateCard
                    .padding(.horizontal, Metrics.side)
                    .padding(.top, 12)
                    .homeTransition(.homeDrop)
            }
            SectionHeader(title: "Active itineraries")
                .padding(.horizontal, Metrics.side)
                .padding(.top, 20)
                .padding(.bottom, 10)
            HomeLoadable(state: store.itineraries, retry: retry) {
                HomeItinerariesSkeleton()
            } content: { list in
                content(list)
            }
            // A plan you just made is on its way: keep these on screen, dimmed, until it lands.
            .sqRefreshing(store.awaitsNewItinerary && store.itineraries.value != nil)
        }
        .onAppear { arrival.begin(loading: store.itineraries.isLoading) }
        .onChange(of: store.itineraries.phase) { _, phase in arrival.update(phase) }
    }

    @ViewBuilder
    private func content(_ list: [Itinerary]) -> some View {
        cardsRow(list)
        if list.isEmpty {
            EmptyStateView(message: "No active itineraries yet. Plan one and its timeline shows up here.")
                .transition(.opacity)
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
                    // The count rolls when a rating lands.
                    Text(title).sqFont(15, .semibold).foregroundStyle(Theme.ink).homeLine(15).sqNumeric()
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
        let arrives = arrival.isOpen
        return ScrollViewReader { proxy in
            ScrollView(.horizontal) {
                HStack(spacing: 12) {
                    ForEach(Array(list.enumerated()), id: \.element.id) { index, itinerary in
                        HomeItineraryCard(itinerary: itinerary, isSelected: itinerary.id == selected,
                                          highlight: store.cardHighlights[itinerary.id]) {
                            select(itinerary.id)
                        }
                        .homeArrival(index, enabled: arrives)
                        .sqTransition(.pop)
                        .id(itinerary.id)
                    }
                    newSidequestTile
                        .homeArrival(list.count, enabled: arrives)
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
        let itinerary = list[index]
        return HStack(alignment: .firstTextBaseline, spacing: 8) {
            // An invisible copy lays the row out (and keeps its baseline for the dots); the visible
            // title swaps on top of it, sliding in from the side you swiped toward.
            Text(itinerary.title)
                .sectionStyle()
                .lineLimit(1)
                .minimumScaleFactor(0.8)
                .hidden()
                .overlay(alignment: .leading) {
                    ZStack(alignment: .leading) {
                        Text(itinerary.title)
                            .sectionStyle()
                            .lineLimit(1)
                            .minimumScaleFactor(0.8)
                            .accessibilityAddTraits(.isHeader)
                            .id(itinerary.id)
                            .homeTransition(.homeSlide(forward: index >= headerIndex))
                    }
                }
            Spacer(minLength: 0)
            HomePageDots(count: list.count, index: index)
                .alignmentGuide(.firstTextBaseline) { $0[.bottom] }
        }
        .padding(.horizontal, Metrics.side)
        .padding(.top, 20)
        .padding(.bottom, 10)
        .animation(reduceMotion ? Motion.reduced : Motion.standard, value: index)
        .onChange(of: index) { _, new in headerIndex = new }
    }

    private func carousel(_ list: [Itinerary]) -> some View {
        let arrives = arrival.isOpen
        return ScrollView(.horizontal) {
            HStack(alignment: .top, spacing: 12) {
                ForEach(list) { itinerary in
                    HomeTimelineCard(itinerary: itinerary, arrives: arrives) { openBlock($0, itinerary) }
                        .frame(width: max(0, pageWidth - 2 * Metrics.side))
                        .sqTransition(.pop)
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

/// 7pt page dots (active sage, others `mutedBorder`). The active dot slides to the new page.
private struct HomePageDots: View {
    let count: Int
    let index: Int
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        HStack(spacing: 5) {
            ForEach(0..<count, id: \.self) { page in
                Circle()
                    .fill(Theme.mutedBorder)
                    .frame(width: 7, height: 7)
                    .opacity(page == index ? 0 : 1)
            }
        }
        .overlay(alignment: .leading) {
            Circle()
                .fill(Theme.sage)
                .frame(width: 7, height: 7)
                .offset(x: CGFloat(index) * 12)
                .animation(reduceMotion ? Motion.reduced : Motion.arrive, value: index)
        }
        .accessibilityElement()
        .accessibilityLabel("Itinerary \(index + 1) of \(count)")
    }
}

/// 212pt itinerary card: title, "Today · 1–8 PM", one 6pt bar per block, "3 stops · 3 going".
/// The selection ring eases between cards; `highlight` pops the card with a sage glow.
struct HomeItineraryCard: View {
    let itinerary: Itinerary
    let isSelected: Bool
    var highlight: HomeCardHighlight? = nil
    let action: () -> Void
    @Environment(AppEnvironment.self) private var env
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

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
                    .sqNumeric()
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
        .animation(reduceMotion ? Motion.reduced : Motion.quick, value: isSelected)
        .modifier(HomeCardPopModifier(highlight: highlight))
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

/// The card's highlight pop: scale and the glow ring's strength (0 at rest).
private nonisolated struct HomeCardPop {
    var scale: CGFloat = 1
    var glow: Double = 0
}

/// Once per `highlight.token`, after its delay: the card pops and a sage ring pulses around it.
private struct HomeCardPopModifier: ViewModifier {
    let highlight: HomeCardHighlight?
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func body(content: Content) -> some View {
        let delay = max(0.01, highlight?.delay ?? 0)
        let peak: CGFloat = reduceMotion ? 1 : 1.06
        let ring = Theme.sage
        let radius = Metrics.cardRadius + 4
        return content.keyframeAnimator(initialValue: HomeCardPop(), trigger: highlight?.token ?? 0) { view, pop in
            view
                .scaleEffect(pop.scale)
                .overlay {
                    RoundedRectangle(cornerRadius: radius, style: .continuous)
                        .stroke(ring, lineWidth: 3)
                        .padding(-4)
                        .opacity(pop.glow * 0.6)
                        .allowsHitTesting(false)
                        .accessibilityHidden(true)
                }
        } keyframes: { _ in
            KeyframeTrack(\.scale) {
                LinearKeyframe(1, duration: delay)
                SpringKeyframe(peak, duration: 0.2, spring: .snappy)
                SpringKeyframe(1, duration: 0.5, spring: .bouncy)
            }
            KeyframeTrack(\.glow) {
                LinearKeyframe(0, duration: delay)
                CubicKeyframe(1, duration: 0.2)
                CubicKeyframe(0, duration: 0.9)
            }
        }
    }
}

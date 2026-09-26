import SwiftUI
import UIKit

/// Home › Calendar (GUI_PLAN.md §7.5b): day chips from Today, swipeable day panels (6 AM–11 PM),
/// and press and hold on empty time to plan a window. "+ Plan" is the non-gesture alternative. The
/// press-and-hold hint under the days shows until you've made a window once.
///
/// Motion: a skeleton day while the first load runs, then chips and blocks arrive one after
/// another; the planning window grows out of the press point, springs to each 15-minute step and
/// the "Plan this window" bar slides up under it.
struct HomeCalendarView: View {
    @Bindable var store: HomeStore
    let pageWidth: CGFloat
    let openItem: (CalendarItem) -> Void
    let retry: () -> Void

    @Environment(AppEnvironment.self) private var env
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    /// Chips and blocks arrive one after another only when they replace the skeleton.
    @State private var arrival = HomeArrivalWindow()
    /// Set once a window has been made by press and hold (the gesture is found; the hint goes).
    @AppStorage(HomeDayPanel.holdHintKey) private var madeWindow = false

    init(store: HomeStore, pageWidth: CGFloat, openItem: @escaping (CalendarItem) -> Void, retry: @escaping () -> Void) {
        self.store = store
        self.pageWidth = pageWidth
        self.openItem = openItem
        self.retry = retry
    }

    var body: some View {
        HomeLoadable(state: store.days, minHeight: 240, retry: retry) {
            HomeCalendarSkeleton()
        } content: { days in
            chips(days)
            pager(days)
            ZStack(alignment: .top) {
                if !madeWindow {
                    Text("Press and hold on empty time to plan a sidequest")
                        .sqFont(12, relativeTo: .caption)
                        .foregroundStyle(Theme.text3)
                        .homeLine(12)
                        .multilineTextAlignment(.center)
                        .frame(maxWidth: .infinity)
                        .padding(.horizontal, Metrics.side)
                        .padding(.top, 6)
                        .transition(.opacity)
                }
            }
            .padding(.bottom, 24)
        }
        .onAppear { arrival.begin(loading: store.days.isLoading) }
        .onChange(of: store.days.phase) { _, phase in arrival.update(phase) }
    }

    private func selectedId(in days: [CalendarDay]) -> String? {
        store.selectedDayId ?? days.first?.id
    }

    // MARK: Day chips

    private func chips(_ days: [CalendarDay]) -> some View {
        let selected = selectedId(in: days)
        let arrives = arrival.isOpen
        return ScrollViewReader { proxy in
            ScrollView(.horizontal) {
                HStack(spacing: 8) {
                    ForEach(Array(days.enumerated()), id: \.element.id) { index, day in
                        HomeDayChip(day: day, isSelected: day.id == selected) {
                            withAnimation(reduceMotion ? nil : .smooth(duration: 0.35)) { store.selectedDayId = day.id }
                        }
                        .homeArrival(index, enabled: arrives)
                        .id(day.id)
                    }
                }
                .padding(.horizontal, Metrics.side)
                .padding(.top, 14)
                .padding(.bottom, 10)
            }
            .scrollIndicators(.hidden)
            .sensoryFeedback(.selection, trigger: store.selectedDayId)
            .onChange(of: store.selectedDayId) { _, id in
                // Swiping the panels keeps the selected chip in view.
                guard let id else { return }
                withAnimation(reduceMotion ? nil : .smooth(duration: 0.3)) { proxy.scrollTo(id, anchor: .center) }
            }
        }
    }

    // MARK: Day panels

    private func pager(_ days: [CalendarDay]) -> some View {
        let arrives = arrival.isOpen
        return ScrollView(.horizontal) {
            HStack(alignment: .top, spacing: 12) {
                ForEach(days) { day in
                    HomeDayPanel(day: day, store: store, arrives: arrives, openItem: openItem)
                        .frame(width: max(0, pageWidth - 2 * Metrics.side))
                        .id(day.id)
                }
            }
            .scrollTargetLayout()
        }
        .contentMargins(.horizontal, Metrics.side, for: .scrollContent)
        .scrollTargetBehavior(.viewAligned)
        .scrollPosition(id: $store.selectedDayId)
        .scrollIndicators(.hidden)
        .padding(.bottom, 8)
    }
}

/// 50×66 day chip: weekday ("Today" first), day number, up to 3 kind dots. Selected = sage (it
/// eases in with a small pop).
struct HomeDayChip: View {
    let day: CalendarDay
    let isSelected: Bool
    let action: () -> Void
    @Environment(AppEnvironment.self) private var env
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        Button(action: action) {
            VStack(spacing: 2) {
                Text(env.clock.isToday(day.date) ? "Today" : env.format.weekdayShort(day.date))
                    .sqFont(11, .semibold, relativeTo: .caption2)
                    .homeLine(11)
                    .opacity(0.8)
                Text(env.format.dayNumber(day.date))
                    .sqFont(19, .bold, relativeTo: .title3)
                    .homeLine(19)
                HStack(spacing: 2) {
                    ForEach(kinds, id: \.self) { kind in
                        Circle()
                            .fill(isSelected ? Color.white : kind.palette.dot)
                            .frame(width: 5, height: 5)
                            .sqTransition(.pop)
                    }
                }
                .frame(height: 5)
            }
            .lineLimit(1)
            .minimumScaleFactor(0.7)
            .foregroundStyle(Theme.ink)
            .frame(width: 50, height: 66)
            .background(isSelected ? Theme.sage : .white, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
            .contentShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
        }
        .buttonStyle(.sqPressable)
        .sqBounce(when: isSelected, scale: 1.08)
        .animation(reduceMotion ? Motion.reduced : Motion.quick, value: isSelected)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(accessibilityText)
        .accessibilityAddTraits(isSelected ? [.isButton, .isSelected] : .isButton)
    }

    /// Distinct kinds in the order they first appear (busy grey, sidequest sage, group clay).
    private var kinds: [BlockKind] {
        var seen: [BlockKind] = []
        for item in day.items where !seen.contains(item.kind) { seen.append(item.kind) }
        return Array(seen.prefix(3))
    }

    private var accessibilityText: String {
        let count = day.items.count
        let summary = count == 0 ? "Nothing planned" : "\(count) on your calendar"
        return "\(env.format.longDate(day.date)), \(summary)"
    }
}

/// One day: "Friday, Sep 25" + "Today · 5 on your calendar" + "+ Plan", then the 6 AM–11 PM timeline.
///
/// Press and hold empty time (0.4 s) to draw a one-hour window there; keep holding and drag to
/// stretch it. Once it's down, drag its top or bottom edge to adjust it (30 minutes at least), tap
/// elsewhere or × to clear it, or "Plan this window" to open Create with it filled in.
struct HomeDayPanel: View {
    let day: CalendarDay
    let store: HomeStore
    var arrives = false
    let openItem: (CalendarItem) -> Void

    @Environment(AppEnvironment.self) private var env
    @Environment(Router.self) private var router
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    /// Bumped when a press and hold starts a window (medium haptic).
    @State private var holds = 0
    /// A window has been made by press and hold before (the Calendar's hint is no longer needed).
    @AppStorage(HomeDayPanel.holdHintKey) private var madeWindow = false
    /// What the finger on the timeline is doing, if anything.
    @State private var gesture: HomeWindowGesture?
    /// Where a new window grows from: the press point, relative to the box's layout frame.
    @State private var growAnchor = UnitPoint.center

    /// UserDefaults key: the user has made a window by press and hold at least once.
    static let holdHintKey = "home.calendar.madeWindow"
    static let hourHeight: CGFloat = 30
    static let topInset: CGFloat = 10
    static let timelineHeight: CGFloat = 530
    static let firstHour = 6
    /// Blocks and the window sit between these insets of the timeline.
    static let blockLeading: CGFloat = 58
    static let blockTrailing: CGFloat = 12

    private static func y(minutes: Int) -> CGFloat {
        topInset + CGFloat(minutes - firstHour * 60) * hourHeight / 60
    }

    var body: some View {
        VStack(spacing: 0) {
            header
            timeline
        }
        .background(.white, in: RoundedRectangle(cornerRadius: Metrics.cardRadius, style: .continuous))
        .sensoryFeedback(.impact(weight: .medium), trigger: holds)
        // A light tick for every 15-minute step while a finger draws or resizes the window.
        .sensoryFeedback(.selection, trigger: adjustingKey) { old, new in old != nil && new != nil }
    }

    private var header: some View {
        HStack(alignment: .center, spacing: 8) {
            VStack(alignment: .leading, spacing: 0) {
                Text(env.format.dayTitle(day.date))
                    .sqFont(17, .bold)
                    .foregroundStyle(Theme.ink)
                    .homeLine(17)
                Text(subtitle)
                    .sqFont(12, relativeTo: .caption)
                    .foregroundStyle(Theme.text3)
                    .homeLine(12)
                    .sqNumeric()
            }
            .accessibilityElement(children: .combine)
            .accessibilityAddTraits(.isHeader)
            Spacer(minLength: 0)
            Button("+ Plan") {
                router.openCreate(CreateDraft(date: day.date))
            }
            .buttonStyle(.sqTintPill)
            .padding(.vertical, -6)
            .accessibilityLabel("Plan a sidequest on \(env.format.longDate(day.date))")
        }
        .padding(.top, 14)
        .padding(.horizontal, 14)
        .padding(.bottom, 6)
    }

    private var subtitle: String {
        let count = day.items.count
        let summary = count == 0 ? "Nothing planned" : "\(count) on your calendar"
        return env.clock.isToday(day.date) ? "Today · \(summary)" : summary
    }

    // MARK: Timeline

    private var timeline: some View {
        let selection = currentSelection
        return ZStack(alignment: .topLeading) {
            ForEach(Array(stride(from: Self.firstHour, through: 22, by: 2)), id: \.self) { hour in
                hourRow(hour)
                    .offset(y: Self.y(minutes: hour * 60) - 6)
            }
            HomeTimeDragSurface(
                window: settledSpan,
                windowLeading: Self.blockLeading,
                windowTrailing: Self.blockTrailing,
                onHoldBegan: holdBegan,
                onHoldMoved: holdMoved,
                onEdgeBegan: edgeBegan,
                onEdgeMoved: edgeMoved,
                onEnded: gestureEnded,
                onTapOutside: clearSelection
            )
            .frame(maxWidth: .infinity)
            .frame(height: Self.timelineHeight)
            .accessibilityHidden(true)
            ForEach(Array(day.items.enumerated()), id: \.element.id) { index, item in
                if let span = visibleSpan(item) {
                    block(item, span: span)
                        .homeArrival(index + 1, enabled: arrives)
                        .sqTransition(.rise)
                }
            }
            if let selection {
                selectionBox(selection)
                    .id(selection.id)
                    .homeTransition(.asymmetric(insertion: .scale(scale: 0.5, anchor: growAnchor).combined(with: .opacity),
                                                removal: .opacity))
                if !selection.isAdjusting {
                    planBar(selection)
                        .sqTransition(.slideUp)
                }
            }
        }
        .frame(maxWidth: .infinity, alignment: .topLeading)
        .frame(height: Self.timelineHeight, alignment: .top)
    }

    private func hourRow(_ hour: Int) -> some View {
        HStack(spacing: 8) {
            Text(env.format.hourLabel(hour))
                .sqFont(10, relativeTo: .caption2)
                .foregroundStyle(Theme.text3)
                .lineLimit(1)
                .fixedSize()
                .frame(width: 44, alignment: .trailing)
            Rectangle().fill(Theme.hourLine).frame(height: 1)
        }
        .frame(height: 12)
        .padding(.trailing, 12)
        .allowsHitTesting(false)
        .accessibilityHidden(true)
    }

    private func minutes(_ date: Date) -> Int {
        Int((date.timeIntervalSince(env.clock.startOfDay(day.date)) / 60).rounded())
    }

    /// The part of an entry inside 6 AM – 11:20 PM (the bottom of the panel), in minutes.
    private func visibleSpan(_ item: CalendarItem) -> ClosedRange<Int>? {
        let lo = max(minutes(item.start), HomePlanSelection.earliest)
        let hi = min(minutes(item.end), Self.firstHour * 60 + Int((Self.timelineHeight - Self.topInset) / Self.hourHeight * 60))
        return hi > lo ? lo...hi : nil
    }

    private func block(_ item: CalendarItem, span: ClosedRange<Int>) -> some View {
        let palette = item.kind.palette
        let top = Self.y(minutes: span.lowerBound)
        let height = max(20, Self.y(minutes: span.upperBound) - top - 2)
        let time = env.format.range(item.start, item.end)
        let faces = Array((item.people + item.interested).prefix(4))
        let shape = RoundedRectangle(cornerRadius: 8, style: .continuous)
        return Button {
            openItem(item)
        } label: {
            HStack(alignment: .top, spacing: 6) {
                Text("\(item.title) · \(time)")
                    .sqFont(12, .semibold)
                    .homeLine(12, 1.25)
                    .frame(maxWidth: .infinity, alignment: .topLeading)
                if !faces.isEmpty {
                    HomeAvatarStack(people: faces, size: 20, fontSize: 8)
                }
            }
            .foregroundStyle(palette.text)
            // 1pt border + the prototype's 3 / 8 padding.
            .padding(.vertical, 4)
            .padding(.horizontal, 9)
            .frame(maxWidth: .infinity, alignment: .topLeading)
            .frame(height: height, alignment: .top)
            .background(palette.background, in: shape)
            .clipShape(shape)
            .overlay { shape.strokeBorder(palette.border, lineWidth: 1) }
            .contentShape(shape)
        }
        .buttonStyle(.sqPressable)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(item.title), \(time), \(palette.label)")
        .accessibilityHint("Opens details")
        .accessibilityAddTraits(.isButton)
        .padding(.leading, Self.blockLeading)
        .padding(.trailing, Self.blockTrailing)
        .offset(y: top)
    }

    // MARK: Planning window

    private var currentSelection: HomePlanSelection? {
        guard let selection = store.planSelection, selection.dayId == day.id else { return nil }
        return selection
    }

    /// The window's top and bottom once it's down (its edges can be dragged then).
    private var settledSpan: ClosedRange<CGFloat>? {
        guard let selection = currentSelection, !selection.isAdjusting else { return nil }
        return Self.y(minutes: selection.start)...Self.y(minutes: selection.end)
    }

    /// Changes with every 15-minute step while a finger draws or resizes the window.
    private var adjustingKey: Int? {
        guard let selection = currentSelection, selection.isAdjusting else { return nil }
        return selection.start * 10_000 + selection.end
    }

    private func boxHeight(_ selection: HomePlanSelection) -> CGFloat {
        max(14, CGFloat(selection.end - selection.start) * Self.hourHeight / 60)
    }

    private func selectionBox(_ selection: HomePlanSelection) -> some View {
        let top = Self.y(minutes: selection.start)
        let height = boxHeight(selection)
        // Under two lines' height (about 90 minutes), the title and time share one line.
        let compact = height < 44
        let shape = RoundedRectangle(cornerRadius: 8, style: .continuous)
        let showsHandles: Bool = {
            if case .holding = gesture { return false }
            return true
        }()
        return VStack(alignment: .leading, spacing: 0) {
            if compact {
                (Text("New sidequest").fontWeight(.bold).foregroundStyle(Theme.sageInk)
                    + Text(" · \(label(selection))").foregroundStyle(Theme.ink))
                    .sqFont(12)
                    .homeLine(12)
            } else {
                Text("New sidequest")
                    .sqFont(12, .bold)
                    .foregroundStyle(Theme.sageInk)
                    .homeLine(12)
                Text(label(selection))
                    .sqFont(12)
                    .foregroundStyle(Theme.ink)
                    .homeLine(12)
                    .sqNumeric()
            }
        }
        .lineLimit(1)
        // 2pt border + the prototype's 4 / 8 padding (a short window centers its one line).
        .padding(.vertical, compact ? 0 : 6)
        .padding(.horizontal, 10)
        .frame(maxWidth: .infinity, alignment: compact ? .leading : .topLeading)
        .frame(height: height, alignment: compact ? .center : .top)
        .background(Theme.sageTint, in: shape)
        .clipShape(shape)
        .overlay { shape.strokeBorder(Theme.sage, lineWidth: 2) }
        // Grab handles (the whole top and bottom edges drag).
        .overlay(alignment: .topTrailing) { handle.offset(x: -24, y: -5).opacity(showsHandles ? 1 : 0) }
        .overlay(alignment: .bottomLeading) { handle.offset(x: 24, y: 5).opacity(showsHandles ? 1 : 0) }
        .padding(.leading, Self.blockLeading)
        .padding(.trailing, Self.blockTrailing)
        .offset(y: top)
        .allowsHitTesting(false)
        .accessibilityElement(children: .combine)
    }

    private var handle: some View {
        Circle()
            .fill(.white)
            .frame(width: 10, height: 10)
            .overlay { Circle().strokeBorder(Theme.sage, lineWidth: 2) }
            .accessibilityHidden(true)
    }

    private func planBar(_ selection: HomePlanSelection) -> some View {
        let top = Self.y(minutes: selection.start)
        let height = boxHeight(selection)
        return HStack(spacing: 6) {
            Button("Plan this window") { plan(selection) }
                .buttonStyle(.sq(fill: Theme.sage, foreground: Theme.ink, height: 36, radius: 10, fontSize: 14))
                .shadow(color: .black.opacity(0.15), radius: 6, x: 0, y: 4)
                .contentShape(Rectangle().inset(by: -4))
                .accessibilityLabel("Plan this window, \(label(selection))")
            Button(action: clearSelection) {
                HomeIcon(glyph: .close, size: 14, strokeWidth: 2.6)
                    .foregroundStyle(.white)
                    .frame(width: 36, height: 36)
                    .background(Theme.ink, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                    .contentShape(Rectangle().inset(by: -4))
            }
            .buttonStyle(.sqPressable)
            .accessibilityLabel("Clear selection")
        }
        .padding(.leading, Self.blockLeading)
        .padding(.trailing, Self.blockTrailing)
        .offset(y: min(Self.timelineHeight - 40, top + height + 6))
    }

    private func label(_ selection: HomePlanSelection) -> String {
        env.format.range(date(selection.start), date(selection.end))
    }

    private func date(_ minutes: Int) -> Date {
        env.clock.on(day.date, minutes / 60, minutes % 60)
    }

    /// The minute under `y`, snapped to 15 and clamped to 6 AM–11 PM.
    private func minute(at y: CGFloat) -> Int {
        let steps = ((y - Self.topInset) / Self.hourHeight * 60 / 15).rounded()
        let value = Int(steps) * 15 + Self.firstHour * 60
        return min(max(value, HomePlanSelection.earliest), HomePlanSelection.latest)
    }

    // MARK: Gestures

    /// Held long enough: a one-hour window appears under the finger, growing out of it.
    private func holdBegan(at point: CGPoint, width: CGFloat) {
        let selection = HomePlanSelection.hour(at: minute(at: point.y), dayId: day.id)
        growAnchor = UnitPoint(x: point.x / max(width, 1), y: point.y / max(boxHeight(selection), 1))
        gesture = .holding(start: selection.start, end: selection.end)
        holds += 1
        withMotion(Motion.arrive) { store.planSelection = selection }
    }

    /// Still holding: the window keeps the pressed hour and stretches to the finger (above it moves
    /// the start, below it the end).
    private func holdMoved(to y: CGFloat) {
        guard case .holding(let start, let end) = gesture, var selection = currentSelection else { return }
        let finger = minute(at: y)
        let lower = min(start, finger), upper = max(end, finger)
        guard lower != selection.start || upper != selection.end else { return }
        selection.start = lower
        selection.end = upper
        withMotion(Motion.quick) { store.planSelection = selection }
    }

    private func edgeBegan(_ edge: HomeWindowEdge, at y: CGFloat) {
        guard var selection = currentSelection else { return }
        let edgeY = Self.y(minutes: edge == .top ? selection.start : selection.end)
        gesture = .edge(edge, grab: y - edgeY)
        selection.isAdjusting = true
        withMotion(Motion.quick) { store.planSelection = selection }
    }

    /// Resizing from one edge, 30 minutes at least.
    private func edgeMoved(to y: CGFloat) {
        guard case .edge(let edge, let grab) = gesture, var selection = currentSelection else { return }
        let value = minute(at: y - grab)
        switch edge {
        case .top: selection.start = min(value, selection.end - HomePlanSelection.minimumLength)
        case .bottom: selection.end = max(value, selection.start + HomePlanSelection.minimumLength)
        }
        guard selection != currentSelection else { return }
        withMotion(Motion.quick) { store.planSelection = selection }
    }

    /// Finger up: the window settles and the "Plan this window" bar slides up under it.
    private func gestureEnded() {
        guard gesture != nil else { return }
        gesture = nil
        guard var selection = currentSelection, selection.isAdjusting else { return }
        selection.isAdjusting = false
        withMotion(Motion.arrive) {
            store.planSelection = selection
            madeWindow = true
        }
    }

    private func clearSelection() {
        guard currentSelection != nil else { return }
        withMotion(Motion.quick) { store.planSelection = nil }
    }

    private func plan(_ selection: HomePlanSelection) {
        router.openCreate(CreateDraft(date: day.date, start: date(selection.start), end: date(selection.end)))
        store.planSelection = nil
    }
}

/// What a finger on a day's timeline is doing.
private enum HomeWindowGesture: Equatable {
    /// Press and hold: the window keeps this pressed hour and stretches past it.
    case holding(start: Int, end: Int)
    /// Dragging an edge; `grab` is how far from that edge the finger started.
    case edge(HomeWindowEdge, grab: CGFloat)
}

enum HomeWindowEdge: Equatable {
    case top, bottom
}

/// Transparent UIKit surface under the calendar blocks for the planning gestures:
/// - press and hold (0.4 s) on empty time starts a window; keep holding and drag to stretch it;
/// - once a window is down, a drag that starts on its top or bottom edge resizes it;
/// - a tap outside the window clears it.
/// A quick tap or an ordinary swipe does nothing here, so the page scrolls and the days page as
/// usual. While a finger draws or resizes, the enclosing scroll views hold still.
struct HomeTimeDragSurface: UIViewRepresentable {
    /// The settled window's top and bottom (y in this view); its edges can be dragged.
    var window: ClosedRange<CGFloat>?
    var windowLeading: CGFloat
    var windowTrailing: CGFloat
    var onHoldBegan: (_ point: CGPoint, _ width: CGFloat) -> Void
    var onHoldMoved: (_ y: CGFloat) -> Void
    var onEdgeBegan: (_ edge: HomeWindowEdge, _ y: CGFloat) -> Void
    var onEdgeMoved: (_ y: CGFloat) -> Void
    var onEnded: () -> Void
    var onTapOutside: () -> Void

    /// How long to hold before a window appears.
    static let holdDuration: TimeInterval = 0.4

    func makeCoordinator() -> Coordinator { Coordinator(surface: self) }

    func makeUIView(context: Context) -> UIView {
        let view = UIView()
        view.backgroundColor = .clear
        view.isAccessibilityElement = false
        let coordinator = context.coordinator

        let hold = UILongPressGestureRecognizer(target: coordinator, action: #selector(Coordinator.handleHold(_:)))
        hold.minimumPressDuration = Self.holdDuration
        hold.delegate = coordinator
        view.addGestureRecognizer(hold)
        coordinator.hold = hold

        // Starts at touch-down, but only on a settled window's edge (see `shouldReceive`).
        let edge = UILongPressGestureRecognizer(target: coordinator, action: #selector(Coordinator.handleEdge(_:)))
        edge.minimumPressDuration = 0
        edge.allowableMovement = .greatestFiniteMagnitude
        edge.delegate = coordinator
        view.addGestureRecognizer(edge)
        coordinator.edge = edge

        let tap = UITapGestureRecognizer(target: coordinator, action: #selector(Coordinator.handleTap(_:)))
        tap.delegate = coordinator
        view.addGestureRecognizer(tap)
        return view
    }

    func updateUIView(_ view: UIView, context: Context) {
        context.coordinator.surface = self
    }

    static func dismantleUIView(_ view: UIView, coordinator: Coordinator) {
        coordinator.unlockScrolling()
    }

    final class Coordinator: NSObject, UIGestureRecognizerDelegate {
        var surface: HomeTimeDragSurface
        weak var hold: UILongPressGestureRecognizer?
        weak var edge: UILongPressGestureRecognizer?
        private var grabbedEdge: HomeWindowEdge?
        private var lockedPans: [UIGestureRecognizer] = []

        init(surface: HomeTimeDragSurface) {
            self.surface = surface
        }

        @objc func handleHold(_ recognizer: UILongPressGestureRecognizer) {
            let point = recognizer.location(in: recognizer.view)
            switch recognizer.state {
            case .began:
                lockScrolling(from: recognizer.view)
                surface.onHoldBegan(point, recognizer.view?.bounds.width ?? 0)
            case .changed:
                surface.onHoldMoved(point.y)
            case .ended, .cancelled, .failed:
                unlockScrolling()
                surface.onEnded()
            default:
                break
            }
        }

        @objc func handleEdge(_ recognizer: UILongPressGestureRecognizer) {
            let point = recognizer.location(in: recognizer.view)
            switch recognizer.state {
            case .began:
                guard let edge = edgeAt(point, in: recognizer.view) else { return }
                grabbedEdge = edge
                lockScrolling(from: recognizer.view)
                surface.onEdgeBegan(edge, point.y)
            case .changed:
                if grabbedEdge != nil { surface.onEdgeMoved(point.y) }
            case .ended, .cancelled, .failed:
                guard grabbedEdge != nil else { return }
                grabbedEdge = nil
                unlockScrolling()
                surface.onEnded()
            default:
                break
            }
        }

        @objc func handleTap(_ recognizer: UITapGestureRecognizer) {
            guard !isInsideWindow(recognizer.location(in: recognizer.view), in: recognizer.view) else { return }
            surface.onTapOutside()
        }

        /// Edge drags only start on a settled window's edge; press and hold anywhere else.
        func gestureRecognizer(_ recognizer: UIGestureRecognizer, shouldReceive touch: UITouch) -> Bool {
            let onEdge = edgeAt(touch.location(in: recognizer.view), in: recognizer.view) != nil
            if recognizer === edge { return onEdge }
            if recognizer === hold { return !onEdge }
            return true
        }

        /// The window edge under `point`: 18pt outside it, and up to half its height inside.
        private func edgeAt(_ point: CGPoint, in view: UIView?) -> HomeWindowEdge? {
            guard let window = surface.window, let width = view?.bounds.width else { return nil }
            guard point.x >= surface.windowLeading - 8, point.x <= width - surface.windowTrailing + 8 else { return nil }
            let reach: CGFloat = 18
            let inside = min(reach, (window.upperBound - window.lowerBound) / 2)
            if point.y >= window.lowerBound - reach, point.y <= window.lowerBound + inside { return .top }
            if point.y >= window.upperBound - inside, point.y <= window.upperBound + reach { return .bottom }
            return nil
        }

        private func isInsideWindow(_ point: CGPoint, in view: UIView?) -> Bool {
            guard let window = surface.window, let width = view?.bounds.width else { return false }
            return point.x >= surface.windowLeading && point.x <= width - surface.windowTrailing && window.contains(point.y)
        }

        /// Holds the page and the day pager still while a finger draws or resizes a window.
        private func lockScrolling(from view: UIView?) {
            unlockScrolling()
            var ancestor = view?.superview
            while let current = ancestor {
                if let scroll = current as? UIScrollView, scroll.panGestureRecognizer.isEnabled {
                    scroll.panGestureRecognizer.isEnabled = false
                    lockedPans.append(scroll.panGestureRecognizer)
                }
                ancestor = current.superview
            }
        }

        func unlockScrolling() {
            for pan in lockedPans { pan.isEnabled = true }
            lockedPans.removeAll()
        }
    }
}

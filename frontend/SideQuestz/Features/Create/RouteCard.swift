import SwiftUI

/// One row of the Review route: A, a leg, a stop, … a leg, B.
struct CreateRouteRow: Identifiable, Equatable {
    enum Kind: Equatable {
        case start, leg, end
        case stop(number: Int)
    }

    let id: String
    let kind: Kind
    var stopId: String?
    var title: String
    var subtitle: String?
    /// B row in red: "Arrive 6:45 PM · 15 min past 6:30 PM".
    var isLate = false
    /// Waiting on the server's timing: legs read "Updating transit…", stop times "…".
    var isPending = false

    /// Builds the rows for an option in its current order. While the timing is stale (first load,
    /// mid-drag, or waiting for `POST /plans/route`) legs read "Updating transit…", stop times "…"
    /// and B "Back by 6:30 PM".
    static func rows(stops: [PlanStop], state: CreateRouteState, startName: String, endName: String,
                     startTime: Date, backBy: Date, format: TimeFormat) -> [CreateRouteRow] {
        let result = state.isStale ? nil : state.result
        func leg(_ index: Int) -> CreateRouteRow {
            guard let result, result.legs.indices.contains(index) else {
                return CreateRouteRow(id: "leg-\(index)", kind: .leg, title: "Updating transit…", isPending: true)
            }
            let leg = result.legs[index]
            return CreateRouteRow(id: "leg-\(index)", kind: .leg, title: "\(leg.mode.label) · \(leg.minutes) min")
        }

        var rows = [CreateRouteRow(id: "A", kind: .start, title: startName, subtitle: "Leave \(format.time(startTime))")]
        for (index, stop) in stops.enumerated() {
            rows.append(leg(index))
            let time: String?
            if let result, result.stopTimes.indices.contains(index) {
                let interval = result.stopTimes[index]
                time = format.fullRange(interval.start, interval.end)
            } else {
                time = nil
            }
            rows.append(CreateRouteRow(id: "stop-\(stop.id)", kind: .stop(number: index + 1), stopId: stop.id,
                                       title: stop.title, subtitle: "\(time ?? "…") · \(stop.subtitle)",
                                       isPending: time == nil))
        }
        rows.append(leg(stops.count))

        let back = format.time(backBy)
        var end = CreateRouteRow(id: "B", kind: .end, title: endName, subtitle: "Back by \(back)")
        if let result {
            let arrive = format.time(result.arrival)
            if result.minutesLate > 0 {
                end.subtitle = "Arrive \(arrive) · \(result.minutesLate) min past \(back)"
                end.isLate = true
            } else {
                end.subtitle = "Arrive \(arrive) · back by \(back) ✓"
            }
        }
        rows.append(end)
        return rows
    }
}

/// The route card (white, radius 16, padding 6 × 14) with a 22pt rail. Stops reorder by dragging
/// their ☰ handle: the lifted row gets a white fill, shadow and a 2pt sage ring, rows swap live
/// when the finger passes a neighbor's midpoint, and on drop the server re-times the route.
/// VoiceOver: each handle is adjustable (swipe up/down moves the stop).
///
/// Motion: switching options cross-fades the rows. While the timing is pending the legs and stop
/// times shimmer; when it arrives, leg modes and minutes, stop times and the arrival roll into
/// place and the late B row eases to red (or back).
struct CreateRouteCard: View {
    @Bindable var model: CreateFlowModel
    let option: PlanOption

    @Environment(AppEnvironment.self) private var env
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    /// Stop row frames in the card's coordinate space (for the midpoint swap rule).
    @State private var stopFrames: [String: CGRect] = [:]
    @GestureState private var handleHeld = false

    private static let space = "createRouteCard"

    var body: some View {
        let state = model.routes[option.id] ?? CreateRouteState(order: option.stops.map(\.id))
        let stops = model.orderedStops(option)
        let rows = CreateRouteRow.rows(
            stops: stops, state: state, startName: model.start?.name ?? "Start", endName: model.endPlace?.name ?? "End",
            startTime: model.startTime, backBy: model.backBy, format: env.format
        )
        VStack(alignment: .leading, spacing: 8) {
            VStack(spacing: 0) {
                VStack(spacing: 0) {
                    ForEach(Array(rows.enumerated()), id: \.element.id) { index, row in
                        rowView(row, isFirst: index == 0, isLast: index == rows.count - 1, stopCount: stops.count)
                            .zIndex(row.stopId != nil && row.stopId == model.draggingStopId ? 2 : 0)
                    }
                }
                // Rows swap live while dragging.
                .animation(reduceMotion ? nil : Motion.quick, value: state.order)
                // Another option: its rows cross-fade in.
                .id(option.id)
                .transition(.opacity)
            }
            .padding(.vertical, 6)
            .padding(.horizontal, 14)
            .frame(maxWidth: .infinity)
            .background(.white, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
            .coordinateSpace(.named(Self.space))
            .sensoryFeedback(.impact(weight: .medium), trigger: model.dropCount)
            .onChange(of: handleHeld) { _, held in
                // Safety net: a cancelled gesture never reaches onEnded.
                if !held { model.endDrag() }
            }

            if let error = state.error, !state.isLoading {
                HStack(spacing: 8) {
                    Text(error)
                        .sqFont(13)
                        .foregroundStyle(Theme.dangerText)
                        .fixedSize(horizontal: false, vertical: true)
                    Spacer(minLength: 0)
                    Button("Try again") {
                        Task { await model.refreshRoute(option.id, minimumSeconds: 0) }
                    }
                    .buttonStyle(.sqLink(size: 13))
                }
                .sqTransition(.rise)
            }
        }
        // The server's timing arriving (or failing) animates into place.
        .animation(Motion.standard, value: rows)
        .animation(Motion.standard, value: state.error)
    }

    // MARK: Rows

    private func rowView(_ row: CreateRouteRow, isFirst: Bool, isLast: Bool, stopCount: Int) -> some View {
        let lifted = row.stopId != nil && row.stopId == model.draggingStopId
        return HStack(spacing: 8) {
            VStack(alignment: .leading, spacing: 0) {
                Text(row.title)
                    .sqFont(row.kind == .leg ? 13 : 16, row.kind == .leg ? .medium : .semibold)
                    .foregroundStyle(row.kind == .leg ? (row.isPending ? Theme.text3 : Theme.transitText) : Theme.ink)
                    .createLine(row.kind == .leg ? 13 : 16)
                    .sqNumeric()
                    .createPendingShimmer(row.kind == .leg && row.isPending)
                if let subtitle = row.subtitle {
                    Text(subtitle)
                        .sqFont(12, relativeTo: .caption)
                        .foregroundStyle(row.isLate ? Theme.danger : Theme.text3)
                        .createLine(12, relativeTo: .caption)
                        .sqNumeric()
                        .createPendingShimmer(row.kind != .leg && row.isPending)
                }
            }
            .fixedSize(horizontal: false, vertical: true)
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityElement(children: .combine)
            if let stopId = row.stopId, case .stop(let number) = row.kind {
                handle(stopId: stopId, title: row.title, number: number, count: stopCount, lifted: lifted)
            }
        }
        .padding(.top, 6)
        .padding(.bottom, 10)
        .padding(.leading, 22 + 12)
        .overlay(alignment: .topLeading) {
            rail(row.kind, isFirst: isFirst, isLast: isLast)
        }
        .padding(.horizontal, 8)
        .background {
            if lifted {
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .fill(.white)
                    .shadow(color: .black.opacity(0.18), radius: 12, x: 0, y: 8)
                    .overlay {
                        RoundedRectangle(cornerRadius: 14, style: .continuous)
                            .strokeBorder(Theme.sage, lineWidth: 2)
                            .padding(-2)
                    }
                    .transition(.opacity)
            }
        }
        .padding(.horizontal, -8)
        // Picking a row up and setting it down fade the lift in and out.
        .animation(Motion.quick, value: lifted)
        .onGeometryChange(for: CGRect.self) { proxy in
            proxy.frame(in: .named(Self.space))
        } action: { frame in
            if let stopId = row.stopId { stopFrames[stopId] = frame }
        }
    }

    /// 22pt rail: a 12pt line above the marker, the marker, and a line to the bottom
    /// (transparent above the first row and below the last).
    private func rail(_ kind: CreateRouteRow.Kind, isFirst: Bool, isLast: Bool) -> some View {
        VStack(spacing: 0) {
            Rectangle()
                .fill(isFirst ? Color.clear : Theme.lineStrong)
                .frame(width: 2, height: 12)
            marker(kind)
            Rectangle()
                .fill(isLast ? Color.clear : Theme.lineStrong)
                .frame(width: 2)
                .frame(maxHeight: .infinity)
        }
        .frame(width: 22)
        .frame(maxHeight: .infinity, alignment: .top)
        .accessibilityHidden(true)
    }

    @ViewBuilder
    private func marker(_ kind: CreateRouteRow.Kind) -> some View {
        switch kind {
        case .start, .end:
            // The logo's ring (A) and diamond (B). The ring is smaller than its 22pt box, so the
            // rail runs in behind it to meet its edge, like it met the old circle.
            RouteMarker(kind: kind == .start ? .start : .end, size: 22)
                .background(alignment: kind == .start ? .bottom : .top) {
                    Rectangle()
                        .fill(Theme.lineStrong)
                        .frame(width: 2, height: 11)
                }
        case .stop(let number):
            Text("\(number)")
                .sqFont(11, .bold)
                .foregroundStyle(Theme.ink)
                .frame(width: 20, height: 20)
                .background(Theme.sage, in: Circle())
        case .leg:
            Circle()
                .strokeBorder(Theme.transitBorder, lineWidth: 2)
                .frame(width: 8, height: 8)
                .padding(.vertical, 2)
        }
    }

    // MARK: Drag handle

    private func handle(stopId: String, title: String, number: Int, count: Int, lifted: Bool) -> some View {
        DragHandleGlyph(width: 12.8, gap: 5.5, lineWidth: 2.2)
            .foregroundStyle(lifted ? Theme.sageInk : Theme.mutedIcon)
            .frame(width: 44, height: 44)
            .contentShape(Rectangle())
            .gesture(dragGesture(stopId))
            .padding(.trailing, -8)
            .accessibilityElement()
            .accessibilityLabel("Reorder \(title)")
            .accessibilityValue("Stop \(number) of \(count)")
            .accessibilityHint("Swipe up or down to move this stop.")
            .accessibilityAdjustableAction { direction in
                if direction == .increment {
                    model.moveStopAccessibly(stopId, by: 1)
                } else if direction == .decrement {
                    model.moveStopAccessibly(stopId, by: -1)
                }
            }
    }

    private func dragGesture(_ stopId: String) -> some Gesture {
        DragGesture(minimumDistance: 0, coordinateSpace: .named(Self.space))
            .updating($handleHeld) { _, held, _ in held = true }
            .onChanged { value in
                if model.draggingStopId != stopId { model.beginDrag(stopId) }
                swapIfNeeded(stopId, fingerY: value.location.y)
            }
            .onEnded { _ in model.endDrag() }
    }

    /// Swap with a neighbor once the finger passes its midpoint (one slot per move event).
    private func swapIfNeeded(_ stopId: String, fingerY: CGFloat) {
        guard let order = model.routes[option.id]?.order, let index = order.firstIndex(of: stopId) else { return }
        if index > 0, let previous = stopFrames[order[index - 1]], fingerY < previous.midY {
            model.moveDraggedStop(by: -1)
        } else if index < order.count - 1, let next = stopFrames[order[index + 1]], fingerY > next.midY {
            model.moveDraggedStop(by: 1)
        }
    }
}

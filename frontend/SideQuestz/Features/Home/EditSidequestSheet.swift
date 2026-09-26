import SwiftUI

/// Home › "•••" › Edit sidequest: rename it, change the day and time window, reorder or remove
/// stops, and change who can join. Saving sends only what changed (`PATCH /itineraries/{id}`); the
/// server re-times the route and returns the updated sidequest.
///
/// Present with `.sqSheet(item:style: SQSheetStyle(height: .fromTop(60)))`.
struct HomeEditSidequestSheet: View {
    let itinerary: Itinerary
    let close: () -> Void
    /// The server saved the edit; this is its re-timed copy.
    let saved: (Itinerary) -> Void
    /// Confirmed "Delete sidequest" (the parent closes the sheet and deletes it).
    let delete: () -> Void

    @Environment(AppEnvironment.self) private var env
    @State private var title: String
    @State private var day: Date
    @State private var startTime: Date
    @State private var backByTime: Date
    @State private var visibility: Visibility
    @State private var stops: [ItineraryItem]
    @State private var saving = false
    @State private var errorMessage: String?
    @State private var confirmingDelete = false
    @State private var dragging: String?
    @State private var dragStartIndex = 0
    @State private var dragTranslation: CGFloat = 0
    @FocusState private var titleFocused: Bool

    private static let rowHeight: CGFloat = 60
    private static let stopsSpace = "editSidequestStops"

    init(itinerary: Itinerary, close: @escaping () -> Void, saved: @escaping (Itinerary) -> Void, delete: @escaping () -> Void) {
        self.itinerary = itinerary
        self.close = close
        self.saved = saved
        self.delete = delete
        _title = State(initialValue: itinerary.title)
        _day = State(initialValue: itinerary.date)
        _startTime = State(initialValue: itinerary.start)
        _backByTime = State(initialValue: itinerary.backBy)
        _visibility = State(initialValue: itinerary.visibility)
        _stops = State(initialValue: itinerary.items.filter { $0.kind == .sidequest || $0.kind == .group })
    }

    var body: some View {
        SheetScaffold(spacing: 14) {
            header
            SQTextField(label: "Name", text: $title, placeholder: "Name your sidequest", kind: .text)
                .focused($titleFocused)
                .submitLabel(.done)
                .onSubmit { titleFocused = false }
            sectionLabel("WHEN")
            whenCard
            sectionLabel("STOPS")
            stopsCard
            Text("Saving re-times the route and the transit between stops.")
                .sqFont(12)
                .foregroundStyle(Theme.text3)
            sectionLabel("WHO CAN JOIN")
            HStack(spacing: 8) {
                ForEach(Visibility.allCases) { option in
                    SQChip(label: option.label, isOn: visibility == option, style: .grid) {
                        withMotion(Motion.quick) { visibility = option }
                    }
                }
            }
            Text(visibility.note)
                .sqFont(13)
                .foregroundStyle(Theme.text2)
                .id(visibility)
                .sqTransition(.rise)
                .animation(Motion.standard, value: visibility)
            if itinerary.goingCount > 1 {
                InfoBox(systemImage: "person.2") {
                    Text("\(itinerary.goingCount - 1) \(itinerary.goingCount == 2 ? "person has" : "people have") joined. They see your changes.")
                        .sqFont(14)
                        .foregroundStyle(Theme.text2)
                }
            }
            if let errorMessage {
                Text(errorMessage)
                    .sqFont(14)
                    .foregroundStyle(Theme.danger)
                    .sqTransition(.rise)
                    .accessibilityAddTraits(.isStaticText)
            }
            Button("Delete sidequest") { confirmingDelete = true }
                .buttonStyle(.sq(fill: .white, foreground: Theme.danger, border: Theme.errorBorder, borderWidth: 1.5, height: 46, fontSize: 15))
                .padding(.top, 6)
                .disabled(saving)
        }
        .confirmationDialog("Delete this sidequest?", isPresented: $confirmingDelete, titleVisibility: .visible) {
            Button("Delete sidequest", role: .destructive, action: delete)
        } message: {
            Text(itinerary.goingCount > 1 ? "Everyone who joined loses it too. This can't be undone." : "This can't be undone.")
        }
        .sensoryFeedback(.impact(weight: .medium), trigger: dragging) { old, new in old == nil && new != nil }
        .sensoryFeedback(.selection, trigger: stops.map(\.id))
    }

    // MARK: Header

    private var header: some View {
        HStack(spacing: 0) {
            Button(action: close) {
                Text("Cancel")
                    .sqFont(16)
                    .foregroundStyle(Theme.sageInk)
                    .frame(height: 36)
                    .contentShape(Rectangle().inset(by: -6))
            }
            .buttonStyle(.plain)
            .frame(width: 80, alignment: .leading)
            Text("Edit sidequest")
                .sqFont(17, .bold)
                .foregroundStyle(Theme.ink)
                .frame(maxWidth: .infinity)
                .accessibilityAddTraits(.isHeader)
            Button(action: save) {
                ZStack {
                    Text("Save").opacity(saving ? 0 : 1)
                    if saving { LoadingDots(color: Theme.ink, dotSize: 5) }
                }
            }
            .buttonStyle(canSave || saving ? .sqPill : .sqPill(fill: Theme.mutedBorder, foreground: Theme.ink))
            .disabled(!canSave || saving)
            .frame(width: 80, alignment: .trailing)
            .accessibilityValue(saving ? "Saving" : "")
        }
        .frame(height: 36)
        .animation(Motion.quick, value: canSave)
    }

    private func sectionLabel(_ text: String) -> some View {
        Text(text)
            .sqFont(13, .semibold)
            .tracking(0.4)
            .foregroundStyle(Theme.text3)
            .accessibilityAddTraits(.isHeader)
    }

    // MARK: When

    private var whenCard: some View {
        VStack(spacing: 0) {
            whenRow("Day") {
                DatePicker("Day", selection: $day, displayedComponents: .date).labelsHidden()
            }
            RowDivider(color: Theme.cream)
            whenRow("Starts") {
                DatePicker("Starts", selection: $startTime, displayedComponents: .hourAndMinute).labelsHidden()
            }
            RowDivider(color: Theme.cream)
            whenRow("Back by") {
                DatePicker("Back by", selection: $backByTime, displayedComponents: .hourAndMinute).labelsHidden()
            }
            if let windowProblem {
                Text(windowProblem)
                    .sqFont(13)
                    .foregroundStyle(Theme.danger)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.horizontal, 14)
                    .padding(.bottom, 10)
                    .sqTransition(.rise)
            }
        }
        .background(.white, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        .animation(Motion.standard, value: windowProblem)
    }

    private func whenRow<Picker: View>(_ label: String, @ViewBuilder picker: () -> Picker) -> some View {
        HStack {
            Text(label).sqFont(15).foregroundStyle(Theme.ink)
            Spacer(minLength: 8)
            picker()
        }
        .padding(.horizontal, 14)
        .frame(minHeight: 50)
    }

    // MARK: Stops

    private var stopsCard: some View {
        VStack(spacing: 0) {
            ForEach(Array(stops.enumerated()), id: \.element.id) { index, stop in
                stopRow(stop, index: index)
            }
        }
        .coordinateSpace(name: Self.stopsSpace)
        .background(.white, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
    }

    private func stopRow(_ stop: ItineraryItem, index: Int) -> some View {
        let lifted = dragging == stop.id
        return HStack(spacing: 12) {
            Text("\(index + 1)")
                .sqFont(11, .bold)
                .foregroundStyle(Theme.ink)
                .frame(width: 20, height: 20)
                .background(Theme.sage, in: Circle())
                .sqNumeric()
            VStack(alignment: .leading, spacing: 1) {
                Text(stop.title)
                    .sqFont(15, .semibold)
                    .foregroundStyle(Theme.ink)
                    .lineLimit(1)
                if let place = stop.place?.name, !place.isEmpty {
                    Text(place)
                        .sqFont(12)
                        .foregroundStyle(Theme.text3)
                        .lineLimit(1)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            Button {
                withMotion(Motion.arrive) { stops.removeAll { $0.id == stop.id } }
            } label: {
                Image(systemName: "minus.circle.fill")
                    .font(.system(size: 20))
                    .foregroundStyle(stops.count > 1 ? Theme.danger : Theme.mutedBorder)
                    .frame(width: 36, height: 44)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .disabled(stops.count <= 1)
            .accessibilityLabel("Remove \(stop.title)")
            DragHandleGlyph(width: 12.8, gap: 5.5, lineWidth: 2.2)
                .foregroundStyle(lifted ? Theme.sageInk : Theme.mutedIcon)
                .frame(width: 40, height: 44)
                .contentShape(Rectangle())
                .gesture(dragGesture(stop.id))
                .accessibilityElement()
                .accessibilityLabel("Reorder \(stop.title)")
                .accessibilityValue("Stop \(index + 1) of \(stops.count)")
                .accessibilityAdjustableAction { direction in
                    switch direction {
                    case .increment: move(stop.id, by: 1)
                    case .decrement: move(stop.id, by: -1)
                    @unknown default: break
                    }
                }
        }
        .padding(.leading, 14)
        .padding(.trailing, 6)
        .frame(height: Self.rowHeight)
        .background {
            RoundedRectangle(cornerRadius: 14, style: .continuous)
                .fill(.white)
                .shadow(color: .black.opacity(lifted ? 0.18 : 0), radius: 12, y: 4)
                .overlay {
                    RoundedRectangle(cornerRadius: 14, style: .continuous)
                        .strokeBorder(Theme.sage, lineWidth: lifted ? 2 : 0)
                }
        }
        .overlay(alignment: .bottom) {
            if index < stops.count - 1 && !lifted { RowDivider(color: Theme.cream).padding(.leading, 46) }
        }
        .offset(y: lifted ? liftedOffset(index) : 0)
        .zIndex(lifted ? 1 : 0)
        .sqTransition(.rise)
    }

    /// Keeps the lifted row under the finger while the others slide past it.
    private func liftedOffset(_ index: Int) -> CGFloat {
        dragTranslation - CGFloat(index - dragStartIndex) * Self.rowHeight
    }

    private func dragGesture(_ id: String) -> some Gesture {
        DragGesture(minimumDistance: 0, coordinateSpace: .named(Self.stopsSpace))
            .onChanged { value in
                guard let current = stops.firstIndex(where: { $0.id == id }) else { return }
                if dragging == nil {
                    dragStartIndex = current
                    withMotion(Motion.quick) { dragging = id }
                }
                dragTranslation = value.translation.height
                let target = min(max(dragStartIndex + Int((dragTranslation / Self.rowHeight).rounded()), 0), stops.count - 1)
                if target != current {
                    withMotion(Motion.quick) {
                        stops.move(fromOffsets: IndexSet(integer: current), toOffset: target > current ? target + 1 : target)
                    }
                }
            }
            .onEnded { _ in
                withMotion(Motion.arrive) {
                    dragging = nil
                    dragTranslation = 0
                }
            }
    }

    private func move(_ id: String, by delta: Int) {
        guard let index = stops.firstIndex(where: { $0.id == id }) else { return }
        let target = index + delta
        guard stops.indices.contains(target) else { return }
        withMotion(Motion.quick) {
            stops.move(fromOffsets: IndexSet(integer: index), toOffset: delta > 0 ? target + 1 : target)
        }
    }

    // MARK: Saving

    private var trimmedTitle: String { title.trimmingCharacters(in: .whitespacesAndNewlines) }

    private var startMinutes: Int { env.clock.minutesIntoDay(startTime) }
    private var backByMinutes: Int { env.clock.minutesIntoDay(backByTime) }

    private var windowProblem: String? {
        backByMinutes - startMinutes < 30 ? "Back by has to be at least 30 minutes after the start." : nil
    }

    /// Only what changed, so the server keeps everything else as it is.
    private var update: ItineraryUpdate {
        let clock = env.clock
        var update = ItineraryUpdate()
        if trimmedTitle != itinerary.title { update.title = trimmedTitle }
        let dayChanged = !clock.calendar.isDate(day, inSameDayAs: itinerary.date)
        if dayChanged { update.date = clock.startOfDay(day) }
        if dayChanged || startMinutes != clock.minutesIntoDay(itinerary.start) {
            update.start = clock.on(day, startMinutes / 60, startMinutes % 60)
        }
        if dayChanged || backByMinutes != clock.minutesIntoDay(itinerary.backBy) {
            update.backBy = clock.on(day, backByMinutes / 60, backByMinutes % 60)
        }
        if visibility != itinerary.visibility { update.visibility = visibility }
        let order = stops.map(\.id)
        if order != itinerary.items.filter({ $0.kind == .sidequest || $0.kind == .group }).map(\.id) {
            update.stopOrder = order
        }
        return update
    }

    private var canSave: Bool {
        !update.isEmpty && !trimmedTitle.isEmpty && windowProblem == nil
    }

    private func save() {
        let update = self.update
        guard canSave, !saving else { return }
        titleFocused = false
        saving = true
        withMotion { errorMessage = nil }
        Task {
            do {
                let updated = try await env.api.updateItinerary(id: itinerary.id, update)
                saved(updated)
            } catch {
                withMotion { errorMessage = (error as? LocalizedError)?.errorDescription ?? "Couldn't save your changes. Try again." }
                saving = false
            }
        }
    }
}

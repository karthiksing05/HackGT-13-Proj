import SwiftUI

/// Create › Vibe › Must-see, at the bottom of the step: search the catalog for places and events,
/// check as many as you like, and every option includes them (`PlanRequest.mustInclude`).
///
/// Results follow the typing (a short pause, and a newer query replaces the one in flight); with
/// nothing typed they're the server's suggestions for the plan's day near the start. Picks sit as
/// chips above the results and stay while the query changes. Loading: skeleton rows the first time
/// (the logo after a while), then dots in the field while newer results load over the old ones.
/// Focusing the field scrolls the section to the top, above the keyboard.
struct CreateMustSeeSection: View {
    @Bindable var model: CreateFlowModel
    var focus: FocusState<CreateField?>.Binding

    @Environment(AppEnvironment.self) private var env

    private static let anchor = "create.mustSee"
    /// How long typing has to pause before the search goes out.
    private static let debounce: Duration = .milliseconds(250)

    var body: some View {
        ScrollViewReader { proxy in
            VStack(alignment: .leading, spacing: 12) {
                VStack(alignment: .leading, spacing: 2) {
                    CreateEyebrow(text: "MUST-SEE", topMargin: 4)
                    CreateWrapText(text: "Every option includes what you pick.", size: 13, color: Theme.text2)
                }
                field
                if !model.mustSee.isEmpty {
                    chips
                        .sqTransition(.rise)
                }
                results
            }
            .id(Self.anchor)
            .animation(Motion.standard, value: model.mustSee.isEmpty)
            // While typing, the section sits at the top so its results show above the keyboard: once
            // the keyboard is up, and again whenever the results or the picks change its height.
            .onChange(of: focus.wrappedValue == .mustSee) { _, focused in
                if focused { reveal(proxy, after: .milliseconds(350)) }
            }
            .onChange(of: contentKey) {
                if focus.wrappedValue == .mustSee { reveal(proxy, after: .milliseconds(60)) }
            }
        }
        // Each keystroke restarts the wait and cancels the search in flight; suggestions (nothing
        // typed) go out at once.
        .task(id: model.mustSeeSearch) {
            if !model.mustSeeSearch.query.isEmpty {
                try? await Task.sleep(for: Self.debounce)
                guard !Task.isCancelled else { return }
            }
            await model.searchMustSee()
        }
    }

    /// What sets the section's height: the results on screen and the picks.
    private var contentKey: [String] {
        (model.mustSeeResults.value?.map(\.id) ?? ["\(model.mustSeeResults.phase)"]) + ["|"] + model.mustSee.map(\.id)
    }

    /// Scrolls the section's top to the top of the step (as far as the content allows) once the
    /// keyboard or the new layout has settled.
    private func reveal(_ proxy: ScrollViewProxy, after delay: Duration) {
        Task {
            try? await Task.sleep(for: delay)
            withMotion(Motion.gentle) { proxy.scrollTo(Self.anchor, anchor: .top) }
        }
    }

    // MARK: Field

    private var field: some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass")
                .font(.system(size: 12, weight: .semibold))
                .foregroundStyle(Theme.text3)
                .accessibilityHidden(true)
            TextField("", text: $model.mustSeeQuery, prompt: Text("Search places and events").foregroundStyle(Theme.text3))
                .sqFont(15)
                .foregroundStyle(Theme.ink)
                .autocorrectionDisabled()
                .textInputAutocapitalization(.never)
                .submitLabel(.search)
                .focused(focus, equals: .mustSee)
                .onSubmit { focus.wrappedValue = nil }
                .accessibilityLabel("Search places and events")
            if model.searchingMustSee {
                LoadingDots(color: Theme.text3, dotSize: 4.5)
                    .allowsHitTesting(false)
                    .sqTransition(.pop)
            }
            if !model.mustSeeQuery.isEmpty {
                Button {
                    model.mustSeeQuery = ""
                } label: {
                    Image(systemName: "xmark.circle.fill")
                        .font(.system(size: 15))
                        .foregroundStyle(Theme.mutedIcon)
                        .frame(width: 32, height: 42)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Clear search")
                .transition(.opacity)
            }
        }
        .padding(.leading, 12)
        .padding(.trailing, model.mustSeeQuery.isEmpty ? 12 : 2)
        .frame(height: 42)
        .background(.white, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        .contentShape(RoundedRectangle(cornerRadius: 12, style: .continuous))
        .onTapGesture { focus.wrappedValue = .mustSee }
        .animation(Motion.quick, value: model.mustSeeQuery.isEmpty)
        .animation(Motion.quick, value: model.searchingMustSee)
    }

    // MARK: Picks

    /// The picks as sage chips; tapping one takes it out.
    private var chips: some View {
        FlowLayout(spacing: 8) {
            ForEach(model.mustSee) { hit in
                Button {
                    withMotion(Motion.quick) { model.removeMustSee(hit.id) }
                } label: {
                    HStack(spacing: 6) {
                        Text(hit.title)
                            .lineLimit(1)
                            .truncationMode(.tail)
                        Image(systemName: "xmark")
                            .font(.system(size: 9, weight: .bold))
                    }
                    .sqFont(13, .semibold)
                    .foregroundStyle(Theme.ink)
                    .padding(.horizontal, 12)
                    .frame(height: 32)
                    .background(Theme.sage, in: Capsule())
                    // A long title truncates rather than running past the screen.
                    .frame(maxWidth: 300, alignment: .leading)
                    .contentShape(Rectangle().inset(by: -6))
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Remove \(hit.title)")
                .sqTransition(.pop)
            }
        }
        .animation(Motion.quick, value: model.mustSee.map(\.id))
    }

    // MARK: Results

    @ViewBuilder private var results: some View {
        VStack(alignment: .leading, spacing: 0) {
            switch model.mustSeeResults {
            case .loading:
                CreateMustSeeSkeleton()
                    .sqSlowLoading(lines: ["Finding places and events…"])
                    .transition(.opacity)
            case .failed(let message):
                // The flow's inline error: a red line and "Try again".
                HStack(spacing: 8) {
                    Text(message)
                        .sqFont(13)
                        .foregroundStyle(Theme.dangerText)
                        .fixedSize(horizontal: false, vertical: true)
                    Spacer(minLength: 0)
                    Button("Try again") {
                        Task { await model.searchMustSee() }
                    }
                    .buttonStyle(.sqLink(size: 13))
                }
                .transition(.opacity)
            case .loaded(let hits):
                if hits.isEmpty {
                    CreateWrapText(text: emptyMessage, size: 14, color: Theme.text3)
                        .transition(.opacity)
                } else {
                    VStack(spacing: 0) {
                        ForEach(Array(hits.enumerated()), id: \.element.id) { index, hit in
                            VStack(spacing: 0) {
                                if index > 0 { RowDivider(color: Theme.cream) }
                                row(hit)
                            }
                            .sqTransition(.rise)
                        }
                    }
                    .background(.white)
                    .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
                    .transition(.opacity)
                }
            }
        }
        .animation(Motion.standard, value: model.mustSeeResults.phase)
        .animation(Motion.standard, value: model.mustSeeResults.value?.map(\.id))
    }

    /// For the query the results answer (not a newer one still being typed).
    private var emptyMessage: String {
        let query = model.mustSeeResultsSearch?.query ?? ""
        return query.isEmpty ? "Nothing to suggest for this day. Try a search." : "Nothing matches “\(query)”."
    }

    /// Kind tile · title over the server's line · a check that fills when picked.
    private func row(_ hit: ActivityHit) -> some View {
        let isOn = model.isMustSee(hit.id)
        return Button {
            withMotion(Motion.quick) { model.toggleMustSee(hit) }
        } label: {
            HStack(spacing: 12) {
                CreateMustSeeKindTile(kind: hit.kind)
                VStack(alignment: .leading, spacing: 0) {
                    Text(hit.title)
                        .sqFont(15, .semibold)
                        .foregroundStyle(Theme.ink)
                        .createLine(15)
                        .lineLimit(2)
                    if !hit.subtitle.isEmpty {
                        Text(hit.subtitle)
                            .sqFont(13)
                            .foregroundStyle(Theme.text3)
                            .createLine(13)
                            .lineLimit(1)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                CreateMustSeeCheck(isOn: isOn)
            }
            .padding(.vertical, 10)
            .padding(.horizontal, 14)
            .frame(minHeight: 60)
            .contentShape(Rectangle())
        }
        .buttonStyle(.sqPressable)
        .sensoryFeedback(.selection, trigger: isOn)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(accessibilityLabel(hit))
        .accessibilityValue(isOn ? "selected" : "")
        .accessibilityHint(isOn ? "Takes it out of your must-see picks" : "Adds it to your must-see picks")
        .accessibilityAddTraits(.isButton)
    }

    /// "Sunset Jazz on Pier Nine, event, 6:30 PM" (VoiceOver adds "selected" from the value).
    private func accessibilityLabel(_ hit: ActivityHit) -> String {
        var parts = [hit.title, hit.kind == .event ? "event" : "place"]
        if hit.kind == .event, let start = hit.start { parts.append(env.format.time(start)) }
        return parts.joined(separator: ", ")
    }
}

/// 36pt tile: a calendar on sage tint for an event, a pin on cream for a place.
private struct CreateMustSeeKindTile: View {
    let kind: PlanStopKind

    var body: some View {
        Group {
            switch kind {
            case .event:
                CalendarGlyph(size: 20, lineWidth: 1.8)
                    .foregroundStyle(Theme.sageInk)
            case .place:
                CreatePinGlyph(size: 20, lineWidth: 1.8)
                    .foregroundStyle(Theme.text2)
            }
        }
        .frame(width: 36, height: 36)
        .background(kind == .event ? Theme.sageTint : Theme.cream, in: RoundedRectangle(cornerRadius: 10, style: .continuous))
        .accessibilityHidden(true)
    }
}

/// 24pt check: an empty ring, or sage with an ink check that pops in.
private struct CreateMustSeeCheck: View {
    let isOn: Bool

    var body: some View {
        ZStack {
            Circle().fill(isOn ? Theme.sage : .white)
            Circle().strokeBorder(isOn ? Theme.sage : Theme.mutedBorder, lineWidth: 1.5)
            if isOn {
                CheckGlyph(lineWidth: 2.8)
                    .foregroundStyle(Theme.ink)
                    .frame(width: 14, height: 14)
                    .transition(.opacity)
            }
        }
        .frame(width: 24, height: 24)
        .sqBounce(when: isOn, scale: 1.15)
        .animation(Motion.quick, value: isOn)
        .accessibilityHidden(true)
    }
}

/// Three rows shaped like results: a tile, a title and a line, the check.
private struct CreateMustSeeSkeleton: View {
    private static let widths: [(title: CGFloat, line: CGFloat)] = [(170, 120), (140, 150), (190, 104)]

    var body: some View {
        VStack(spacing: 0) {
            ForEach(Array(Self.widths.enumerated()), id: \.offset) { index, width in
                if index > 0 { RowDivider(color: Theme.cream) }
                HStack(spacing: 12) {
                    SkeletonBlock(width: 36, height: 36, radius: 10)
                    VStack(alignment: .leading, spacing: 7) {
                        SkeletonBlock(width: width.title, height: 13)
                        SkeletonBlock(width: width.line, height: 10)
                    }
                    .frame(maxWidth: .infinity, alignment: .leading)
                    Circle().fill(Theme.skeleton).frame(width: 24, height: 24)
                }
                .padding(.vertical, 12)
                .padding(.horizontal, 14)
                .frame(minHeight: 60)
            }
        }
        .background(.white)
        .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
        .sqShimmer()
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Loading")
    }
}

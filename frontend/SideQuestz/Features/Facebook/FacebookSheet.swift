import SwiftUI

/// Account › Connected › Facebook: the likes the import suggests changing ("Use these"), what it
/// found, friends who are on SideQuests, and Refresh / Disconnect. Present with
/// `.sqSheet(isPresented:style: FacebookSheet.style)`.
struct FacebookSheet: View {
    @Environment(AppEnvironment.self) private var env
    let model: FacebookModel
    let close: () -> Void

    @State private var confirmDisconnect = false

    static let style = SQSheetStyle(height: .fitted(max: 720))

    var body: some View {
        SheetScaffold(spacing: 16, shrinksToFit: true) {
            header
            ScrollView {
                VStack(alignment: .leading, spacing: 20) {
                    content
                    if let message = model.message {
                        ErrorBox(messages: [message])
                            .sqTransition(.rise)
                    }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
            }
            .scrollBounceBehavior(.basedOnSize)
            .sheetShrinks()
            if model.isConnected, !(model.connection.value?.needsReconnect ?? false) {
                footer
            }
        }
        .animation(Motion.standard, value: model.connection.phase)
        .animation(Motion.standard, value: model.isImporting)
        .task { await model.load(env) }
        .confirmationDialog("Disconnect Facebook?", isPresented: $confirmDisconnect, titleVisibility: .visible) {
            Button("Disconnect", role: .destructive) {
                Task {
                    if await model.disconnect(env) { close() }
                }
            }
        } message: {
            Text("We'll delete what we imported. Your likes stay as they are.")
        }
    }

    // MARK: Header

    private var header: some View {
        HStack(spacing: 12) {
            FacebookGlyph()
            VStack(alignment: .leading, spacing: 1) {
                Text("Facebook")
                    .sqFont(20, .bold)
                    .foregroundStyle(Theme.ink)
                    .accessibilityAddTraits(.isHeader)
                Text(subtitle)
                    .sqFont(13)
                    .foregroundStyle(Theme.text3)
                    .lineLimit(2)
                    .contentTransition(.opacity)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityElement(children: .combine)
        }
    }

    /// "Connected as Jordan Lee · updated Sep 25"
    private var subtitle: String {
        guard let connection = model.connection.value else { return " " }
        if connection.needsReconnect { return "Sign-in expired" }
        guard connection.connected else { return "Not connected" }
        var parts = [connection.name.map { "Connected as \($0)" } ?? "Connected"]
        if let date = connection.lastImport?.importedAt { parts.append("updated \(dayText(date))") }
        return parts.joined(separator: " · ")
    }

    // MARK: Content

    /// What the server does while it reads Facebook (in that order), under the S.
    private static let importLines = ["Reading Pages you like…", "Matching them to your likes…", "Looking for friends on SideQuests…"]

    @ViewBuilder private var content: some View {
        if model.isImporting {
            // Reading Facebook takes a moment: the S over what's happening, then the fresh import.
            LoadingStateView(lines: Self.importLines, minHeight: 220)
                .transition(.opacity)
        } else {
            connectionContent
                .transition(.opacity)
        }
    }

    /// The connection as we know it: its last import, how to connect, or why it couldn't load.
    @ViewBuilder private var connectionContent: some View {
        switch model.connection {
        case .loading:
            LoadingStateView(minHeight: 220)
        case .failed(let message):
            ErrorStateView(message: message, minHeight: 220) {
                Task { await model.load(env) }
            }
        case .loaded(let connection):
            if connection.needsReconnect || !connection.connected {
                connect(connection)
            } else if let imported = connection.lastImport {
                suggestions(imported)
                found(imported)
                if !imported.friendsOnApp.isEmpty {
                    friends(imported.friendsOnApp)
                }
                if !connection.declinedLabels.isEmpty {
                    declined(connection.declinedLabels)
                }
            } else {
                VStack(alignment: .leading, spacing: 12) {
                    Text("Nothing imported yet.")
                        .bodyStyle(Theme.text2)
                    primaryButton("Import now", busy: model.work == .refreshing) {
                        Task { await model.refresh(env) }
                    }
                }
            }
        }
    }

    /// Not connected (or Facebook wants a new sign-in).
    private func connect(_ connection: FacebookConnection) -> some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(connection.needsReconnect ? "Sign in to Facebook again to keep your likes up to date."
                                           : "Fill in your likes from Pages you like. We never post.")
                .bodyStyle(Theme.text2)
                .fixedSize(horizontal: false, vertical: true)
            primaryButton(connection.needsReconnect ? "Reconnect" : "Connect Facebook", busy: model.work == .connecting) {
                Task { await model.connect(env) }
            }
        }
    }

    // MARK: Suggested likes

    @ViewBuilder private func suggestions(_ imported: FacebookImport) -> some View {
        let changes = model.changes(against: env.preferences)
        VStack(alignment: .leading, spacing: 10) {
            Eyebrow(text: "SUGGESTED LIKES")
            if model.applied {
                HStack(spacing: 8) {
                    AnimatedCheck(lineWidth: 2.8, delay: 0.15)
                        .frame(width: 14, height: 14)
                    Text("Your likes are updated.")
                        .sqFont(14, .semibold)
                }
                .foregroundStyle(Theme.success)
                .padding(.horizontal, 14)
                .padding(.vertical, 12)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Theme.successBg, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
                .accessibilityElement(children: .combine)
                .sqTransition(.rise)
            } else if changes.isEmpty {
                Text(imported.suggestedRatings.isEmpty ? "Not enough likes on Facebook to suggest anything yet."
                                                       : "Your likes already match what we found.")
                    .sqFont(14)
                    .foregroundStyle(Theme.text3)
                    .fixedSize(horizontal: false, vertical: true)
            } else {
                VStack(spacing: 0) {
                    ForEach(Array(changes.enumerated()), id: \.element.id) { index, change in
                        if index > 0 { RowDivider(color: .white) }
                        changeRow(change)
                    }
                }
                .background(Theme.cream, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
                primaryButton("Use these", busy: model.work == .applying) {
                    Task { await model.apply(env) }
                }
            }
        }
    }

    /// "Live music ··· Not rated → 5 · Love it"
    private func changeRow(_ change: FacebookRatingChange) -> some View {
        HStack(spacing: 8) {
            Text(change.type.label)
                .sqFont(15)
                .foregroundStyle(Theme.ink)
                .lineLimit(1)
                .minimumScaleFactor(0.85)
            Spacer(minLength: 8)
            (Text(change.from.map(String.init) ?? "Not rated").foregroundStyle(Theme.text3)
                + Text("  →  ").foregroundStyle(Theme.text3)
                + Text("\(change.to) · \(TripType.scaleLabel(change.to))").foregroundStyle(Theme.sageInk).fontWeight(.semibold))
                .sqFont(14)
                .lineLimit(1)
                .minimumScaleFactor(0.85)
        }
        .padding(.vertical, 12)
        .padding(.horizontal, 14)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(change.type.label)
        .accessibilityValue("\(change.from.map { "From \($0)" } ?? "Not rated") to \(change.to), \(TripType.scaleLabel(change.to))")
    }

    // MARK: What we found

    private func found(_ imported: FacebookImport) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Eyebrow(text: "WHAT WE FOUND")
            if !imported.interests.isEmpty {
                FlowLayout(spacing: 8) {
                    ForEach(imported.interests, id: \.self) { interest in
                        TagLabel(text: interest, fill: Theme.sageTint, foreground: Theme.sageInk, fontSize: 13,
                                 horizontalPadding: 10, verticalPadding: 6, radius: 14)
                    }
                }
                .accessibilityElement(children: .combine)
            }
            Text(foundLine(imported))
                .sqFont(13)
                .foregroundStyle(Theme.text3)
        }
    }

    /// "From 48 Pages you like · Atlanta, Georgia"
    private func foundLine(_ imported: FacebookImport) -> String {
        var parts = [imported.likedPages == 1 ? "From 1 Page you like" : "From \(imported.likedPages) Pages you like"]
        if let area = imported.homeArea { parts.append(area) }
        return parts.joined(separator: " · ")
    }

    // MARK: Friends on SideQuests

    private func friends(_ people: [UserSearchResult]) -> some View {
        VStack(alignment: .leading, spacing: 10) {
            Eyebrow(text: "FRIENDS ON SIDEQUESTS")
            VStack(spacing: 0) {
                ForEach(Array(people.enumerated()), id: \.element.id) { index, person in
                    if index > 0 { RowDivider(color: .white) }
                    friendRow(person)
                }
            }
            .background(Theme.cream, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
        }
    }

    private func friendRow(_ person: UserSearchResult) -> some View {
        HStack(spacing: 10) {
            Avatar(person: person.person, size: 36)
            VStack(alignment: .leading, spacing: 0) {
                Text(person.person.name)
                    .sqFont(15, .semibold)
                    .foregroundStyle(Theme.ink)
                    .lineLimit(1)
                if person.relation == .incoming {
                    Text("Sent you a request")
                        .sqFont(12)
                        .foregroundStyle(Theme.text3)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            friendButton(person)
        }
        .padding(.vertical, 10)
        .padding(.horizontal, 14)
    }

    @ViewBuilder private func friendButton(_ person: UserSearchResult) -> some View {
        let sending = model.befriending.contains(person.id)
        switch person.relation {
        case .none, .incoming:
            let title = person.relation == .incoming ? "Accept" : "Add"
            Button {
                Task { await model.befriend(person, env: env) }
            } label: {
                Text(title)
                    .foregroundStyle(sending ? Color.clear : Theme.ink)
                    .overlay {
                        if sending {
                            LoadingDots(color: Theme.ink, dotSize: 5)
                                .sqTransition(.pop)
                        }
                    }
            }
            .buttonStyle(.sqPill(fill: Theme.sage, foreground: Theme.ink, height: 32, fontSize: 13))
            .authHitHeight(32)
            .disabled(sending)
            .accessibilityLabel("\(title) \(person.person.name)")
            .accessibilityValue(sending ? "In progress" : "")
        case .outgoing:
            TagLabel(text: "Requested", fill: .white, foreground: Theme.text2, fontSize: 13,
                     horizontalPadding: 12, verticalPadding: 7, radius: 16)
                .transition(.opacity)
        case .friend:
            HStack(spacing: 4) {
                CheckGlyph(lineWidth: 2.6)
                    .frame(width: 12, height: 12)
                Text("Friends")
            }
            .sqFont(13, .semibold)
            .foregroundStyle(Theme.success)
            .transition(.opacity)
            .accessibilityElement(children: .combine)
        }
    }

    // MARK: Declined permissions

    private func declined(_ labels: [String]) -> some View {
        InfoBox(systemImage: "info.circle") {
            VStack(alignment: .leading, spacing: 6) {
                Text("You didn't share \(ListFormatter.localizedString(byJoining: labels)).")
                    .sqFont(14)
                    .foregroundStyle(Theme.ink)
                    .fixedSize(horizontal: false, vertical: true)
                Button("Share them") {
                    Task { await model.connect(env, rerequest: true) }
                }
                .buttonStyle(.sqLink(size: 14))
                .disabled(model.work != nil)
            }
        }
    }

    // MARK: Footer

    private var footer: some View {
        HStack {
            Button {
                Task { await model.refresh(env) }
            } label: {
                ZStack {
                    Text("Refresh").opacity(model.work == .refreshing ? 0 : 1)
                    if model.work == .refreshing {
                        LoadingDots(color: Theme.sageInk, dotSize: 5)
                            .sqTransition(.pop)
                    }
                }
            }
            .buttonStyle(.sqTintPill)
            .authHitHeight(32)
            .accessibilityLabel("Refresh from Facebook")
            .accessibilityValue(model.work == .refreshing ? "In progress" : "")
            Spacer(minLength: 8)
            Button {
                confirmDisconnect = true
            } label: {
                ZStack {
                    Text("Disconnect").opacity(model.work == .disconnecting ? 0 : 1)
                    if model.work == .disconnecting {
                        LoadingDots(color: Theme.danger, dotSize: 5)
                            .sqTransition(.pop)
                    }
                }
            }
            .buttonStyle(.sqLink(size: 14, color: Theme.danger))
            .frame(minHeight: Metrics.minTouch)
            .accessibilityLabel("Disconnect Facebook")
        }
        .disabled(model.work != nil)
    }

    // MARK: Helpers

    private func primaryButton(_ title: String, busy: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            ZStack {
                Text(title).opacity(busy ? 0 : 1)
                if busy {
                    LoadingDots(color: Theme.ink)
                        .sqTransition(.pop)
                }
            }
        }
        .buttonStyle(.sq(fill: Theme.sage, foreground: Theme.ink, height: 50))
        .disabled(model.work != nil)
        .accessibilityLabel(title)
        .accessibilityValue(busy ? "In progress" : "")
    }

    /// "Sep 25"
    private func dayText(_ date: Date) -> String {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US")
        formatter.timeZone = env.clock.timeZone
        formatter.setLocalizedDateFormatFromTemplate("MMMd")
        return formatter.string(from: date)
    }
}

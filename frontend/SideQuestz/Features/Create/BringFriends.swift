import SwiftUI

// Create › Bring friends: the row under Who's coming, the picker sheet, and Review's "With Maya and
// Dev". The picks are the flow's (`CreateFlowModel.invitees`); Start sends them as
// `invite_user_ids`, and the server puts them on the plan and in its group chat.

// MARK: - Vibe row

/// Create › Vibe › Who's coming: "Bring friends" with the faces of the friends picked, or "Add
/// friends" when there are none. Opens the picker. VoiceOver: "Bring friends, 2 added, button".
struct CreateBringFriendsRow: View {
    let model: CreateFlowModel

    /// Faces shown before the rest are counted ("+2").
    private static let faces = 4

    var body: some View {
        Button { model.friendsPickerOpen = true } label: {
            HStack(spacing: 10) {
                Image(systemName: "person.2.fill")
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(Theme.sageInk)
                    .frame(width: 32, height: 32)
                    .background(Theme.sageTint, in: Circle())
                Text("Bring friends")
                    .sqFont(15, .semibold)
                    .foregroundStyle(Theme.ink)
                    .frame(maxWidth: .infinity, alignment: .leading)
                trailing
                CreateChevron(direction: .right, size: 16)
                    .foregroundStyle(Theme.mutedStar)
            }
            .lineLimit(1)
            .padding(.leading, 10)
            .padding(.trailing, 14)
            .frame(minHeight: 52)
            .background(.white, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
            .contentShape(RoundedRectangle(cornerRadius: 14, style: .continuous))
        }
        .buttonStyle(.sqPressable)
        .animation(Motion.standard, value: model.invitees.map(\.id))
        .accessibilityLabel("Bring friends")
        .accessibilityValue(model.invitees.isEmpty ? "None added" : "\(model.invitees.count) added")
        .accessibilityHint("Opens your friends list")
        // The flow's note about Who's coming (switched to Friends only, or friends taken off).
        .onChange(of: model.whoNotice) { _, notice in
            if let notice { AccessibilityNotification.Announcement(notice).post() }
        }
    }

    @ViewBuilder private var trailing: some View {
        let people = model.invitees
        if people.isEmpty {
            Text("Add friends")
                .sqFont(14, .semibold)
                .foregroundStyle(Theme.sageInk)
                .transition(.opacity)
        } else {
            HStack(spacing: 6) {
                AvatarStack(people: people, size: 28, overlap: 8, maxCount: Self.faces)
                if people.count > Self.faces {
                    Text("+\(people.count - Self.faces)")
                        .sqFont(13, .semibold)
                        .foregroundStyle(Theme.text2)
                        .sqNumeric()
                }
            }
            .transition(.opacity)
        }
    }
}

// MARK: - Review line

/// Create › Review, under the summary: the faces of the friends coming and "With Maya and Dev".
struct CreateWithFriendsLine: View {
    let people: [PersonRef]

    var body: some View {
        HStack(spacing: 8) {
            AvatarStack(people: people, size: 24, overlap: 7, maxCount: 4)
            Text("With \(CreateFlowModel.names(people, limit: 3))")
                .sqFont(14, .semibold)
                .foregroundStyle(Theme.text2)
                .lineLimit(1)
                .truncationMode(.tail)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("With \(CreateFlowModel.names(people))")
    }
}

// MARK: - Picker

/// Create › Vibe › Bring friends (and Review › More options › Friends): your friends with their
/// presence dots and status lines, a search, and a checkmark on each friend coming along, up to 12.
/// Picks change the flow as you make them; "Done (2)" closes the sheet.
///
/// Loading: rows shaped like friends shimmer (the logo after a while). Failure: a sentence and "Try
/// again". VoiceOver reads each row as a switch ("Maya R., Free until 8 PM, on").
struct CreateFriendsPicker: View {
    @Bindable var model: CreateFlowModel
    let done: () -> Void

    static let style = SQSheetStyle(height: .fitted(max: 700))

    var body: some View {
        SheetScaffold(spacing: 14, shrinksToFit: true) {
            header
            SearchField(text: $model.friendQuery, placeholder: "Search your friends", fill: Theme.cream,
                        accessibilityLabel: "Search your friends")
                .submitLabel(.search)
            if !model.canInviteMore {
                Text("That's \(CreateFlowModel.maxInvites), the most one sidequest can bring.")
                    .sqFont(13)
                    .foregroundStyle(Theme.text3)
                    .fixedSize(horizontal: false, vertical: true)
                    .sqTransition(.rise)
            }
            ScrollView {
                content
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .padding(.bottom, 4)
            }
            .scrollBounceBehavior(.basedOnSize)
            .scrollDismissesKeyboard(.interactively)
            .sheetShrinks()
        }
        .animation(Motion.standard, value: model.friends.phase)
        .animation(Motion.standard, value: model.canInviteMore)
        .task { await model.loadFriends() }
    }

    // MARK: Header

    private var header: some View {
        HStack(alignment: .top, spacing: 12) {
            VStack(alignment: .leading, spacing: 1) {
                Text("Bring friends")
                    .sqFont(20, .bold)
                    .foregroundStyle(Theme.ink)
                    .accessibilityAddTraits(.isHeader)
                Text("They'll be on the plan and in its group chat once you start it.")
                    .sqFont(13)
                    .foregroundStyle(Theme.text3)
                    .fixedSize(horizontal: false, vertical: true)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            Button(action: done) {
                Text(model.invitees.isEmpty ? "Done" : "Done (\(model.invitees.count))")
                    .contentTransition(.numericText())
            }
            .buttonStyle(.sqPill)
            .animation(Motion.quick, value: model.invitees.count)
            .accessibilityLabel("Done")
            .accessibilityValue(model.invitees.isEmpty ? "" : "\(model.invitees.count) added")
            .accessibilityIdentifier("invite.done")
        }
    }

    // MARK: Content

    @ViewBuilder private var content: some View {
        switch model.friends {
        case .loading:
            card {
                ForEach(0..<3, id: \.self) { index in
                    if index > 0 { RowDivider(color: .white) }
                    CreateFriendSkeletonRow(nameWidth: [118, 96, 132][index], lineWidth: [92, 128, 80][index])
                }
            }
            .sqShimmer()
            .accessibilityElement()
            .accessibilityLabel("Loading your friends")
            .sqSlowLoading(lines: ["Loading your friends…"])
            .transition(.opacity)
        case .failed(let message):
            ErrorStateView(message: message, minHeight: 180) {
                Task { await model.loadFriends(force: true) }
            }
            .transition(.opacity)
        case .loaded(let all):
            let rows = model.friendResults
            if all.isEmpty {
                EmptyStateView(message: "No friends yet. Add some in Account › Friends, then bring them along.", minHeight: 140)
                    .transition(.opacity)
            } else if rows.isEmpty {
                EmptyStateView(message: "No friends match “\(model.friendQuery.trimmingCharacters(in: .whitespaces))”.", minHeight: 120)
                    .transition(.opacity)
            } else {
                card {
                    ForEach(Array(rows.enumerated()), id: \.element.id) { index, friend in
                        VStack(spacing: 0) {
                            if index > 0 { RowDivider(color: .white) }
                            row(friend)
                        }
                    }
                }
                .animation(Motion.standard, value: rows.map(\.id))
                .transition(.opacity)
            }
        }
    }

    private func card<Content: View>(@ViewBuilder _ content: () -> Content) -> some View {
        VStack(spacing: 0) { content() }
            .background(Theme.cream, in: RoundedRectangle(cornerRadius: 14, style: .continuous))
    }

    /// Photo (or initials) with the presence dot, name over the live status line, and the check.
    /// Past 12 picks the others can't be checked.
    private func row(_ friend: Friend) -> some View {
        let person = friend.person
        let isOn = model.isInvited(person.id)
        let available = isOn || model.canInviteMore
        let picked = Binding(get: { model.isInvited(person.id) }, set: { on in
            guard on != model.isInvited(person.id) else { return }
            withMotion(Motion.quick) { model.toggleInvite(person) }
        })
        return Button { picked.wrappedValue.toggle() } label: {
            HStack(spacing: 12) {
                Avatar(initials: person.initials, fill: person.color, size: 40, fontSize: 14, imageURL: person.photoURL,
                       statusColor: friend.activity.dotColor, statusSize: 12, statusBorder: Theme.cream, statusBorderWidth: 2)
                VStack(alignment: .leading, spacing: 0) {
                    Text(person.name)
                        .sqFont(15, .semibold)
                        .foregroundStyle(Theme.ink)
                        .createLine(15)
                    if !friend.statusLine.isEmpty {
                        Text(friend.statusLine)
                            .sqFont(12)
                            .foregroundStyle(Theme.text3)
                            .createLine(12)
                    }
                }
                .lineLimit(1)
                .frame(maxWidth: .infinity, alignment: .leading)
                CreateInviteCheck(isOn: isOn)
            }
            .padding(.vertical, 10)
            .padding(.horizontal, 14)
            .frame(minHeight: 62)
            .contentShape(Rectangle())
        }
        .buttonStyle(.sqPressable)
        .disabled(!available)
        .opacity(available ? 1 : 0.45)
        .sensoryFeedback(.selection, trigger: isOn)
        // A switch for VoiceOver and UI tests: "Maya R., Free until 8 PM", on / off.
        .accessibilityRepresentation {
            Toggle(isOn: picked) {
                Text([person.name, friend.statusLine].filter { !$0.isEmpty }.joined(separator: ", "))
            }
            .disabled(!available)
        }
        .accessibilityIdentifier("invite.\(person.id)")
    }
}

/// 24pt check: an empty ring, or sage with an ink check that pops in.
private struct CreateInviteCheck: View {
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

/// A row shaped like a friend: the avatar, a name and a status line, the check.
private struct CreateFriendSkeletonRow: View {
    let nameWidth: CGFloat
    let lineWidth: CGFloat

    var body: some View {
        HStack(spacing: 12) {
            Circle().fill(Theme.skeletonOnCream).frame(width: 40, height: 40)
            VStack(alignment: .leading, spacing: 7) {
                SkeletonBlock(width: nameWidth, height: 13, color: Theme.skeletonOnCream)
                SkeletonBlock(width: lineWidth, height: 10, color: Theme.skeletonOnCream)
            }
            .frame(maxWidth: .infinity, alignment: .leading)
            Circle().fill(Theme.skeletonOnCream).frame(width: 24, height: 24)
        }
        .padding(.vertical, 11)
        .padding(.horizontal, 14)
        .frame(minHeight: 62)
    }
}

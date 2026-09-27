import SwiftUI

/// Setup · 1 "Let's set up your profile": photo, name, email, password (+ rules), username,
/// date of birth with the age note. Continue stays blocked until the basics pass.
///
/// Once the account exists (Back from step 2, or setup resumed after sign-in) this step edits it:
/// the email shows read-only and the password fields are gone, since neither can change here.
struct SetupBasicsStep: View {
    @Environment(AppEnvironment.self) private var env
    @Bindable var draft: SetupDraft
    /// After a failed Continue: list the problems (they update as you type).
    let showsErrors: Bool
    let serverError: String?
    var focus: FocusState<AuthFocus?>.Binding
    let onEditPhoto: () -> Void
    @State private var showsBirthDate = false

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            SetupHeading(title: "Let's set up your profile")
            avatarRow
                .padding(.top, 6)

            AuthField(label: "Name", text: $draft.name, placeholder: "Your name", kind: .name,
                      bordered: false, focus: focus, field: .name)
                .submitLabel(.next)
                .onSubmit { focus.wrappedValue = draft.signedUp ? .username : .email }
                .padding(.top, 8)
            if draft.signedUp {
                SetupReadOnlyField(label: "Email", value: draft.trimmedEmail)
            } else {
                AuthField(label: "Email", text: $draft.email, placeholder: "you@school.edu", kind: .email,
                          bordered: false, focus: focus, field: .email)
                    .submitLabel(.next)
                    .onSubmit { focus.wrappedValue = .password }
                AuthField(label: "Create a password", text: $draft.password, placeholder: "At least 8 characters",
                          kind: .newPassword, revealed: $draft.revealsPasswords, revealsBoth: true, focus: focus, field: .password)
                    .submitLabel(.next)
                    .onSubmit { focus.wrappedValue = .confirm }
                AuthField(label: "Confirm password", text: $draft.confirm, placeholder: "Type it again",
                          kind: .newPassword, revealed: $draft.revealsPasswords, revealsBoth: true, focus: focus, field: .confirm)
                    .submitLabel(.next)
                    .onSubmit { focus.wrappedValue = .username }
                PasswordRulesView(password: draft.password, confirm: draft.confirm)
            }
            AuthField(label: "Username (optional)", text: $draft.username, placeholder: "@handle", kind: .username,
                      bordered: false, focus: focus, field: .username)
                .submitLabel(.done)
                .onSubmit { focus.wrappedValue = nil }
            birthDateField
            ageNote
                .id(SetupScrollTarget.ageNote)
            if !errorMessages.isEmpty {
                ErrorBox(messages: errorMessages)
                    .id(SetupScrollTarget.basicsErrors)
                    .sqTransition(.rise)
            }
        }
        // The error box rises in and its lines update as you fix things; the age note recolors.
        .authMotion(value: errorMessages)
        .authMotion(Motion.quick, value: tooYoung)
        .authMotion(Motion.quick, value: draft.birthDate == nil)
        .sqSheet(isPresented: $showsBirthDate) {
            BirthDateSheet(start: draft.birthDateStart(now: env.clock.now, calendar: env.clock.calendar),
                           latest: env.clock.now) { date in
                draft.setBirthDate(date, calendar: env.clock.calendar)
                showsBirthDate = false
            } cancel: {
                showsBirthDate = false
            }
        }
    }

    private var errorMessages: [String] {
        (showsErrors ? draft.basicsErrors : []) + (serverError.map { [$0] } ?? [])
    }

    // MARK: Photo

    private var avatarRow: some View {
        HStack(spacing: 14) {
            Avatar(initials: draft.initials, fill: draft.avatarColor.background, foreground: draft.avatarColor.foreground,
                   size: 72, fontSize: 24, fontWeight: .semibold, image: env.profileImage,
                   imageURL: env.profileImage == nil && draft.signedUp ? env.user?.photoURL : nil)
                .overlay {
                    if draft.initials.isEmpty && !hasPhoto {
                        SetupPersonGlyph(size: 72, color: draft.avatarColor.foreground)
                            .transition(.opacity)
                    }
                }
                .authMotion(Motion.quick, value: draft.initials.isEmpty)
                .accessibilityHidden(true)
            VStack(alignment: .leading, spacing: 6) {
                Button(hasPhoto ? "Change photo" : "Add a photo", action: onEditPhoto)
                    .buttonStyle(.sqPill)
                    .authMotion(Motion.quick, value: hasPhoto)
                    .authHitHeight(34)
                    .accessibilityHint("Optional. Friends see it on plans and in chats.")
                Text("Optional · friends see it on plans and in chats")
                    .sqFont(12)
                    .foregroundStyle(Theme.text3)
                    .authLineHeight(1.35, size: 12)
                    .accessibilityHidden(true)
            }
        }
    }

    private var hasPhoto: Bool {
        env.profileImage != nil || (draft.signedUp && env.user?.photoURL != nil)
    }

    // MARK: Date of birth

    /// "June 14, 2003", or nil until one is picked.
    private var birthDateText: String? {
        draft.birthDate.map { BirthDateSheet.text($0, clock: env.clock) }
    }

    /// The whole card is one button that opens the wheels (`BirthDateSheet`).
    private var birthDateField: some View {
        let card = RoundedRectangle(cornerRadius: Metrics.fieldRadius, style: .continuous)
        return Button {
            focus.wrappedValue = nil
            showsBirthDate = true
        } label: {
            VStack(alignment: .leading, spacing: 2) {
                Text("Date of birth")
                    .sqFont(12, relativeTo: .caption)
                    .foregroundStyle(Theme.text3)
                    .authLineHeight(1.35, size: 12)
                HStack(spacing: 8) {
                    Text(birthDateText ?? "Select date")
                        .sqFont(17)
                        .foregroundStyle(draft.birthDate == nil ? Theme.text3 : Theme.ink)
                        .lineLimit(1)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    CalendarGlyph(size: 18, lineWidth: 1.8)
                        .foregroundStyle(Theme.text3)
                        .frame(width: 18, height: 18)
                }
                .frame(minHeight: 22.95)
            }
            .padding(.horizontal, 14)
            .padding(.vertical, 10)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(.white, in: card)
            .contentShape(card)
        }
        .buttonStyle(.sqPressable)
        .accessibilityLabel("Date of birth")
        .accessibilityValue(birthDateText ?? "Not set")
        .accessibilityHint("Opens month, day and year wheels")
        .accessibilityIdentifier("setup.birthDate")
    }

    // MARK: Age note

    private var age: Int? { draft.age(on: env.clock.now, calendar: env.clock.calendar) }

    private var tooYoung: Bool { (age ?? 13) < 13 }

    private var ageNote: some View {
        HStack(alignment: .top, spacing: 8) {
            Image(systemName: "lock")
                .font(.system(size: 12.5, weight: .semibold))
                .foregroundStyle(tooYoung ? Theme.danger : Theme.sageInk)
                .frame(width: 16, height: 16)
                .padding(.top, 1)
            Text(Validation.ageNote(age: age))
                .sqFont(13)
                .foregroundStyle(Theme.ink)
                .authLineHeight(1.4, size: 13)
                .frame(maxWidth: .infinity, alignment: .leading)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(.vertical, 10)
        .padding(.horizontal, 12)
        .background(tooYoung ? Theme.dangerBg : Theme.sageTint, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        .accessibilityElement(children: .combine)
    }
}

/// A field card that can't be edited (the account's email after sign-up): same card as the
/// borderless fields, the value in `text2` and a small lock.
private struct SetupReadOnlyField: View {
    let label: String
    let value: String

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(label)
                .sqFont(12, relativeTo: .caption)
                .foregroundStyle(Theme.text3)
                .authLineHeight(1.35, size: 12)
            HStack(spacing: 8) {
                Text(value)
                    .sqFont(17)
                    .foregroundStyle(Theme.text2)
                    .lineLimit(1)
                    .truncationMode(.middle)
                    .frame(maxWidth: .infinity, alignment: .leading)
                Image(systemName: "lock")
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(Theme.text3)
            }
            .frame(minHeight: 22.95)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.white, in: RoundedRectangle(cornerRadius: Metrics.fieldRadius, style: .continuous))
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(label)
        .accessibilityValue(value)
        .accessibilityHint("Can't be changed here")
    }
}

/// Scroll anchors inside the Setup scroll view.
enum SetupScrollTarget: Hashable {
    case ageNote, basicsErrors, stepError
}

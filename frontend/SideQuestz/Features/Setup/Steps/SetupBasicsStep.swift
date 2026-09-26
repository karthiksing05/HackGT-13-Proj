import SwiftUI

/// Setup · 1 "Let's set up your profile": photo, name, email, password (+ rules), username,
/// date of birth with the age note. Continue stays blocked until the basics pass.
struct SetupBasicsStep: View {
    @Environment(AppEnvironment.self) private var env
    @Bindable var draft: SetupDraft
    /// After a failed Continue: list the problems (they update as you type).
    let showsErrors: Bool
    let serverError: String?
    var focus: FocusState<AuthFocus?>.Binding
    let onEditPhoto: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            SetupHeading(title: "Let's set up your profile",
                         subtitle: "This takes about a minute. It helps us pick things you'll actually like.")
            avatarRow
                .padding(.top, 6)

            AuthField(label: "Name", text: $draft.name, placeholder: "Your name", kind: .name,
                      bordered: false, focus: focus, field: .name)
                .submitLabel(.next)
                .onSubmit { focus.wrappedValue = .email }
                .padding(.top, 8)
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
            Text("Friends can find you by name or username.")
                .sqFont(12)
                .foregroundStyle(Theme.text3)
                .authLineHeight(1.35, size: 12)
        }
        // The error box rises in and its lines update as you fix things; the age note recolors.
        .authMotion(value: errorMessages)
        .authMotion(Motion.quick, value: tooYoung)
        .authMotion(Motion.quick, value: draft.birthDate == nil)
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

    private var birthDateBinding: Binding<Date> {
        Binding(
            get: { draft.birthDate ?? defaultBirthDate },
            set: { draft.birthDate = $0 }
        )
    }

    /// Where the calendar opens before a date is picked (20 years back).
    private var defaultBirthDate: Date {
        env.clock.calendar.date(byAdding: .year, value: -20, to: env.clock.now) ?? env.clock.now
    }

    private var birthDateField: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text("Date of birth")
                .sqFont(12, relativeTo: .caption)
                .foregroundStyle(Theme.text3)
                .authLineHeight(1.35, size: 12)
                .accessibilityHidden(true)
            ZStack(alignment: .leading) {
                DatePicker("Date of birth", selection: birthDateBinding, in: ...env.clock.now, displayedComponents: .date)
                    .datePickerStyle(.compact)
                    .labelsHidden()
                    // Until a date is picked, the (still tappable) picker hides behind a placeholder.
                    .opacity(draft.birthDate == nil ? 0.02 : 1)
                    .accessibilityLabel("Date of birth")
                    .accessibilityHint(draft.birthDate == nil ? "Not set yet" : "")
                if draft.birthDate == nil {
                    // Covers the picker's pill; taps fall through to it.
                    Text("Select date")
                        .sqFont(17)
                        .foregroundStyle(Theme.text3)
                        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .leading)
                        .background(.white)
                        .allowsHitTesting(false)
                        .accessibilityHidden(true)
                        .transition(.opacity)
                }
            }
            .padding(.vertical, -6)
            .frame(minHeight: 22.95)
            .environment(\.timeZone, env.clock.timeZone)
            .environment(\.calendar, env.clock.calendar)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.white, in: RoundedRectangle(cornerRadius: Metrics.fieldRadius, style: .continuous))
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

/// Scroll anchors inside the Setup scroll view.
enum SetupScrollTarget: Hashable {
    case ageNote, basicsErrors, stepError
}

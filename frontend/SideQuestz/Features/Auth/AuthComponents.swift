import SwiftUI

// Building blocks shared by Login, Forgot password and Profile setup. They reproduce the
// prototype's form metrics exactly (CSS line boxes, 1pt borders drawn outside the padding).

// MARK: - CSS line height

/// Natural line heights (× point size) of the two app fonts.
enum AuthFontMetrics {
    /// SF Pro: `UIFont.systemFont(ofSize: 17).lineHeight` = 20.29.
    static let sf: CGFloat = 1.1934
    /// JetBrains Mono: ascender 1020 + descender 300 per 1000 em.
    static let mono: CGFloat = 1.32
}

extension View {
    /// Matches a CSS `line-height` multiplier: the extra leading goes between lines and half of it
    /// above and below the text, so text boxes line up with the prototype's.
    func authLineHeight(_ multiplier: CGFloat, size: CGFloat, mono: Bool = false) -> some View {
        let leading = (multiplier - (mono ? AuthFontMetrics.mono : AuthFontMetrics.sf)) * size
        return lineSpacing(max(0, leading)).padding(.vertical, leading / 2)
    }

    /// Keeps a ≥44pt touch target while laying out at the prototype's smaller `visualHeight`.
    func authHitHeight(_ visualHeight: CGFloat) -> some View {
        padding(.vertical, -max(0, Metrics.minTouch - visualHeight) / 2)
    }
}

// MARK: - Focus

/// Which field has the keyboard in the auth and setup forms.
enum AuthFocus: Hashable {
    case name, email, password, confirm, username, code
}

// MARK: - Field card

/// The prototype's field card: a 12pt grey label over a 17pt input, white, radius 14. Bordered
/// cards (passwords, Login, Forgot) have a 1pt `line` border that turns `errorBorder` on errors.
struct AuthField: View {
    enum Kind {
        case name, email, username, password, newPassword, code
    }

    let label: String
    @Binding var text: String
    var placeholder = ""
    var kind: Kind = .name
    /// Password cards: the "Show"/"Hide" link. Share one binding to toggle New + Confirm together.
    var revealed: Binding<Bool>? = nil
    var revealsBoth = false
    var bordered = true
    var hasError = false
    var focus: FocusState<AuthFocus?>.Binding
    let field: AuthFocus

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(label)
                .sqFont(12, relativeTo: .caption)
                .foregroundStyle(Theme.text3)
                .authLineHeight(1.35, size: 12)
                .onTapGesture { focus.wrappedValue = field }
                .accessibilityHidden(true)
            HStack(spacing: 8) {
                input
                if let revealed, isPassword {
                    revealButton(revealed)
                }
            }
            .frame(minHeight: kind == .code ? 43.6 : 22.95)
        }
        // CSS: 8pt padding + 1pt border (bordered) or 10pt padding (borderless).
        .padding(.horizontal, bordered ? 15 : 14)
        .padding(.vertical, bordered ? 9 : 10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(.white, in: RoundedRectangle(cornerRadius: Metrics.fieldRadius, style: .continuous))
        .overlay {
            if bordered || hasError {
                RoundedRectangle(cornerRadius: Metrics.fieldRadius, style: .continuous)
                    .strokeBorder(hasError ? Theme.errorBorder : Theme.line, lineWidth: 1)
            }
        }
    }

    private var isPassword: Bool { kind == .password || kind == .newPassword }

    @ViewBuilder private var input: some View {
        Group {
            if isPassword && !(revealed?.wrappedValue ?? false) {
                SecureField("", text: $text, prompt: prompt)
            } else {
                TextField("", text: $text, prompt: prompt)
            }
        }
        .focused(focus, equals: field)
        .modifier(AuthInputStyle(kind: kind))
        .accessibilityLabel(label)
    }

    private var prompt: Text {
        Text(placeholder).foregroundStyle(Theme.text3)
    }

    private func revealButton(_ revealed: Binding<Bool>) -> some View {
        Button {
            let hadFocus = focus.wrappedValue == field
            revealed.wrappedValue.toggle()
            // Swapping SecureField ⇄ TextField drops focus; hand it back.
            if hadFocus { DispatchQueue.main.async { focus.wrappedValue = field } }
        } label: {
            Text(revealed.wrappedValue ? "Hide" : "Show")
                .sqFont(13, .semibold)
                .foregroundStyle(Theme.sageInk)
                .frame(minWidth: Metrics.minTouch, minHeight: Metrics.minTouch, alignment: .trailing)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .authHitHeight(22)
        .accessibilityLabel(revealed.wrappedValue ? (revealsBoth ? "Hide passwords" : "Hide password")
                                                  : (revealsBoth ? "Show passwords" : "Show password"))
    }
}

/// Font, keyboard and autofill per field kind.
private struct AuthInputStyle: ViewModifier {
    let kind: AuthField.Kind

    func body(content: Content) -> some View {
        switch kind {
        case .code:
            content
                .font(.mono(30, .bold, relativeTo: .title))
                .tracking(10)
                .foregroundStyle(Theme.ink)
                .keyboardType(.numberPad)
                .textContentType(.oneTimeCode)
                .padding(.top, 4)
        case .name:
            content.sqFont(17).foregroundStyle(Theme.ink)
                .textContentType(.name)
                .textInputAutocapitalization(.words)
                .autocorrectionDisabled()
        case .email:
            content.sqFont(17).foregroundStyle(Theme.ink)
                .textContentType(.emailAddress)
                .keyboardType(.emailAddress)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
        case .username:
            // The @handle is a social name, not the sign-in identifier (that's the email), so it
            // must not be `.username`: AutoFill would save and suggest the handle as the login.
            content.sqFont(17).foregroundStyle(Theme.ink)
                .textContentType(.nickname)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
        case .password:
            content.sqFont(17).foregroundStyle(Theme.ink)
                .textContentType(.password)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
        case .newPassword:
            content.sqFont(17).foregroundStyle(Theme.ink)
                .textContentType(.newPassword)
                .textInputAutocapitalization(.never)
                .autocorrectionDisabled()
        }
    }
}

// MARK: - Buttons and text

/// Full-width 52pt button with a spinner while its request runs. Sage by default; the grey
/// "not yet valid" fill keeps ink text (never white on sage).
struct AuthPrimaryButton: View {
    let title: String
    var busy = false
    var fill: Color = Theme.sage
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            ZStack {
                Text(title).opacity(busy ? 0 : 1)
                if busy {
                    ProgressView().tint(Theme.ink)
                }
            }
        }
        .buttonStyle(.sq(fill: fill, foreground: Theme.ink))
        .disabled(busy)
        .accessibilityLabel(title)
        .accessibilityValue(busy ? "In progress" : "")
    }
}

/// "New here? **Create an account**" — text2 prompt with a sageInk Semibold action.
struct AuthPromptButton: View {
    let prompt: String
    let action: String
    var fontSize: CGFloat = 15
    var height: CGFloat = 40
    let onTap: () -> Void

    var body: some View {
        Button(action: onTap) {
            (Text(prompt + " ").foregroundStyle(Theme.text2)
                + Text(action).foregroundStyle(Theme.sageInk).fontWeight(.semibold))
                .sqFont(fontSize)
                .multilineTextAlignment(.center)
                .frame(maxWidth: .infinity, minHeight: max(height, Metrics.minTouch))
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .authHitHeight(height)
    }
}

/// Centered sageInk Semibold text button ("Use a different email").
struct AuthLinkButton: View {
    let title: String
    var fontSize: CGFloat = 14
    var height: CGFloat = 36
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Text(title)
                .sqFont(fontSize, .semibold)
                .foregroundStyle(Theme.sageInk)
                .frame(maxWidth: .infinity, minHeight: max(height, Metrics.minTouch))
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .authHitHeight(height)
    }
}

/// Red one-line form error (13pt `danger`), announced to VoiceOver when it appears.
struct AuthErrorText: View {
    let message: String

    var body: some View {
        Text(message)
            .sqFont(13)
            .foregroundStyle(Theme.danger)
            .authLineHeight(1.35, size: 13)
            .frame(maxWidth: .infinity, alignment: .leading)
            .onAppear { AccessibilityNotification.Announcement(message).post() }
            .onChange(of: message) { _, new in AccessibilityNotification.Announcement(new).post() }
    }
}

/// Forgot password + Setup header: 90pt sides and a centered middle, 44 tall, 50pt from the top of
/// the screen (the prototype's `padding: 50px 12px 0` grid). Empty sides keep their width.
struct AuthFlowHeader<Leading: View, Center: View, Trailing: View>: View {
    @ViewBuilder var leading: Leading
    @ViewBuilder var center: Center
    @ViewBuilder var trailing: Trailing

    var body: some View {
        HStack(spacing: 0) {
            ZStack(alignment: .leading) {
                Color.clear
                leading
            }
            .frame(width: 90)
            center
                .frame(maxWidth: .infinity)
            ZStack(alignment: .trailing) {
                Color.clear
                trailing
            }
            .frame(width: 90)
        }
        .frame(height: Metrics.minTouch)
        .padding(.horizontal, 12)
        .designTopPadding(50, minimum: 0)
    }
}

/// "‹ Back" for the Forgot password and Setup headers: sageInk 17, the chevron centered in a
/// 20pt box, 8pt from the button's leading edge (as in the prototype).
struct AuthBackButton: View {
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 2) {
                Image(systemName: "chevron.left")
                    .font(.system(size: 17, weight: .semibold))
                    .frame(width: 20, height: 20)
                Text("Back").sqFont(17)
            }
            .foregroundStyle(Theme.sageInk)
            .padding(.leading, 8)
            .frame(minWidth: Metrics.minTouch, minHeight: Metrics.minTouch, alignment: .leading)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Back")
    }
}

/// Setup + Forgot password step title: JetBrains Mono 28 ExtraBold, tracking −0.8.
struct AuthStepTitle: View {
    let text: String

    var body: some View {
        Text(text)
            .setupTitleStyle()
            .authLineHeight(1.35, size: 28, mono: true)
            .fixedSize(horizontal: false, vertical: true)
            .accessibilityAddTraits(.isHeader)
    }
}

/// Pads so content ends `designBottom` points above the screen's bottom edge (prototype
/// "padding-bottom: 36px"), counting the home-indicator safe area.
private struct AuthDesignBottomPadding: ViewModifier {
    @Environment(\.safeAreaBottom) private var safeBottom
    let designBottom: CGFloat
    let minimum: CGFloat

    func body(content: Content) -> some View {
        content.padding(.bottom, max(designBottom - safeBottom, minimum))
    }
}

extension View {
    func authDesignBottomPadding(_ designBottom: CGFloat, minimum: CGFloat = 0) -> some View {
        modifier(AuthDesignBottomPadding(designBottom: designBottom, minimum: minimum))
    }
}

/// Turns any thrown error into the short sentence we show under a form.
func authMessage(for error: Error, fallback: String = "Something went wrong. Try again.") -> String {
    (error as? LocalizedError)?.errorDescription ?? fallback
}

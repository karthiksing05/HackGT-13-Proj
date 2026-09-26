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
            // Always drawn (clear when borderless) so an error border fades in instead of popping.
            RoundedRectangle(cornerRadius: Metrics.fieldRadius, style: .continuous)
                .strokeBorder(hasError ? Theme.errorBorder : Theme.line, lineWidth: 1)
                .opacity(bordered || hasError ? 1 : 0)
                .authMotion(Motion.quick, value: hasError)
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

/// Full-width 52pt button whose label turns into `LoadingDots` while its request runs, and
/// cross-fades when the title changes ("Continue" → "Finish setup"). Sage by default; the grey
/// "not yet valid" fill keeps ink text (never white on sage).
struct AuthPrimaryButton: View {
    let title: String
    var busy = false
    var fill: Color = Theme.sage
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            ZStack {
                Text(title)
                    .opacity(busy ? 0 : 1)
                    .id(title)
                    .transition(.opacity)
                if busy {
                    LoadingDots(color: Theme.ink)
                        .sqTransition(.pop)
                }
            }
        }
        .buttonStyle(.sq(fill: fill, foreground: Theme.ink))
        .disabled(busy)
        .authMotion(Motion.quick, value: busy)
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

// MARK: - Motion

/// `.animation(_:value:)` on the kit's curves; with Reduce Motion it becomes `Motion.reduced`,
/// like `withMotion`.
private struct AuthMotionModifier<Value: Equatable>: ViewModifier {
    let animation: Animation
    let value: Value
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func body(content: Content) -> some View {
        content.animation(reduceMotion ? Motion.reduced : animation, value: value)
    }
}

extension View {
    /// Animates whatever changes inside this view when `value` changes (errors rising in, the
    /// layout below them moving down), respecting Reduce Motion.
    func authMotion<Value: Equatable>(_ animation: Animation = Motion.standard, value: Value) -> some View {
        modifier(AuthMotionModifier(animation: animation, value: value))
    }

    /// `.sqAppear(index)` only when `animated` (e.g. rows arriving with their first load, not rows
    /// rebuilt later from data that was already on screen).
    func authArrive(_ index: Int, animated: Bool) -> some View {
        modifier(AuthArriveModifier(index: index, animated: animated))
    }
}

private struct AuthArriveModifier: ViewModifier {
    let index: Int
    /// Fixed when the row is created, so later renders don't re-trigger it.
    @State private var animated: Bool

    init(index: Int, animated: Bool) {
        self.index = index
        _animated = State(initialValue: animated)
    }

    func body(content: Content) -> some View {
        if animated {
            content.sqAppear(index)
        } else {
            content
        }
    }
}

// MARK: - Loading

/// Like the kit's `LoadableView`, but the loading state is a skeleton that echoes this section's own
/// layout (shimmering while `shimmers`), cross-fading to the content or an error. A first load
/// that's still running after `slowAfter` swaps the skeleton for the logo loader (`SlowLoading`);
/// the S is 32pt by default, since these are cards. Pass the content as one view (a `VStack` of
/// rows): this container overlaps its children.
struct AuthLoadable<Value, Skeleton: View, Content: View>: View {
    let state: Loadable<Value>
    var minHeight: CGFloat = 120
    /// Off while the screen isn't visible (tabs stay alive underneath), so nothing animates offscreen
    /// and the slow-loading wait doesn't start until the card can be seen.
    var shimmers = true
    var slowAfter: Duration = SlowLoading.threshold
    var slowLines: [String] = []
    var slowLogoSize: CGFloat = 32
    let retry: () -> Void
    @ViewBuilder var skeleton: () -> Skeleton
    @ViewBuilder var content: (Value) -> Content

    var body: some View {
        ZStack(alignment: .top) {
            switch state {
            case .loading:
                skeleton()
                    .sqShimmer(active: shimmers)
                    .accessibilityElement(children: .ignore)
                    .accessibilityLabel("Loading")
                    .sqSlowLoading(shimmers, after: slowAfter, lines: slowLines, logoSize: slowLogoSize)
                    .transition(.opacity)
            case .failed(let message):
                ErrorStateView(message: message, minHeight: minHeight, retry: retry)
                    .transition(.opacity)
            case .loaded(let value):
                content(value)
                    .transition(.opacity)
            }
        }
        .animation(Motion.standard, value: state.phase)
    }
}

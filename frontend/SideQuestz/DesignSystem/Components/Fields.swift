import SwiftUI

/// A white field card: small grey label on top, 17pt input below (Login, Setup, Forgot, Add expense).
///
/// Attach `.focused($focus, equals: .email)` and `.onSubmit { … }` at the call site for
/// "Return advances through fields".
struct SQTextField: View {
    enum Kind {
        case text, name, email, username, newPassword, password, numericCode, decimal
    }

    let label: String
    @Binding var text: String
    var placeholder = ""
    var kind: Kind = .text
    /// Password fields: shows the "Show"/"Hide" link and controls secure entry. Share one binding
    /// between New + Confirm to toggle both.
    var revealed: Binding<Bool>? = nil
    /// Setup's Name/Email cards have no border; Login/password cards do.
    var bordered = true
    var hasError = false

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(label).sqFont(12, relativeTo: .caption).foregroundStyle(Theme.text3)
                .frame(minHeight: 12 * 1.35, alignment: .leading)
            HStack(spacing: 8) {
                input
                if let revealed, isPassword {
                    Button(revealed.wrappedValue ? "Hide" : "Show") { revealed.wrappedValue.toggle() }
                        .buttonStyle(.plain)
                        .sqFont(13, .semibold)
                        .foregroundStyle(Theme.sageInk)
                        .accessibilityLabel(revealed.wrappedValue ? "Hide password" : "Show password")
                }
            }
            // CSS line box of a 17pt input (30pt Mono for the code field).
            .frame(minHeight: kind == .numericCode ? 43.6 : 22.95)
        }
        // CSS: 8pt padding + 1pt border when bordered, 10pt padding when not.
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
        .sqFont(kind == .numericCode ? 30 : 17)
        .foregroundStyle(Theme.ink)
        .textContentType(contentType)
        .keyboardType(keyboard)
        .textInputAutocapitalization(kind == .name || kind == .text ? .words : .never)
        .autocorrectionDisabled(kind != .text && kind != .name)
        .accessibilityLabel(label)
    }

    private var prompt: Text { Text(placeholder).foregroundStyle(Theme.text3) }

    private var contentType: UITextContentType? {
        switch kind {
        case .name: .name
        case .email: .emailAddress
        case .username: .username
        case .newPassword: .newPassword
        case .password: .password
        case .numericCode: .oneTimeCode
        case .text, .decimal: nil
        }
    }

    private var keyboard: UIKeyboardType {
        switch kind {
        case .email: .emailAddress
        case .numericCode: .numberPad
        case .decimal: .decimalPad
        default: .default
        }
    }
}

/// Multi-line input on the `field` color with a 1pt `line` border, radius 12 (Setup answers,
/// Event notes, Rate note). Optional label/header row on top.
struct SQTextArea: View {
    @Binding var text: String
    var placeholder: String
    var minLines = 3
    var fontSize: CGFloat = 15
    var fill: Color = Theme.field
    var bordered = true

    var body: some View {
        TextField("", text: $text, prompt: Text(placeholder).foregroundStyle(Theme.text3), axis: .vertical)
            .lineLimit(minLines...8)
            .sqFont(fontSize)
            .foregroundStyle(Theme.ink)
            .padding(.horizontal, 12)
            .padding(.vertical, 10)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(fill, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
            .overlay {
                if bordered {
                    RoundedRectangle(cornerRadius: 12, style: .continuous).strokeBorder(Theme.line, lineWidth: 1)
                }
            }
    }
}

/// Search field: magnifier + 15pt input, 40–42 tall, radius 12 (Create › Where, Friends, Forum area).
struct SearchField: View {
    @Binding var text: String
    var placeholder: String
    var fill: Color = .white
    var height: CGFloat = 42
    var accessibilityLabel = "Search"

    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass")
                .font(.system(size: 12, weight: .semibold))
                .foregroundStyle(Theme.text3)
            TextField("", text: $text, prompt: Text(placeholder).foregroundStyle(Theme.text3))
                .sqFont(15)
                .foregroundStyle(Theme.ink)
                .autocorrectionDisabled()
                .accessibilityLabel(accessibilityLabel)
        }
        .padding(.horizontal, 12)
        .frame(height: height)
        .background(fill, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
    }
}

/// Red error box listing problems (Setup step 1, Add expense).
struct ErrorBox: View {
    let messages: [String]

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            ForEach(messages, id: \.self) { Text($0) }
        }
        .sqFont(14)
        .foregroundStyle(Theme.dangerText)
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.horizontal, 14)
        .padding(.vertical, 10)
        .background(Theme.dangerBg, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        .accessibilityElement(children: .combine)
        .accessibilityAddTraits(.isStaticText)
    }
}

/// Password rules checklist shared by Setup and Reset: 18pt circle, `lineStrong` when not met,
/// sageInk with a white check when met.
struct PasswordRulesView: View {
    let password: String
    let confirm: String

    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            ForEach(Validation.passwordRules(password, confirm)) { rule in
                HStack(spacing: 8) {
                    ZStack {
                        Circle().fill(rule.passed ? Theme.sageInk : Theme.lineStrong)
                        if rule.passed {
                            AnimatedCheck(lineWidth: 2.8).foregroundStyle(.white).frame(width: 14, height: 14)
                        }
                    }
                    .frame(width: 18, height: 18)
                    Text(rule.text)
                        .sqFont(13)
                        .foregroundStyle(rule.passed ? Theme.ink : Theme.text3)
                }
                .accessibilityElement(children: .ignore)
                .accessibilityLabel("\(rule.text), \(rule.passed ? "done" : "not yet")")
            }
        }
        .padding(.horizontal, 4)
        .padding(.vertical, 2)
        .animation(Motion.quick, value: Validation.passwordRules(password, confirm).map(\.passed))
    }
}

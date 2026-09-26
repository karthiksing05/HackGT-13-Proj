import Foundation

struct AuthTokens: Codable, Hashable {
    var accessToken: String
    var refreshToken: String
    var expiresAt: Date?
}

struct AuthResponse: Codable, Hashable {
    var user: User
    var tokens: AuthTokens
}

struct SignupRequest: Codable, Hashable {
    var name: String
    var email: String
    var password: String
    var username: String?
    var dateOfBirth: Date?
}

/// PATCH /me
struct UserPatch: Codable, Hashable {
    var name: String?
    var username: String?
    var dateOfBirth: Date?
    var status: PresenceStatus?
}

/// Shared validation for Login, Setup and Forgot password (exact copy from GUI_PLAN.md).
enum Validation {
    static func isValidEmail(_ value: String) -> Bool {
        let v = value.trimmingCharacters(in: .whitespacesAndNewlines)
        return v.range(of: #"^[^\s@]+@[^\s@]+\.[^\s@]+$"#, options: .regularExpression) != nil
    }

    struct PasswordRule: Identifiable, Hashable {
        let text: String
        let passed: Bool
        var id: String { text }
    }

    /// "At least 8 characters", "Includes a number", "Both passwords match".
    static func passwordRules(_ password: String, _ confirm: String) -> [PasswordRule] {
        [
            PasswordRule(text: "At least 8 characters", passed: password.count >= 8),
            PasswordRule(text: "Includes a number", passed: password.rangeOfCharacter(from: .decimalDigits) != nil),
            PasswordRule(text: "Both passwords match", passed: !password.isEmpty && password == confirm),
        ]
    }

    static func passwordRulesPass(_ password: String, _ confirm: String) -> Bool {
        passwordRules(password, confirm).allSatisfy(\.passed)
    }

    /// Age on the demo's "today" (or the live date) for the Setup age note.
    static func age(birthDate: Date, on today: Date, calendar: Calendar) -> Int {
        calendar.dateComponents([.year], from: birthDate, to: today).year ?? 0
    }

    /// Setup step 1 age note copy.
    static func ageNote(age: Int?) -> String {
        guard let age else { return "Only used to recommend age-appropriate events, like 21+ nights. Never shown to others." }
        if age < 13 { return "You need to be 13 or older to use SideQuests." }
        if age < 18 { return "Age \(age): we'll only suggest all-ages events and hide 18+ and 21+ ones. Never shown to others." }
        if age < 21 { return "Age \(age): we'll hide 21+ events (bars, some concerts). Never shown to others." }
        return "Age \(age): 21+ events can show up in your suggestions. Never shown to others."
    }

    static func ageBracket(age: Int?) -> AgeBracket {
        guard let age else { return .adult }
        if age < 13 { return .under13 }
        if age < 18 { return .teen }
        if age < 21 { return .under21 }
        return .adult
    }
}

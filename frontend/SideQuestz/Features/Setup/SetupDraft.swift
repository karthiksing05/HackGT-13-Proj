import Foundation
import Observation

/// Everything typed or picked during Profile setup, shared by the five steps.
@Observable
final class SetupDraft {
    // MARK: Step 1 · basics
    var name = ""
    var email = ""
    var password = ""
    var confirm = ""
    var username = ""
    var birthDate: Date?
    /// One Show/Hide for both password fields.
    var revealsPasswords = false
    /// Initials color picked in the Photo sheet (sent to the server once the account exists).
    var avatarColor: AvatarColor = .ink
    /// `signup` succeeded — going Back to step 1 then edits the account instead.
    var signedUp = false
    /// A photo or color picked before sign-up that still has to reach the server.
    var lookPending = false

    // MARK: Step 2 · calendars (server state)
    var integrations: Loadable<[Integration]> = .loading
    var hasCalendar: Bool { integrations.value?.contains(where: \.connected) ?? false }

    // MARK: Steps 3–5 · saved with `PUT /me/preferences`
    var preferences = Preferences()

    // MARK: Derived

    var trimmedName: String { name.trimmingCharacters(in: .whitespacesAndNewlines) }
    var trimmedEmail: String { email.trimmingCharacters(in: .whitespacesAndNewlines) }

    /// "@jordan lee " → "jordanlee"; nil when empty.
    var cleanUsername: String? {
        let handle = username.filter { !$0.isWhitespace && $0 != "@" }
        return handle.isEmpty ? nil : handle
    }

    /// Step 1 problems, in the order and wording of GUI_PLAN.md §7.4.
    var basicsErrors: [String] {
        var errors: [String] = []
        if trimmedName.isEmpty { errors.append("Add your name.") }
        if !Validation.isValidEmail(trimmedEmail) { errors.append("Enter a valid email address.") }
        if !Validation.passwordRulesPass(password, confirm) {
            errors.append("Your password needs 8+ characters, a number, and both entries must match.")
        }
        return errors
    }

    func age(on now: Date, calendar: Calendar) -> Int? {
        birthDate.map { Validation.age(birthDate: $0, on: now, calendar: calendar) }
    }

    /// Initials for the avatar and the Photo sheet (JL like the demo user until a name is typed).
    var initials: String { Initials.from(trimmedName, fallback: "JL") }

    /// Step 5 answers, keyed like `Preferences.answers`.
    func answer(_ key: String) -> String { preferences.answers[key] ?? "" }

    func setAnswer(_ key: String, _ value: String) {
        preferences.answers[key] = value.isEmpty ? nil : value
    }
}

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
    /// The account exists (`signup` succeeded, or setup resumed after sign-in). Step 1 then edits
    /// it: the email shows read-only and the password fields are gone (neither can change there).
    var signedUp = false
    /// A photo or color picked before sign-up that still has to reach the server.
    var lookPending = false

    // MARK: Step 2 · calendars (server state)
    var integrations: Loadable<[Integration]> = .loading
    var hasCalendar: Bool { integrations.value?.contains(where: \.connected) ?? false }

    // MARK: Step 2 · Facebook (fills step 3)
    /// The Facebook import made during this setup.
    var facebookImport: FacebookImport?
    /// The ratings it filled in, with their values: disconnecting takes back only the ones you
    /// haven't changed since.
    var facebookFilled: [TripType: Int] = [:]

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

    /// Step 1 problems, in the order and wording of GUI_PLAN.md §7.4. Once the account exists only
    /// the fields step 1 can still change are checked.
    var basicsErrors: [String] {
        var errors: [String] = []
        if trimmedName.isEmpty { errors.append("Add your name.") }
        guard !signedUp else { return errors }
        if !Validation.isValidEmail(trimmedEmail) { errors.append("Enter a valid email address.") }
        if !Validation.passwordRulesPass(password, confirm) {
            errors.append("Your password needs 8+ characters, a number, and both entries must match.")
        }
        return errors
    }

    func age(on now: Date, calendar: Calendar) -> Int? {
        birthDate.map { Validation.age(birthDate: $0, on: now, calendar: calendar) }
    }

    /// Where the date-of-birth wheels start: the date already picked, or the same day 20 years back.
    func birthDateStart(now: Date, calendar: Calendar) -> Date {
        if let birthDate { return birthDate }
        return calendar.startOfDay(for: calendar.date(byAdding: .year, value: -20, to: now) ?? now)
    }

    /// Done in the date-of-birth sheet: the day the wheels show, at midnight in the app's time zone
    /// (how `date_of_birth` is sent).
    func setBirthDate(_ shown: Date, calendar: Calendar) {
        birthDate = calendar.startOfDay(for: shown)
    }

    /// Initials for the avatar and the Photo sheet; empty until a name is typed (a person glyph
    /// stands in, never someone else's initials).
    var initials: String { Initials.from(trimmedName, fallback: "") }

    /// Setup resumed after sign-in (`setup_complete` false): the account exists, so start from it.
    func resume(from user: User) {
        name = user.name
        email = user.email
        username = user.username ?? ""
        avatarColor = user.avatarColor
        signedUp = true
    }

    /// Fills the likes you haven't rated with the import's suggestions (never replaces your own).
    func useFacebook(_ imported: FacebookImport) {
        let result = preferences.merging(imported.suggestedRatings, overwrite: false)
        preferences = result.preferences
        for type in result.changed { facebookFilled[type] = result.preferences.ratings[type] }
        facebookImport = imported
    }

    /// Facebook disconnected during setup: takes back the ratings it filled that you haven't changed.
    func dropFacebook() {
        for (type, value) in facebookFilled where preferences.ratings[type] == value {
            preferences.ratings[type] = nil
        }
        facebookFilled = [:]
        facebookImport = nil
    }

    /// Step 5 answers, keyed like `Preferences.answers`.
    func answer(_ key: String) -> String { preferences.answers[key] ?? "" }

    func setAnswer(_ key: String, _ value: String) {
        preferences.answers[key] = value.isEmpty ? nil : value
    }
}

import Observation
import SwiftUI

/// Facebook in Account (the Connected row and the Facebook sheet): the connection, the latest import
/// and what the person did with it. The server does everything that touches Facebook; this only calls
/// the API and keeps the screen in step.
@Observable
final class FacebookModel {
    enum Work: Equatable { case connecting, refreshing, applying, disconnecting }

    var connection: Loadable<FacebookConnection> = .loading
    var work: Work?
    /// A short sentence when something failed.
    var message: String?
    /// "Use these" saved the suggestions of the current import.
    var applied = false
    /// Friend buttons waiting for the server, by person id.
    var befriending: Set<String> = []

    var isConnected: Bool { connection.value?.connected ?? false }
    var lastImport: FacebookImport? { connection.value?.lastImport }
    /// Facebook is being read (the import after connecting, or a refresh): the sheet shows the S
    /// over what the server is doing meanwhile, and Setup's caption says it's still going.
    var isImporting: Bool { work == .connecting || work == .refreshing }

    /// Loads (or quietly reloads) the connection, and the saved preferences the changes are shown against.
    func load(_ env: AppEnvironment) async {
        async let prefs: Void = loadPreferencesIfNeeded(env)
        let result = await Loadable.run { try await env.api.facebookConnection() }
        _ = await prefs
        // Keep what's on screen if a background refresh fails.
        guard result.value != nil || connection.value == nil else { return }
        withMotion { connection = result }
    }

    /// Facebook's page, then the import. False when nothing changed (closed or declined there) or it failed.
    @discardableResult
    func connect(_ env: AppEnvironment, rerequest: Bool = false) async -> Bool {
        begin(.connecting)
        do {
            guard let imported = try await FacebookConnector.connect(env: env, rerequest: rerequest) else {
                end()
                return false
            }
            await loadPreferencesIfNeeded(env)
            var fresh = (try? await env.api.facebookConnection()) ?? FacebookConnection(connected: true)
            fresh.lastImport = imported
            withMotion(Motion.arrive) {
                connection = .loaded(fresh)
                applied = false
                work = nil
            }
            return true
        } catch {
            end(message: sentence(for: error, fallback: "Couldn't connect Facebook. Try again."))
            return false
        }
    }

    /// Reads Facebook again (new likes since last time).
    func refresh(_ env: AppEnvironment) async {
        begin(.refreshing)
        do {
            let imported = try await env.api.importFacebook()
            withMotion(Motion.arrive) {
                if var current = connection.value {
                    current.lastImport = imported
                    connection = .loaded(current)
                }
                applied = false
                work = nil
            }
        } catch {
            end(message: sentence(for: error, fallback: "Couldn't read Facebook. Try again."))
            // e.g. Facebook wants a new sign-in: the status says so, and the sheet explains it.
            await load(env)
            if connection.value?.needsReconnect == true { withMotion { message = nil } }
        }
    }

    /// What "Use these" would change: each trip type whose saved rating differs from the suggestion,
    /// in the order Setup asks them.
    func changes(against preferences: Preferences?) -> [FacebookRatingChange] {
        guard let suggestions = lastImport?.suggestedRatings else { return [] }
        let saved = preferences?.ratings ?? [:]
        return TripType.allCases.compactMap { type in
            guard let value = suggestions[type], saved[type] != value else { return nil }
            return FacebookRatingChange(type: type, from: saved[type], to: value)
        }
    }

    /// "Use these": the suggestions replace the saved ratings (the person saw each change first).
    func apply(_ env: AppEnvironment) async {
        guard let suggestions = lastImport?.suggestedRatings else { return }
        begin(.applying)
        do {
            let saved: Preferences
            if let current = env.preferences { saved = current } else { saved = try await env.api.preferences() }
            let merged = saved.merging(suggestions, overwrite: true).preferences
            try await env.api.savePreferences(merged)
            env.preferences = merged
            withMotion(Motion.arrive) {
                applied = true
                work = nil
            }
        } catch {
            end(message: sentence(for: error, fallback: "Couldn't update your likes. Try again."))
        }
    }

    /// Revokes SideQuests on Facebook and deletes the import. True when done.
    func disconnect(_ env: AppEnvironment) async -> Bool {
        begin(.disconnecting)
        do {
            try await env.api.disconnectFacebook()
            withMotion {
                connection = .loaded(FacebookConnection(connected: false))
                applied = false
                work = nil
            }
            return true
        } catch {
            end(message: sentence(for: error, fallback: "Couldn't disconnect Facebook. Try again."))
            return false
        }
    }

    /// Friends on SideQuests: "Add" sends a request, "Accept" accepts theirs. The row updates when
    /// the server says so.
    func befriend(_ person: UserSearchResult, env: AppEnvironment) async {
        guard !befriending.contains(person.id) else { return }
        withMotion(Motion.quick) {
            befriending.insert(person.id)
            message = nil
        }
        do {
            var updated = person
            switch person.relation {
            case .none:
                let request = try await env.api.sendFriendRequest(userId: person.person.id)
                updated.relation = .outgoing
                updated.requestId = request.id
            case .incoming:
                if let id = person.requestId { try await env.api.acceptFriendRequest(id: id) }
                updated.relation = .friend
                updated.requestId = nil
            case .outgoing, .friend:
                break
            }
            withMotion(Motion.arrive) {
                replace(updated)
                befriending.remove(person.id)
            }
        } catch {
            withMotion {
                befriending.remove(person.id)
                message = sentence(for: error, fallback: "Couldn't send that. Try again.")
            }
        }
    }

    // MARK: Helpers

    private func loadPreferencesIfNeeded(_ env: AppEnvironment) async {
        guard env.preferences == nil, let saved = try? await env.api.preferences() else { return }
        env.preferences = saved
    }

    private func replace(_ person: UserSearchResult) {
        guard var current = connection.value, var imported = current.lastImport,
              let index = imported.friendsOnApp.firstIndex(where: { $0.id == person.id }) else { return }
        imported.friendsOnApp[index] = person
        current.lastImport = imported
        connection = .loaded(current)
    }

    private func begin(_ kind: Work) {
        withMotion(Motion.quick) {
            work = kind
            message = nil
        }
    }

    private func end(message: String? = nil) {
        withMotion {
            work = nil
            self.message = message
        }
    }

    private func sentence(for error: Error, fallback: String) -> String {
        (error as? LocalizedError)?.errorDescription ?? fallback
    }
}

/// One row of "Suggested likes": Live music, Not rated → 5.
struct FacebookRatingChange: Identifiable, Hashable {
    let type: TripType
    let from: Int?
    let to: Int
    var id: TripType { type }
}

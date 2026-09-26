import Foundation
import Observation
import Security

/// Holds the session tokens and persists them in the Keychain.
@Observable
final class AuthStore {
    private(set) var tokens: AuthTokens?
    var isSignedIn: Bool { tokens != nil }

    @ObservationIgnored private let service: String
    @ObservationIgnored private let account = "session"

    init(service: String = "com.karthiksing05.SideQuestz.auth") {
        self.service = service
        self.tokens = Self.read(service: service, account: account)
    }

    func save(_ tokens: AuthTokens) {
        self.tokens = tokens
        guard let data = try? JSONEncoder().encode(tokens) else { return }
        let query: [String: Any] = [kSecClass as String: kSecClassGenericPassword,
                                    kSecAttrService as String: service,
                                    kSecAttrAccount as String: account]
        SecItemDelete(query as CFDictionary)
        var add = query
        add[kSecValueData as String] = data
        add[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlock
        SecItemAdd(add as CFDictionary, nil)
    }

    func clear() {
        tokens = nil
        let query: [String: Any] = [kSecClass as String: kSecClassGenericPassword,
                                    kSecAttrService as String: service,
                                    kSecAttrAccount as String: account]
        SecItemDelete(query as CFDictionary)
    }

    private static func read(service: String, account: String) -> AuthTokens? {
        let query: [String: Any] = [kSecClass as String: kSecClassGenericPassword,
                                    kSecAttrService as String: service,
                                    kSecAttrAccount as String: account,
                                    kSecReturnData as String: true,
                                    kSecMatchLimit as String: kSecMatchLimitOne]
        var result: AnyObject?
        guard SecItemCopyMatching(query as CFDictionary, &result) == errSecSuccess, let data = result as? Data else { return nil }
        return try? JSONDecoder().decode(AuthTokens.self, from: data)
    }
}

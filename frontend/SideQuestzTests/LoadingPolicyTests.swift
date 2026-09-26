import Foundation
import Testing
@testable import SideQuestz

/// The loading policy (`SlowLoading`): the S only ever takes a skeleton's place, and only once a
/// first load has run past the threshold; `-SQSlowLoadingAfter` sets that threshold.
@MainActor
struct LoadingPolicyTests {
    @Test func showsLogoTruthTable() {
        let threshold = SlowLoading.defaultThreshold
        let past = threshold + .milliseconds(1)
        let early = threshold - .milliseconds(1)
        // Only a running load with nothing to show yet, once the threshold is reached.
        #expect(SlowLoading.showsLogo(isLoading: true, hasContent: false, elapsed: past))
        #expect(SlowLoading.showsLogo(isLoading: true, hasContent: false, elapsed: threshold))
        #expect(!SlowLoading.showsLogo(isLoading: true, hasContent: false, elapsed: early))
        #expect(!SlowLoading.showsLogo(isLoading: true, hasContent: false, elapsed: .zero))
        // Content on screen (a reload, a merge) never goes back to the S.
        #expect(!SlowLoading.showsLogo(isLoading: true, hasContent: true, elapsed: past))
        #expect(!SlowLoading.showsLogo(isLoading: true, hasContent: true, elapsed: early))
        // Nothing loading: nothing to wait for.
        #expect(!SlowLoading.showsLogo(isLoading: false, hasContent: false, elapsed: past))
        #expect(!SlowLoading.showsLogo(isLoading: false, hasContent: false, elapsed: early))
        #expect(!SlowLoading.showsLogo(isLoading: false, hasContent: true, elapsed: past))
        #expect(!SlowLoading.showsLogo(isLoading: false, hasContent: true, elapsed: early))
    }

    @Test func thresholdMovesTheRule() {
        // 0 = at once; `never` = never.
        #expect(SlowLoading.showsLogo(isLoading: true, hasContent: false, elapsed: .zero, threshold: .zero))
        #expect(!SlowLoading.showsLogo(isLoading: true, hasContent: false, elapsed: .seconds(3600), threshold: SlowLoading.never))
        #expect(SlowLoading.showsLogo(isLoading: true, hasContent: false, elapsed: .milliseconds(500), threshold: .milliseconds(500)))
        #expect(!SlowLoading.showsLogo(isLoading: true, hasContent: false, elapsed: .milliseconds(499), threshold: .milliseconds(500)))
        #expect(SlowLoading.defaultThreshold == .seconds(2))
    }

    @Test func parsesTheLaunchArgument() {
        // Nothing, or not a non-negative number: the default.
        #expect(SlowLoading.parse(nil) == SlowLoading.defaultThreshold)
        #expect(SlowLoading.parse("") == SlowLoading.defaultThreshold)
        #expect(SlowLoading.parse("abc") == SlowLoading.defaultThreshold)
        #expect(SlowLoading.parse("-1") == SlowLoading.defaultThreshold)
        #expect(SlowLoading.parse("nan") == SlowLoading.defaultThreshold)
        #expect(SlowLoading.parse("inf") == SlowLoading.defaultThreshold)
        // Seconds, with or without a fraction; 0 = at once.
        #expect(SlowLoading.parse("0") == .zero)
        #expect(SlowLoading.parse("2") == .seconds(2))
        #expect(SlowLoading.parse(" 3 ") == .seconds(3))
        #expect(SlowLoading.parse("0.5") == .milliseconds(500))
        // Anything huge is "never" (and can't overflow a Duration).
        #expect(SlowLoading.parse("1e12") == SlowLoading.never)
        #expect(SlowLoading.parse("999999999999999999999") == SlowLoading.never)
        #expect(SlowLoading.never > .seconds(365 * 86_400))
    }
}

import SwiftUI

// PLACEHOLDER — replaced by the feature implementation.
struct SetupFlowView: View {
    let startStep: Int
    let isRedo: Bool

    var body: some View {
        VStack(spacing: 12) {
            Text("Profile setup").sectionStyle()
            Text("Coming soon").captionStyle(13)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Theme.cream.ignoresSafeArea())
    }
}

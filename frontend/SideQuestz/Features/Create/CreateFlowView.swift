import SwiftUI

// PLACEHOLDER — replaced by the feature implementation.
struct CreateFlowView: View {
    let draft: CreateDraft

    var body: some View {
        VStack(spacing: 12) {
            Text("Create").sectionStyle()
            Text("Coming soon").captionStyle(13)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Theme.cream.ignoresSafeArea())
    }
}

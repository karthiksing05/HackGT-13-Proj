import SwiftUI

// PLACEHOLDER — replaced by the feature implementation.
struct ForumView: View {

    var body: some View {
        VStack(spacing: 12) {
            Text("Forum").sectionStyle()
            Text("Coming soon").captionStyle(13)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Theme.cream.ignoresSafeArea())
    }
}

import SwiftUI

// PLACEHOLDER — replaced by the feature implementation.
struct GroupsView: View {

    var body: some View {
        VStack(spacing: 12) {
            Text("Groups").sectionStyle()
            Text("Coming soon").captionStyle(13)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(Theme.cream.ignoresSafeArea())
    }
}

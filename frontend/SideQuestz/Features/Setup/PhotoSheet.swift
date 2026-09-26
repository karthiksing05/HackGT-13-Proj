import PhotosUI
import SwiftUI
import UIKit

/// Profile photo sheet (GUI_PLAN.md §7.10), used by Setup step 1 and Account. Present it with
/// `sqSheet`. The photo shows immediately (`env.profileImage`) and uploads with `POST /me/photo`;
/// picking an initials color clears the photo.
struct PhotoSheet: View {
    @Environment(AppEnvironment.self) private var env

    let initials: String
    /// The initials color (Account: the user's; Setup: the draft's).
    @Binding var color: AvatarColor
    /// False in Setup before sign-up: keep changes on the device; Setup sends them after sign-up.
    var savesToServer = true
    let onDone: () -> Void

    @State private var pickerItem: PhotosPickerItem?
    @State private var showCamera = false
    @State private var uploading = false
    @State private var errorText: String?
    /// The last photo that failed to upload (for "Try again").
    @State private var failedImage: UIImage?

    var body: some View {
        SheetScaffold(spacing: 14) {
            Text("Profile photo")
                .font(.mono(22, .extraBold, relativeTo: .title2))
                .foregroundStyle(Theme.ink)
                .authLineHeight(1.35, size: 22, mono: true)
                .accessibilityAddTraits(.isHeader)

            preview
                .frame(maxWidth: .infinity)

            if uploading || errorText != nil {
                statusLine
            }

            HStack(spacing: 8) {
                takePhotoButton
                choosePhotoButton
            }
            if !CameraPicker.isAvailable {
                Text("This device has no camera. Choose a photo instead.")
                    .sqFont(12)
                    .foregroundStyle(Theme.text3)
                    .frame(maxWidth: .infinity)
                    .padding(.top, -6)
            }

            SetupEyebrow(text: "OR USE YOUR INITIALS")
            colorRow

            if hasPhoto {
                Button("Remove photo", action: removePhoto)
                    .buttonStyle(.sq(fill: .clear, foreground: Theme.danger, height: 44, fontSize: 15))
            }
            Button("Done", action: onDone)
                .buttonStyle(.sq(fill: Theme.ink, foreground: .white, height: 50, fontSize: 16))
        }
        .fullScreenCover(isPresented: $showCamera) {
            CameraPicker { image in use(image) }
                .ignoresSafeArea()
        }
        .onChange(of: pickerItem) { _, item in
            guard let item else { return }
            Task { await load(item) }
        }
    }

    // MARK: Pieces

    private var hasPhoto: Bool {
        env.profileImage != nil || (savesToServer && env.user?.photoURL != nil)
    }

    private var preview: some View {
        Avatar(initials: initials, fill: color.background, foreground: color.foreground, size: 120, fontSize: 40,
               fontWeight: .semibold, image: env.profileImage,
               imageURL: env.profileImage == nil && savesToServer ? env.user?.photoURL : nil)
            .overlay {
                if uploading {
                    Circle().fill(.black.opacity(0.25))
                    ProgressView().tint(.white)
                }
            }
            .accessibilityElement()
            .accessibilityLabel(hasPhoto ? "Your profile photo" : "Your initials on \(color.name.lowercased())")
    }

    @ViewBuilder private var statusLine: some View {
        if uploading {
            Text("Uploading…")
                .sqFont(13)
                .foregroundStyle(Theme.text3)
                .frame(maxWidth: .infinity)
        } else if let errorText {
            HStack(spacing: 6) {
                Text(errorText).foregroundStyle(Theme.danger)
                if failedImage != nil {
                    Button("Try again") {
                        if let failedImage { use(failedImage) }
                    }
                    .fontWeight(.semibold)
                    .foregroundStyle(Theme.sageInk)
                    .buttonStyle(.plain)
                }
            }
            .sqFont(13)
            .frame(maxWidth: .infinity)
            .onAppear { AccessibilityNotification.Announcement(errorText).post() }
        }
    }

    private var takePhotoButton: some View {
        Button {
            showCamera = true
        } label: {
            Label("Take photo", systemImage: "camera")
                .labelStyle(PhotoSheetButtonLabelStyle())
        }
        .buttonStyle(.sq(fill: Theme.sage, foreground: Theme.ink, height: 48, fontSize: 15))
        .disabled(!CameraPicker.isAvailable || uploading)
        .opacity(CameraPicker.isAvailable ? 1 : 0.5)
    }

    private var choosePhotoButton: some View {
        PhotosPicker(selection: $pickerItem, matching: .images, photoLibrary: .shared()) {
            Label("Choose photo", systemImage: "photo")
                .labelStyle(PhotoSheetButtonLabelStyle())
        }
        .buttonStyle(.sq(fill: Theme.cream, foreground: Theme.ink, height: 48, fontSize: 15))
        .disabled(uploading)
    }

    /// Five 52pt circles spread edge to edge; the selected one gets a 3pt white + 2pt sageInk ring.
    private var colorRow: some View {
        HStack(spacing: 0) {
            ForEach(Array(AvatarColor.allCases.enumerated()), id: \.element) { index, option in
                if index > 0 { Spacer(minLength: 8) }
                let selected = option == color && !hasPhoto
                Button {
                    pick(option)
                } label: {
                    Text(initials)
                        .font(.system(size: 17, weight: .semibold))
                        .foregroundStyle(option.foreground)
                        .frame(width: 52, height: 52)
                        .background(option.background, in: Circle())
                        .background {
                            if selected {
                                ZStack {
                                    Circle().fill(Theme.sageInk).padding(-5)
                                    Circle().fill(.white).padding(-3)
                                }
                            }
                        }
                        .contentShape(Circle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Use initials on \(option.name.lowercased())")
                .accessibilityAddTraits(selected ? .isSelected : [])
            }
        }
        .sensoryFeedback(.selection, trigger: color)
    }

    // MARK: Actions

    private func load(_ item: PhotosPickerItem) async {
        defer { pickerItem = nil }
        guard let data = try? await item.loadTransferable(type: Data.self), let image = UIImage(data: data) else {
            errorText = "That photo couldn't be opened. Try another one."
            failedImage = nil
            return
        }
        use(image)
    }

    private func use(_ image: UIImage) {
        env.profileImage = image
        errorText = nil
        failedImage = nil
        guard savesToServer else { return }
        guard let data = image.uploadJPEG() else {
            errorText = "That photo couldn't be used. Try another one."
            return
        }
        uploading = true
        Task {
            defer { uploading = false }
            do {
                env.user?.photoURL = try await env.api.uploadPhoto(data)
            } catch {
                errorText = "Your photo didn't upload."
                failedImage = image
            }
        }
    }

    private func pick(_ option: AvatarColor) {
        let hadServerPhoto = savesToServer && env.user?.photoURL != nil
        let previousColor = color
        color = option
        env.profileImage = nil
        errorText = nil
        failedImage = nil
        guard savesToServer else { return }
        if hadServerPhoto { env.user?.photoURL = nil }
        Task {
            do {
                try await env.api.setAvatarColor(option)
                if hadServerPhoto { try await env.api.deletePhoto() }
            } catch {
                color = previousColor
                errorText = "Couldn't save that. Try again."
            }
        }
    }

    private func removePhoto() {
        let previousURL = env.user?.photoURL
        env.profileImage = nil
        errorText = nil
        failedImage = nil
        guard savesToServer, previousURL != nil else { return }
        env.user?.photoURL = nil
        Task {
            do {
                try await env.api.deletePhoto()
            } catch {
                env.user?.photoURL = previousURL
                errorText = "Couldn't remove your photo. Try again."
            }
        }
    }
}

/// Icon + title with a 6pt gap (Take photo / Choose photo).
private struct PhotoSheetButtonLabelStyle: LabelStyle {
    func makeBody(configuration: Configuration) -> some View {
        HStack(spacing: 6) {
            configuration.icon.font(.system(size: 15, weight: .medium))
            configuration.title
        }
    }
}

#Preview {
    Color.clear
        .sqSheet(isPresented: .constant(true)) {
            PhotoSheet(initials: "JL", color: .constant(.ink), onDone: {})
        }
        .environment(AppEnvironment.preview())
        .environment(Router(phase: .main))
}

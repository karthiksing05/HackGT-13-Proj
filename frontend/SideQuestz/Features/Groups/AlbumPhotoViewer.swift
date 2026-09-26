import Photos
import SwiftUI
import UIKit

/// Thread › Album › a photo: full screen on `ink`, swipe between the album's photos. "Save" adds
/// the photo to the phone's library (add-only permission), "Share" opens the share sheet, and
/// "Delete" (only on photos you added) asks first, then removes it from the album for everyone
/// (`DELETE /groups/{id}/photos/{photoId}`).
///
/// Photos load from their `url` (or the bytes the demo keeps); the demo's color-only photos can't
/// be saved or shared, so those two buttons stay dimmed for them.
struct AlbumPhotoViewer: View {
    let groupId: String
    /// The album's photos in grid order (kept current by the album while this is open).
    let photos: [GroupPhoto]
    /// Photos picked on this device, by photo id (shown until the server's copy loads).
    let previews: [String: UIImage]
    let canDelete: (GroupPhoto) -> Bool
    let onDeleted: (String) -> Void
    let close: () -> Void

    @Environment(AppEnvironment.self) private var env

    @State private var selection: String
    /// Each photo's bytes once loaded (for display, Save and Share).
    @State private var files: [String: Data] = [:]
    @State private var failedLoads: Set<String> = []
    @State private var saving = false
    @State private var savedIds: Set<String> = []
    @State private var deleting = false
    @State private var confirmingDelete = false
    @State private var sharing: AlbumShareItem?
    @State private var note: String?

    init(groupId: String, photos: [GroupPhoto], startId: String, previews: [String: UIImage],
         canDelete: @escaping (GroupPhoto) -> Bool, onDeleted: @escaping (String) -> Void, close: @escaping () -> Void) {
        self.groupId = groupId
        self.photos = photos
        self.previews = previews
        self.canDelete = canDelete
        self.onDeleted = onDeleted
        self.close = close
        _selection = State(initialValue: startId)
    }

    private var current: GroupPhoto? { photos.first { $0.id == selection } ?? photos.first }
    private var position: Int { photos.firstIndex { $0.id == current?.id } ?? 0 }

    var body: some View {
        VStack(spacing: 0) {
            topBar
            TabView(selection: $selection) {
                ForEach(photos) { photo in
                    AlbumViewerPage(photo: photo, preview: previews[photo.id], file: files[photo.id],
                                    failed: failedLoads.contains(photo.id), index: position(of: photo), count: photos.count)
                        .task(id: photo.id) { await loadFile(for: photo) }
                        .tag(photo.id)
                }
            }
            .tabViewStyle(.page(indexDisplayMode: .never))
            .disabled(deleting)
            if let note {
                Text(note)
                    .sqFont(13)
                    .foregroundStyle(.white.opacity(0.85))
                    .multilineTextAlignment(.center)
                    .padding(.horizontal, Metrics.side)
                    .padding(.top, 8)
                    .sqTransition(.rise)
            }
            actionBar
        }
        .background(Theme.ink.ignoresSafeArea())
        .preferredColorScheme(.dark)
        .animation(Motion.standard, value: note)
        .onChange(of: selection) { _, _ in withMotion(Motion.quick) { note = nil } }
        // The album changed underneath (a refresh): stay on a photo that's still there.
        .onChange(of: photos.map(\.id)) { _, ids in
            guard !ids.contains(selection) else { return }
            if let first = ids.first { selection = first } else { close() }
        }
        .confirmationDialog("Delete this photo?", isPresented: $confirmingDelete, titleVisibility: .visible) {
            Button("Delete photo", role: .destructive) { if let current { delete(current) } }
        } message: {
            Text("It's removed from the album for everyone in the group.")
        }
        .sheet(item: $sharing) { item in
            SocialShareSheet(items: [item.image])
                .presentationDetents([.medium, .large])
                .ignoresSafeArea()
        }
        .accessibilityAction(.escape) { close() }
    }

    private func position(of photo: GroupPhoto) -> Int {
        photos.firstIndex { $0.id == photo.id } ?? 0
    }

    // MARK: Bars

    private var topBar: some View {
        HStack(spacing: 0) {
            Button(action: close) {
                Image(systemName: "xmark")
                    .font(.system(size: 17, weight: .semibold))
                    .foregroundStyle(.white)
                    .frame(width: Metrics.minTouch, height: Metrics.minTouch)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Close")
            Spacer(minLength: 8)
            VStack(spacing: 0) {
                Text(photos.isEmpty ? "" : "\(position + 1) of \(photos.count)")
                    .sqFont(15, .semibold)
                    .foregroundStyle(.white)
                    .sqNumeric()
                if let current {
                    Text("by \(current.byName)")
                        .sqFont(12)
                        .foregroundStyle(.white.opacity(0.7))
                }
            }
            .lineLimit(1)
            .animation(Motion.quick, value: selection)
            .accessibilityElement(children: .combine)
            .accessibilityAddTraits(.isHeader)
            Spacer(minLength: 8)
            Color.clear.frame(width: Metrics.minTouch, height: Metrics.minTouch)
        }
        .padding(.horizontal, 8)
        .padding(.top, 4)
    }

    private var actionBar: some View {
        let photo = current
        let file = photo.flatMap { files[$0.id] }
        let saved = photo.map { savedIds.contains($0.id) } ?? false
        let deletable = photo.map(canDelete) ?? false
        return HStack(spacing: 10) {
            Button { if let photo { save(photo) } } label: {
                ZStack {
                    if saving {
                        LoadingDots(color: Theme.ink, dotSize: 6)
                            .accessibilityHidden(true)
                            .transition(.opacity)
                    } else if saved {
                        HStack(spacing: 6) {
                            AnimatedCheck(lineWidth: 2.8, delay: 0.05).frame(width: 14, height: 14)
                            Text("Saved")
                        }
                        .transition(.opacity)
                    } else {
                        Label("Save", systemImage: "arrow.down.to.line").transition(.opacity)
                    }
                }
                .animation(Motion.quick, value: saving)
                .animation(Motion.quick, value: saved)
            }
            .buttonStyle(.sq(fill: Theme.sage, foreground: Theme.ink, height: 48, radius: 14, fontSize: 15))
            .disabled(file == nil || saving || saved || deleting)
            .opacity(file == nil ? 0.45 : 1)
            .accessibilityLabel(saved ? "Saved to Photos" : "Save to Photos")
            .accessibilityValue(saving ? "Loading" : "")

            Button { if let file, let image = UIImage(data: file) { sharing = AlbumShareItem(image: image) } } label: {
                Label("Share", systemImage: "square.and.arrow.up")
            }
            .buttonStyle(.sq(fill: .white.opacity(0.14), foreground: .white, height: 48, radius: 14, fontSize: 15))
            .disabled(file == nil || deleting)
            .opacity(file == nil ? 0.45 : 1)
            .accessibilityLabel("Share photo")

            if deletable {
                Button { confirmingDelete = true } label: {
                    SocialBusyLabel(isBusy: deleting, color: Theme.dangerText) {
                        Label("Delete", systemImage: "trash")
                    }
                }
                .buttonStyle(.sq(fill: Theme.dangerBg, foreground: Theme.dangerText, height: 48, radius: 14, fontSize: 15))
                .disabled(deleting)
                .accessibilityLabel("Delete photo")
                .accessibilityValue(deleting ? "Loading" : "")
                .sqTransition(.pop)
            }
        }
        .labelStyle(AlbumViewerLabelStyle())
        .padding(.horizontal, Metrics.side)
        .padding(.top, 12)
        .padding(.bottom, 8)
        .animation(Motion.quick, value: deletable)
    }

    // MARK: Loading

    /// The photo's bytes: the demo keeps them on the photo, the server serves them at `url`, and a
    /// photo picked here falls back to its preview.
    private func loadFile(for photo: GroupPhoto) async {
        guard files[photo.id] == nil else { return }
        if let bytes = photo.imageData {
            files[photo.id] = bytes
            return
        }
        if let url = photo.url {
            do {
                let (bytes, _) = try await URLSession.shared.data(from: url)
                guard UIImage(data: bytes) != nil else { throw APIError.decoding("Not an image") }
                withMotion { files[photo.id] = bytes }
            } catch {
                if !Task.isCancelled { withMotion { _ = failedLoads.insert(photo.id) } }
            }
            return
        }
        if let preview = previews[photo.id], let bytes = preview.jpegData(compressionQuality: 0.9) {
            files[photo.id] = bytes
        }
    }

    // MARK: Actions

    private func save(_ photo: GroupPhoto) {
        guard let bytes = files[photo.id], !saving else { return }
        saving = true
        withMotion(Motion.quick) { note = nil }
        Task {
            do {
                try await AlbumPhotoLibrary.save(bytes)
                withMotion(Motion.arrive) {
                    _ = savedIds.insert(photo.id)
                    saving = false
                }
            } catch {
                withMotion {
                    note = (error as? AlbumPhotoLibrary.Failure)?.message ?? "Couldn't save the photo. Try again."
                    saving = false
                }
            }
        }
    }

    /// After you confirm: the Delete button shows dots until the server removes it, then the viewer
    /// moves to the next photo (or closes after the last one) and the album drops it.
    private func delete(_ photo: GroupPhoto) {
        guard !deleting else { return }
        deleting = true
        withMotion(Motion.quick) { note = nil }
        Task {
            do {
                try await env.api.deleteGroupPhoto(groupId: groupId, photoId: photo.id)
                let index = position(of: photo)
                let remaining = photos.filter { $0.id != photo.id }
                deleting = false
                if remaining.isEmpty {
                    close()
                } else {
                    withMotion { selection = remaining[min(index, remaining.count - 1)].id }
                }
                onDeleted(photo.id)
            } catch {
                withMotion {
                    note = error.socialMessage
                    deleting = false
                }
            }
        }
    }
}

/// One photo, fitted to the screen: its bytes once loaded, the photo picked here while the server's
/// copy loads, loading dots, or the demo's color tile.
private struct AlbumViewerPage: View {
    let photo: GroupPhoto
    let preview: UIImage?
    let file: Data?
    let failed: Bool
    let index: Int
    let count: Int

    private var image: UIImage? {
        if let file, let image = UIImage(data: file) { return image }
        return preview
    }

    var body: some View {
        ZStack {
            if let image {
                Image(uiImage: image)
                    .resizable()
                    .scaledToFit()
                    .transition(.opacity)
            } else if photo.url != nil, !failed {
                LoadingDots(color: .white.opacity(0.8), dotSize: 8)
                    .transition(.opacity)
            } else if photo.url != nil {
                Text("Couldn't load this photo.")
                    .sqFont(15)
                    .foregroundStyle(.white.opacity(0.8))
                    .transition(.opacity)
            } else {
                RoundedRectangle(cornerRadius: 12, style: .continuous)
                    .fill(photo.placeholderHex.flatMap { Color(hexString: $0) } ?? Theme.segmentBg)
                    .aspectRatio(1, contentMode: .fit)
                    .padding(.horizontal, Metrics.side)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .animation(Motion.standard, value: file)
        .accessibilityElement()
        .accessibilityLabel("Photo by \(photo.byName), \(index + 1) of \(count)")
        .accessibilityAddTraits(.isImage)
    }
}

/// Icon beside a short label, for the viewer's three buttons.
private struct AlbumViewerLabelStyle: LabelStyle {
    func makeBody(configuration: Configuration) -> some View {
        HStack(spacing: 6) {
            configuration.icon.font(.system(size: 15, weight: .semibold))
            configuration.title
        }
    }
}

private struct AlbumShareItem: Identifiable {
    let id = UUID()
    let image: UIImage
}

/// Adds photos to the phone's library with add-only access (asked the first time).
enum AlbumPhotoLibrary {
    struct Failure: Error {
        let message: String
    }

    nonisolated static func save(_ data: Data) async throws {
        let status = await PHPhotoLibrary.requestAuthorization(for: .addOnly)
        guard status == .authorized || status == .limited else {
            throw Failure(message: "To save photos, allow SideQuests to add to Photos in Settings.")
        }
        try await PHPhotoLibrary.shared().performChanges {
            PHAssetCreationRequest.forAsset().addResource(with: .photo, data: data, options: nil)
        }
    }
}

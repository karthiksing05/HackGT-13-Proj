import PhotosUI
import SwiftUI
import UIKit

/// Thread › Album (GUI_PLAN.md §7.9): the shared photo album. "Add photos" opens the system
/// PhotosPicker (multiple) and uploads each as a JPEG (`POST /groups/{id}/photos`).
///
/// Loading: a grid of placeholder tiles, then the photos arrive one after another. Uploads are
/// optimistic: every picked photo gets a tile right away (veiled, with loading dots) that becomes
/// the real photo once the server has it; a failed upload's tile fades away and a note says why.
struct GroupAlbumView: View {
    let groupId: String
    /// nil while the thread is still loading (the header shows placeholder bars).
    let thread: ChatThread?
    /// Called after uploads so the thread's album subtitle ("10 photos · 3 people") refreshes.
    var onPhotosChanged: () async -> Void = {}

    @Environment(AppEnvironment.self) private var env
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    @State private var photos: Loadable<[GroupPhoto]> = .loading
    /// Photos going up from here, newest first (shown before the album's photos).
    @State private var uploads: [String] = []
    /// Server photo id → upload id, so an uploaded tile stays the same view as it settles.
    @State private var localIds: [String: String] = [:]
    /// Each upload's picked photo, shown in its tile while it goes up (and under the real one).
    @State private var previews: [String: UIImage] = [:]
    /// Tiles built before this moment belong to the load that replaced the skeleton and arrive one
    /// after another; later ones (scrolling, uploads, refreshes) don't.
    @State private var arrivalDeadline: Date?
    /// Photos from the load that replaced the skeleton (they don't pop when they appear).
    @State private var initialIds: Set<String> = []
    @State private var picks: [PhotosPickerItem] = []
    @State private var uploadError: String?

    private let columns = Array(repeating: GridItem(.flexible(), spacing: 4), count: 3)
    private var uploading: Bool { !uploads.isEmpty }

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                header
                if let uploadError {
                    Text(uploadError)
                        .socialText(12)
                        .foregroundStyle(Theme.dangerText)
                        .sqTransition(.rise)
                }
                grid
                Text("Everyone in the group can add and save photos.")
                    .socialText(12)
                    .foregroundStyle(Theme.text3)
            }
            .padding(.horizontal, 16)
            .padding(.top, 15)
            .padding(.bottom, 30)
        }
        .scrollIndicators(.hidden)
        .sqPullToRefresh()
        .task { await load() }
        .sqReloadable("thread.\(groupId).album") { await reload() }
        .onChange(of: picks) { _, items in
            guard !items.isEmpty else { return }
            picks = []
            upload(items.map { item in { try? await item.loadTransferable(type: Data.self) } })
        }
    }

    private var header: some View {
        HStack(spacing: 12) {
            ZStack(alignment: .leading) {
                if let thread {
                    VStack(alignment: .leading, spacing: 0) {
                        Text(thread.albumTitle ?? thread.title)
                            .socialText(15, .semibold)
                            .foregroundStyle(Theme.ink)
                        if let subtitle = thread.albumSubtitle {
                            Text(subtitle)
                                .socialText(12)
                                .foregroundStyle(Theme.text3)
                                .sqNumeric()
                        }
                    }
                    .accessibilityElement(children: .combine)
                    .accessibilityAddTraits(.isHeader)
                    .transition(.opacity)
                } else {
                    VStack(alignment: .leading, spacing: 8) {
                        SkeletonBlock(width: 170, height: 14, color: Theme.skeletonOnCream)
                        SkeletonBlock(width: 110, height: 10, color: Theme.skeletonOnCream)
                    }
                    .padding(.vertical, 4)
                    .sqShimmer()
                    .accessibilityElement(children: .ignore)
                    .accessibilityLabel("Loading")
                    .transition(.opacity)
                }
            }
            .animation(Motion.standard, value: thread == nil)
            Spacer(minLength: 8)
            PhotosPicker(selection: $picks, matching: .images) {
                SocialBusyLabel(isBusy: uploading, color: Theme.ink) { Text("Add photos") }
                    .sqFont(14, .semibold)
                    .foregroundStyle(Theme.ink)
                    .padding(.horizontal, 14)
                    .frame(height: 34)
                    .background(Theme.sage, in: Capsule())
                    .contentShape(Rectangle().inset(by: -5))
            }
            .buttonStyle(.plain)
            .disabled(uploading)
            .accessibilityLabel(uploading ? "Adding photos" : "Add photos")
        }
    }

    // MARK: Grid

    /// Uploads first (newest first, like the album), then the album's photos.
    private var tiles: [AlbumTileModel] {
        let pending = uploads.map { AlbumTileModel(id: $0, photo: nil, preview: previews[$0]) }
        let posted = (photos.value ?? []).map { photo in
            let localId = localIds[photo.id]
            return AlbumTileModel(id: localId ?? photo.id, photo: photo, preview: localId.flatMap { previews[$0] })
        }
        return pending + posted
    }

    private var grid: some View {
        ZStack(alignment: .top) {
            switch photos {
            case .loading:
                SkeletonView(layout: .grid(columns: 3, count: 9))
                    .transition(.opacity)
            case .failed(let message):
                ErrorStateView(message: message) { Task { await retryLoad() } }
                    .transition(.opacity)
            case .loaded:
                let tiles = tiles
                if tiles.isEmpty {
                    EmptyStateView(message: "No photos yet.")
                        .transition(.opacity)
                } else {
                    LazyVGrid(columns: columns, spacing: 4) {
                        ForEach(Array(tiles.enumerated()), id: \.element.id) { index, tile in
                            AlbumTile(tile: tile)
                                .modifier(SocialFirstArrival(index: index,
                                                             staggered: arrivalDeadline.map { Date() < $0 } ?? false))
                                // Uploads and photos added since pop in.
                                .transition(initialIds.contains(tile.id) ? SocialMotion.settled
                                                                         : SocialMotion.transition(.pop, reduceMotion: reduceMotion))
                        }
                    }
                    .transition(.opacity)
                }
            }
        }
        .animation(Motion.standard, value: photos.phase)
    }

    // MARK: Loading

    private func load() async {
        show(await Loadable.run { try await env.api.groupPhotos(groupId: groupId) })
    }

    private func retryLoad() async {
        withMotion { photos = .loading }
        await load()
    }

    /// Pull to refresh: new photos pop in; the album stays if the call fails.
    private func reload() async {
        guard let fresh = try? await env.api.groupPhotos(groupId: groupId) else { return }
        show(.loaded(fresh))
    }

    private func show(_ result: Loadable<[GroupPhoto]>) {
        let fromSkeleton = photos.value == nil
        if fromSkeleton, let list = result.value {
            arrivalDeadline = Date().addingTimeInterval(1)
            initialIds = Set(list.map(\.id))
        }
        withMotion(fromSkeleton ? Motion.standard : Motion.arrive) { photos = result }
    }

    // MARK: Upload

    /// Uploads picked photos (each source loads one image's data).
    private func upload(_ sources: [() async -> Data?]) {
        let batch = sources.map { (id: "upload-\(UUID().uuidString)", load: $0) }
        // Each photo lands on top as it's added, so the last pick ends up first.
        withMotion(Motion.arrive) {
            uploads.insert(contentsOf: batch.reversed().map(\.id), at: 0)
            uploadError = nil
        }
        Task {
            var files: [String: Data] = [:]
            // Show each picked photo in its tile while it waits its turn.
            for entry in batch {
                guard let data = await entry.load(), let image = UIImage(data: data),
                      let jpeg = image.uploadJPEG() else { continue }
                files[entry.id] = jpeg
                let preview = image.albumPreview()
                withMotion { previews[entry.id] = preview }
            }
            var failures = 0
            var lastError: String?
            for entry in batch {
                guard let jpeg = files[entry.id] else {
                    failures += 1
                    drop(entry.id)
                    continue
                }
                do {
                    let photo = try await env.api.uploadGroupPhoto(groupId: groupId, jpegData: jpeg)
                    confirm(entry.id, with: photo)
                } catch {
                    failures += 1
                    lastError = error.socialMessage
                    drop(entry.id)
                }
            }
            if failures > 0 {
                let message = lastError ?? (failures == 1 ? "One photo couldn't be added." : "\(failures) photos couldn't be added.")
                withMotion { uploadError = message }
            }
            await onPhotosChanged()
        }
    }

    /// The server has the photo: its tile settles in place (veil and dots fade, the label appears).
    private func confirm(_ uploadId: String, with photo: GroupPhoto) {
        localIds[photo.id] = uploadId
        withMotion {
            uploads.removeAll { $0 == uploadId }
            // (Still on the skeleton: the album's own load brings it.)
            if var list = photos.value {
                if !list.contains(where: { $0.id == photo.id }) { list.insert(photo, at: 0) }
                photos = .loaded(list)
            }
        }
    }

    private func drop(_ uploadId: String) {
        withMotion {
            uploads.removeAll { $0 == uploadId }
            previews[uploadId] = nil
        }
    }
}

/// One tile: an album photo, or a photo still going up (then `photo` is nil).
private struct AlbumTileModel: Identifiable {
    let id: String
    let photo: GroupPhoto?
    /// The picked image for photos added here.
    let preview: UIImage?
}

/// Square tile (radius 6): the photo (or its placeholder color) with a "by {name}" label. While it
/// uploads, the picked photo shows under a light veil with loading dots.
private struct AlbumTile: View {
    let tile: AlbumTileModel

    private var isUploading: Bool { tile.photo == nil }

    private var placeholder: Color {
        if isUploading { return Theme.skeletonOnCream }
        return tile.photo?.placeholderHex.flatMap { Color(hexString: $0) } ?? Theme.segmentBg
    }

    var body: some View {
        Color.clear
            .aspectRatio(1, contentMode: .fit)
            .background(placeholder)
            .overlay { image }
            .overlay {
                if isUploading {
                    ZStack {
                        Color.white.opacity(0.45)
                        LoadingDots(color: Theme.ink, dotSize: 7)
                    }
                    .transition(.opacity)
                }
            }
            .overlay(alignment: .bottomLeading) {
                if let photo = tile.photo {
                    Text("by \(photo.byName)")
                        .socialText(10, .semibold)
                        .foregroundStyle(Theme.text2)
                        .lineLimit(1)
                        .padding(.horizontal, 5)
                        .padding(.vertical, 1)
                        .background(.white.opacity(0.8), in: RoundedRectangle(cornerRadius: 4, style: .continuous))
                        .padding(6)
                        .transition(.opacity)
                }
            }
            .clipShape(RoundedRectangle(cornerRadius: 6, style: .continuous))
            .accessibilityElement()
            .accessibilityLabel(tile.photo.map { "Photo by \($0.byName)" } ?? "Photo")
            .accessibilityValue(isUploading ? "Uploading" : "")
            .accessibilityAddTraits(.isImage)
    }

    @ViewBuilder private var image: some View {
        ZStack {
            if let preview = tile.preview {
                Image(uiImage: preview).resizable().scaledToFill()
            }
            if let photo = tile.photo {
                if tile.preview == nil, let data = photo.imageData, let uiImage = UIImage(data: data) {
                    Image(uiImage: uiImage).resizable().scaledToFill()
                } else if let url = photo.url {
                    // The server's copy fades in over the picked one (or the placeholder color).
                    AsyncImage(url: url, transaction: Transaction(animation: Motion.standard)) { phase in
                        if let loaded = phase.image { loaded.resizable().scaledToFill().transition(.opacity) }
                    }
                }
            }
        }
    }
}

private extension UIImage {
    /// A small copy for the upload tile (the tile is ~120pt, the picked photo can be 12 MP).
    func albumPreview(maxDimension: CGFloat = 600) -> UIImage {
        let longest = max(size.width, size.height)
        guard longest > maxDimension else { return self }
        let scale = maxDimension / longest
        let target = CGSize(width: (size.width * scale).rounded(), height: (size.height * scale).rounded())
        return preparingThumbnail(of: target) ?? self
    }
}

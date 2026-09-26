import PhotosUI
import SwiftUI
import UIKit

/// Thread › Album (GUI_PLAN.md §7.9): the shared photo album. "Add photos" opens the system
/// PhotosPicker (multiple) and uploads each as a JPEG (`POST /groups/{id}/photos`).
struct GroupAlbumView: View {
    let thread: ChatThread
    /// Called after uploads so the thread's album subtitle ("10 photos · 3 people") refreshes.
    var onPhotosChanged: () async -> Void = {}

    @Environment(AppEnvironment.self) private var env
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    @State private var photos: Loadable<[GroupPhoto]> = .loading
    @State private var picks: [PhotosPickerItem] = []
    @State private var uploading = false
    @State private var uploadError: String?

    private let columns = Array(repeating: GridItem(.flexible(), spacing: 4), count: 3)

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 12) {
                header
                if let uploadError {
                    Text(uploadError)
                        .socialText(12)
                        .foregroundStyle(Theme.dangerText)
                }
                LoadableView(state: photos, retry: { Task { await load(showLoading: true) } }) { list in
                    if list.isEmpty {
                        EmptyStateView(message: "No photos yet.")
                    } else {
                        LazyVGrid(columns: columns, spacing: 4) {
                            ForEach(list) { AlbumTile(photo: $0) }
                        }
                    }
                }
                Text("Everyone in the group can add and save photos.")
                    .socialText(12)
                    .foregroundStyle(Theme.text3)
            }
            .padding(.horizontal, 16)
            .padding(.top, 15)
            .padding(.bottom, 30)
        }
        .scrollIndicators(.hidden)
        .task { await load() }
        .onChange(of: picks) { _, items in
            if !items.isEmpty { upload(items) }
        }
    }

    private var header: some View {
        HStack(spacing: 12) {
            VStack(alignment: .leading, spacing: 0) {
                Text(thread.albumTitle ?? thread.title)
                    .socialText(15, .semibold)
                    .foregroundStyle(Theme.ink)
                if let subtitle = thread.albumSubtitle {
                    Text(subtitle)
                        .socialText(12)
                        .foregroundStyle(Theme.text3)
                }
            }
            .accessibilityElement(children: .combine)
            .accessibilityAddTraits(.isHeader)
            Spacer(minLength: 8)
            PhotosPicker(selection: $picks, matching: .images) {
                HStack(spacing: 6) {
                    if uploading {
                        ProgressView().controlSize(.small).tint(Theme.ink)
                    }
                    Text("Add photos")
                }
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

    // MARK: Loading + upload

    private func load(showLoading: Bool = false) async {
        if showLoading || photos.value == nil { photos = .loading }
        photos = await Loadable.run { try await env.api.groupPhotos(groupId: thread.id) }
    }

    private func upload(_ items: [PhotosPickerItem]) {
        picks = []
        uploading = true
        uploadError = nil
        Task {
            var failures = 0
            for item in items {
                do {
                    guard let data = try await item.loadTransferable(type: Data.self),
                          let jpeg = UIImage(data: data)?.uploadJPEG() else {
                        failures += 1
                        continue
                    }
                    let photo = try await env.api.uploadGroupPhoto(groupId: thread.id, jpegData: jpeg)
                    insert(photo)
                } catch {
                    failures += 1
                    uploadError = error.socialMessage
                }
            }
            if failures > 0, uploadError == nil {
                uploadError = failures == 1 ? "One photo couldn't be added." : "\(failures) photos couldn't be added."
            }
            uploading = false
            await onPhotosChanged()
        }
    }

    private func insert(_ photo: GroupPhoto) {
        var list = photos.value ?? []
        list.insert(photo, at: 0)
        if reduceMotion {
            photos = .loaded(list)
        } else {
            withAnimation(.easeOut(duration: 0.2)) { photos = .loaded(list) }
        }
    }
}

/// Square tile (radius 6): the photo (or its placeholder color) with a "by {name}" label.
private struct AlbumTile: View {
    let photo: GroupPhoto

    private var placeholder: Color {
        photo.placeholderHex.flatMap { Color(hexString: $0) } ?? Theme.segmentBg
    }

    var body: some View {
        Color.clear
            .aspectRatio(1, contentMode: .fit)
            .background(placeholder)
            .overlay { image }
            .overlay(alignment: .bottomLeading) {
                Text("by \(photo.byName)")
                    .socialText(10, .semibold)
                    .foregroundStyle(Theme.text2)
                    .lineLimit(1)
                    .padding(.horizontal, 5)
                    .padding(.vertical, 1)
                    .background(.white.opacity(0.8), in: RoundedRectangle(cornerRadius: 4, style: .continuous))
                    .padding(6)
            }
            .clipShape(RoundedRectangle(cornerRadius: 6, style: .continuous))
            .accessibilityElement()
            .accessibilityLabel("Photo by \(photo.byName)")
            .accessibilityAddTraits(.isImage)
    }

    @ViewBuilder private var image: some View {
        if let data = photo.imageData, let uiImage = UIImage(data: data) {
            Image(uiImage: uiImage).resizable().scaledToFill()
        } else if let url = photo.url {
            AsyncImage(url: url) { phase in
                if let loaded = phase.image { loaded.resizable().scaledToFill() }
            }
        }
    }
}

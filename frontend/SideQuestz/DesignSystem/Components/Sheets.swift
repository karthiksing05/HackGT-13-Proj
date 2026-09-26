import SwiftUI

/// Custom grabber: 40×5, radius 3 (`lineStrong` on white sheets, `mutedBorder` on cream sheets).
struct SheetGrabber: View {
    var color: Color = Theme.lineStrong

    var body: some View {
        RoundedRectangle(cornerRadius: 3, style: .continuous)
            .fill(color)
            .frame(width: 40, height: 5)
            .frame(maxWidth: .infinity)
            .accessibilityHidden(true)
    }
}

/// Standard sheet body: grabber, then content; padding top 8 / sides 20 / bottom 34 (the bottom
/// includes the home indicator, so only the part the safe area doesn't cover is added).
struct SheetScaffold<Content: View>: View {
    var spacing: CGFloat = 14
    var grabberColor: Color = Theme.lineStrong
    @ViewBuilder var content: Content
    @Environment(\.safeAreaBottom) private var safeBottom

    var body: some View {
        VStack(alignment: .leading, spacing: spacing) {
            SheetGrabber(color: grabberColor)
            content
        }
        .padding(.top, 8)
        .padding(.horizontal, Metrics.side)
        .padding(.bottom, max(0, 34 - safeBottom))
    }
}

/// How tall a sheet is.
enum SQSheetHeight {
    /// Sized to its content, scrolling past `max` (measured from the bottom of the screen).
    case fitted(max: CGFloat? = nil)
    /// A fixed height from the bottom of the screen (Event sheet: 660), content scrolls.
    case fixed(CGFloat)
    /// Fills from this distance below the top of the screen (Add expense: 60), content scrolls.
    case fromTop(CGFloat)
}

struct SQSheetStyle {
    var background: Color = .white
    var height: SQSheetHeight = .fitted()
    /// Backdrop darkness: 0.35 for most sheets, 0.45 for Checkout.
    var dim: Double = 0.35

    static let standard = SQSheetStyle()
    /// Cream sheets (Add expense, More options).
    static func cream(_ height: SQSheetHeight = .fitted()) -> SQSheetStyle { SQSheetStyle(background: Theme.cream, height: height) }
}

// MARK: - Presentation

/// The prototype's sheets are full-width, flush with the bottom, 24pt top corners, over a black
/// 35% backdrop that also covers the tab bar. iOS 26 system sheets float inset instead, so we
/// present a clear full-screen cover (no system animation) and animate our own panel.
private struct SQSheetModifier<SheetContent: View>: ViewModifier {
    @Binding var isPresented: Bool
    var style: SQSheetStyle
    var onDismiss: (() -> Void)?
    @ViewBuilder var sheet: () -> SheetContent
    @State private var coverShown = false

    func body(content: Content) -> some View {
        content
            .fullScreenCover(isPresented: $coverShown, onDismiss: onDismiss) {
                SQSheetContainer(style: style, isPresented: isPresented,
                                 requestDismiss: { isPresented = false },
                                 finished: { setCover(false) },
                                 content: sheet)
                    .presentationBackground(.clear)
            }
            .onChange(of: isPresented, initial: true) { _, shown in
                if shown && !coverShown { setCover(true) }
            }
    }

    private func setCover(_ shown: Bool) {
        var transaction = Transaction()
        transaction.disablesAnimations = true
        withTransaction(transaction) { coverShown = shown }
    }
}

private struct SQItemSheetModifier<Item: Identifiable, SheetContent: View>: ViewModifier {
    @Binding var item: Item?
    var style: SQSheetStyle
    var onDismiss: (() -> Void)?
    @ViewBuilder var sheet: (Item) -> SheetContent
    /// Keeps the last item alive while the panel animates out.
    @State private var shownItem: Item?
    @State private var coverShown = false

    func body(content: Content) -> some View {
        content
            .fullScreenCover(isPresented: $coverShown, onDismiss: {
                shownItem = nil
                onDismiss?()
            }) {
                if let shownItem {
                    SQSheetContainer(style: style, isPresented: item != nil,
                                     requestDismiss: { item = nil },
                                     finished: { setCover(false) },
                                     content: { sheet(shownItem) })
                        .presentationBackground(.clear)
                }
            }
            .onChange(of: item?.id, initial: true) { _, _ in
                if let item {
                    shownItem = item
                    if !coverShown { setCover(true) }
                }
            }
    }

    private func setCover(_ shown: Bool) {
        var transaction = Transaction()
        transaction.disablesAnimations = true
        withTransaction(transaction) { coverShown = shown }
    }
}

private struct SQSheetContainer<Content: View>: View {
    let style: SQSheetStyle
    let isPresented: Bool
    let requestDismiss: () -> Void
    let finished: () -> Void
    @ViewBuilder var content: () -> Content

    @State private var visible = false
    @State private var drag: CGFloat = 0
    @State private var contentHeight: CGFloat = 0
    @Environment(\.safeAreaBottom) private var safeBottom
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        GeometryReader { proxy in
            ZStack(alignment: .bottom) {
                Color.black.opacity(visible ? style.dim : 0)
                    .ignoresSafeArea()
                    .contentShape(Rectangle())
                    .onTapGesture { requestDismiss() }
                    .accessibilityLabel("Close")
                    .accessibilityAddTraits(.isButton)

                if visible {
                    panel(screenHeight: proxy.size.height + proxy.safeAreaInsets.top + proxy.safeAreaInsets.bottom)
                        .transition(reduceMotion ? .opacity : .move(edge: .bottom))
                        .offset(y: max(0, drag))
                }
            }
        }
        .onAppear {
            withAnimation(reduceMotion ? .easeOut(duration: 0.2) : .spring(duration: 0.38, bounce: 0.08)) { visible = true }
        }
        .onChange(of: isPresented) { _, presented in
            if !presented { animateOut() }
        }
        .accessibilityAction(.escape) { requestDismiss() }
    }

    @ViewBuilder
    private func panel(screenHeight: CGFloat) -> some View {
        let shape = UnevenRoundedRectangle(topLeadingRadius: Metrics.sheetRadius, topTrailingRadius: Metrics.sheetRadius, style: .continuous)
        Group {
            switch style.height {
            case .fitted(let max):
                if let max {
                    let limit = max - safeBottom
                    ScrollView {
                        measured
                    }
                    .scrollBounceBehavior(.basedOnSize)
                    .scrollDismissesKeyboard(.interactively)
                    .frame(height: min(contentHeight, limit))
                } else {
                    content()
                }
            case .fixed(let height):
                scrolling.frame(height: height - safeBottom)
            case .fromTop(let top):
                scrolling.frame(height: screenHeight - top - safeBottom)
            }
        }
        .frame(maxWidth: .infinity, alignment: .top)
        .background {
            shape.fill(style.background).ignoresSafeArea(edges: .bottom)
        }
        .overlay(alignment: .top) {
            // Drag the grabber area down to dismiss.
            Color.clear
                .frame(height: 30)
                .contentShape(Rectangle())
                .gesture(dragToDismiss)
        }
    }

    private var measured: some View {
        content()
            .onGeometryChange(for: CGFloat.self, of: { $0.size.height }) { contentHeight = $0 }
    }

    private var scrolling: some View {
        ScrollView {
            content()
        }
        .scrollBounceBehavior(.basedOnSize)
        .scrollDismissesKeyboard(.interactively)
    }

    private var dragToDismiss: some Gesture {
        DragGesture(minimumDistance: 4)
            .onChanged { drag = $0.translation.height }
            .onEnded { value in
                if value.translation.height > 110 || value.predictedEndTranslation.height > 260 {
                    requestDismiss()
                } else {
                    withAnimation(.spring(duration: 0.3)) { drag = 0 }
                }
            }
    }

    private func animateOut() {
        withAnimation(reduceMotion ? .easeOut(duration: 0.15) : .easeIn(duration: 0.22)) {
            visible = false
        } completion: {
            drag = 0
            finished()
        }
    }
}

extension View {
    /// Presents a SideQuestz sheet (full-width, 24pt top corners, dim backdrop). Put a `SheetScaffold`
    /// inside for the grabber and padding. Tap the backdrop or drag the grabber down to close.
    func sqSheet<Content: View>(isPresented: Binding<Bool>, style: SQSheetStyle = .standard, onDismiss: (() -> Void)? = nil,
                                @ViewBuilder content: @escaping () -> Content) -> some View {
        modifier(SQSheetModifier(isPresented: isPresented, style: style, onDismiss: onDismiss, sheet: content))
    }

    /// Item-driven version of `sqSheet`.
    func sqSheet<Item: Identifiable, Content: View>(item: Binding<Item?>, style: SQSheetStyle = .standard, onDismiss: (() -> Void)? = nil,
                                                    @ViewBuilder content: @escaping (Item) -> Content) -> some View {
        modifier(SQItemSheetModifier(item: item, style: style, onDismiss: onDismiss, sheet: content))
    }
}

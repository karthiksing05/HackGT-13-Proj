import CoreImage
import CoreImage.CIFilterBuiltins
import UIKit

/// The ticket's QR code, made on the phone with Core Image (`CIQRCodeGenerator`): nothing is
/// fetched, so it shows offline too.
enum TicketQRCode {
    private static let context = CIContext()

    /// What the code carries: the ticket's page when it has one, else its confirmation code.
    static func payload(for ticket: Ticket) -> String? {
        if let url = ticket.url { return url.absoluteString }
        guard let code = ticket.confirmation, !code.isEmpty else { return nil }
        return code
    }

    /// `text` as a QR code, `scale` device pixels per module (drawn without smoothing it stays
    /// crisp at any size). nil for empty text.
    static func image(for text: String, scale: CGFloat = 12) -> UIImage? {
        guard !text.isEmpty else { return nil }
        let filter = CIFilter.qrCodeGenerator()
        filter.message = Data(text.utf8)
        filter.correctionLevel = "M"
        guard let code = filter.outputImage?.transformed(by: CGAffineTransform(scaleX: scale, y: scale)),
              let cgImage = context.createCGImage(code, from: code.extent) else { return nil }
        return UIImage(cgImage: cgImage)
    }
}

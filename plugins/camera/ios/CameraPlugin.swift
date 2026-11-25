import AVFoundation
import UIKit

private final class PreviewView: UIView, AVCapturePhotoCaptureDelegate, AVCaptureMetadataOutputObjectsDelegate {
    override class var layerClass: AnyClass { AVCaptureVideoPreviewLayer.self }
    let session = AVCaptureSession()
    let output = AVCapturePhotoOutput()
    var waiting: PluginReply?
    var id = 0
    // lastCode is the code told last, which is told again only after another.
    var lastCode: String?

    func start(front: Bool, codes: Bool) {
        (layer as! AVCaptureVideoPreviewLayer).session = session
        (layer as! AVCaptureVideoPreviewLayer).videoGravity = .resizeAspectFill
        AVCaptureDevice.requestAccess(for: .video) { granted in
            guard granted else { return }
            let position: AVCaptureDevice.Position = front ? .front : .back
            guard let d = AVCaptureDevice.default(.builtInWideAngleCamera, for: .video, position: position),
                  let input = try? AVCaptureDeviceInput(device: d) else { return }
            self.session.beginConfiguration()
            if self.session.canAddInput(input) { self.session.addInput(input) }
            if self.session.canAddOutput(self.output) { self.session.addOutput(self.output) }
            if codes {
                let m = AVCaptureMetadataOutput()
                if self.session.canAddOutput(m) {
                    self.session.addOutput(m)
                    // The types it reads can be set once it is in the session.
                    if m.availableMetadataObjectTypes.contains(.qr) { m.metadataObjectTypes = [.qr] }
                    m.setMetadataObjectsDelegate(self, queue: .main)
                }
            }
            self.session.commitConfiguration()
            DispatchQueue.global(qos: .userInitiated).async { self.session.startRunning() }
        }
    }

    func stop() {
        DispatchQueue.global(qos: .userInitiated).async { self.session.stopRunning() }
    }

    func metadataOutput(_ o: AVCaptureMetadataOutput, didOutput objects: [AVMetadataObject], from c: AVCaptureConnection) {
        for case let code as AVMetadataMachineReadableCodeObject in objects {
            guard let text = code.stringValue, text != lastCode else { continue }
            lastCode = text
            PluginHost.emit("camera", "code", ["id": id, "text": text])
        }
    }

    func photoOutput(_ o: AVCapturePhotoOutput, didFinishProcessingPhoto photo: AVCapturePhoto, error: Error?) {
        guard let reply = waiting else { return }
        waiting = nil
        guard let data = photo.fileDataRepresentation() else {
            reply(.failure(PluginFailure("Failed", error?.localizedDescription ?? "no photo")))
            return
        }
        let path = NSTemporaryDirectory() + UUID().uuidString + ".jpg"
        do {
            try data.write(to: URL(fileURLWithPath: path))
            reply(.success(["path": path]))
        } catch {
            reply(.failure(PluginFailure("Failed", error.localizedDescription)))
        }
    }
}

final class CameraPlugin: MyGoPlugin {
    private var views: [Int: PreviewView] = [:]

    init() {
        PluginHost.registerView("camera") { [weak self] id, args in
            let v = PreviewView()
            v.id = id
            self?.views[id] = v
            v.start(front: args["front"] as? Bool ?? false, codes: args["codes"] as? Bool ?? false)
            return v
        }
    }

    func call(_ method: String, _ args: [String: Any], _ reply: @escaping PluginReply) {
        guard let id = args["id"] as? Int, let v = views[id] else {
            return reply(.failure(PluginFailure("NoView")))
        }
        switch method {
        case "takePhoto":
            guard !v.session.outputs.isEmpty, v.session.isRunning else {
                return reply(.failure(PluginFailure("NoCamera", "the camera is not running")))
            }
            v.waiting = reply
            v.output.capturePhoto(with: AVCapturePhotoSettings(), delegate: v)
        case "dispose":
            v.stop()
            views[id] = nil
            reply(.success(nil))
        default:
            reply(.failure(PluginFailure("NoMethod", method)))
        }
    }
}

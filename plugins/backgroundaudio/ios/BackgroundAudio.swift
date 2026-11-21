import AVFoundation
import MediaPlayer
import UIKit

final class BackgroundAudioPlugin: MyGoPlugin {
    private let session = AVAudioSession.sharedInstance()
    private var targets: [(MPRemoteCommand, Any)] = []
    private var observers: [NSObjectProtocol] = []
    private var originalCategory: AVAudioSession.Category?
    private var originalOptions: AVAudioSession.CategoryOptions = []
    private var originalMode: AVAudioSession.Mode = .default

    private func listen() {
        guard observers.isEmpty else { return }
        let center = NotificationCenter.default
        observers.append(center.addObserver(forName: AVAudioSession.interruptionNotification, object: session, queue: .main) { n in
            let began = n.userInfo?[AVAudioSessionInterruptionTypeKey] as? UInt == AVAudioSession.InterruptionType.began.rawValue
            let options = AVAudioSession.InterruptionOptions(rawValue: n.userInfo?[AVAudioSessionInterruptionOptionKey] as? UInt ?? 0)
            PluginHost.emit("backgroundaudio", "lifecycle", ["Kind": began ? "interrupted" : "resume", "ResumeAllowed": options.contains(.shouldResume)])
        })
        observers.append(center.addObserver(forName: AVAudioSession.routeChangeNotification, object: session, queue: .main) { n in
            let reason = n.userInfo?[AVAudioSessionRouteChangeReasonKey] as? UInt
            PluginHost.emit("backgroundaudio", "lifecycle", ["Kind": reason == AVAudioSession.RouteChangeReason.oldDeviceUnavailable.rawValue ? "outputRemoved" : "outputChanged"])
        })
    }

    func call(_ method: String, _ args: [String: Any], _ reply: @escaping PluginReply) {
        let center = MPRemoteCommandCenter.shared()
        for (command, target) in targets {
            command.removeTarget(target)
            command.isEnabled = false
        }
        targets.removeAll()
        do {
            if method == "clear" {
                MPNowPlayingInfoCenter.default().nowPlayingInfo = nil
                for observer in observers { NotificationCenter.default.removeObserver(observer) }
                observers.removeAll()
                if let originalCategory {
                    // An active output can report isBusy on deactivation.
                    // Restore its category even in that case.
                    try? session.setActive(false, options: .notifyOthersOnDeactivation)
                    try session.setCategory(originalCategory, mode: originalMode, options: originalOptions)
                    self.originalCategory = nil
                }
            } else {
                listen()
                if args["State"] as? String == "playing" {
                    if originalCategory == nil {
                        originalCategory = session.category
                        originalOptions = session.categoryOptions
                        originalMode = session.mode
                    }
                    if session.category != .playback { try session.setCategory(.playback) }
                    try session.setActive(true)
                }
                let mask = args["Commands"] as? Int ?? 0
                for (command, bit) in [(center.playCommand, 1), (center.pauseCommand, 2), (center.togglePlayPauseCommand, 4), (center.stopCommand, 8), (center.nextTrackCommand, 16), (center.previousTrackCommand, 32), (center.changePlaybackPositionCommand, 64)] {
                    command.isEnabled = mask & (bit == 4 ? 3 : bit) != 0
                    if command.isEnabled {
                        let target = command.addTarget { event in
                            let pos = (event as? MPChangePlaybackPositionCommandEvent)?.positionTime ?? 0
                            PluginHost.emit("backgroundaudio", "command", ["Command": bit, "Milliseconds": Int64(pos * 1000)])
                            return .success
                        }
                        targets.append((command, target))
                    }
                }
                var info: [String: Any] = [
                    MPMediaItemPropertyTitle: args["Title"] as? String ?? "",
                    MPMediaItemPropertyArtist: args["Artist"] as? String ?? "",
                    MPMediaItemPropertyAlbumTitle: args["Album"] as? String ?? "",
                    MPNowPlayingInfoPropertyElapsedPlaybackTime: (args["Position"] as? Double ?? 0) / 1e9,
                    MPNowPlayingInfoPropertyPlaybackRate: args["State"] as? String == "playing" ? args["Rate"] as? Double ?? 1 : 0
                ]
                if let ns = args["Duration"] as? Double, ns > 0 { info[MPMediaItemPropertyPlaybackDuration] = ns / 1e9 }
                if let art = args["Artwork"] as? String, let data = Data(base64Encoded: art), let image = UIImage(data: data) {
                    info[MPMediaItemPropertyArtwork] = MPMediaItemArtwork(boundsSize: image.size) { _ in image }
                }
                MPNowPlayingInfoCenter.default().nowPlayingInfo = info
            }
            reply(.success(nil))
        } catch { reply(.failure(PluginFailure("AudioSession", error.localizedDescription))) }
    }
}

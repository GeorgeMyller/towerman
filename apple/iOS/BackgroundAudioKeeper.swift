import Foundation
import AVFoundation
import UIKit

/// Mantém o processo do iOS vivo em segundo plano enquanto a ligação estiver ativa, a tocar
/// silêncio em ciclo contínuo. O modo de fundo «audio» só segura o app enquanto o áudio toca:
/// um pulso curto com pausas deixava o iOS suspendê-lo na pausa, e o temporizador seguinte
/// nunca disparava. Usa AVAudioSession com mixWithOthers, sem cortar o áudio de outros apps.
public final class BackgroundAudioKeeper: NSObject, AVAudioPlayerDelegate {
    public static let shared = BackgroundAudioKeeper()

    public private(set) var isActive: Bool = false

    private var player: AVAudioPlayer?
    private var heartbeatWorkItem: DispatchWorkItem?

    private override init() {
        super.init()
        setupNotifications()
    }

    /// Atualiza o estado do keeper com base na intenção de conexão do utilizador.
    public func update(wantsConnection: Bool) {
        if Thread.isMainThread {
            applyUpdate(wantsConnection: wantsConnection)
        } else {
            DispatchQueue.main.async { [weak self] in
                self?.applyUpdate(wantsConnection: wantsConnection)
            }
        }
    }

    /// Mantido para compatibilidade.
    public func update(hasActiveAgents: Bool) {
        update(wantsConnection: hasActiveAgents)
    }

    private func applyUpdate(wantsConnection: Bool) {
        if wantsConnection {
            if !isActive {
                start()
            }
        } else {
            if isActive {
                stop()
            }
        }
    }

    private func start() {
        guard !isActive else { return }

        do {
            let session = AVAudioSession.sharedInstance()
            try session.setCategory(.playback, mode: .default, options: [.mixWithOthers])
            try session.setActive(true)

            if player == nil {
                let data = Self.makeSilenceWAV()
                player = try AVAudioPlayer(data: data)
                player?.delegate = self
                player?.numberOfLoops = -1
                player?.volume = 0.0
                player?.prepareToPlay()
            }

            isActive = true
            playHeartbeat()
        } catch {
            print("[BackgroundAudioKeeper] Falha ao iniciar áudio de fundo: \(error)")
        }
    }

    private func stop() {
        guard isActive else { return }
        isActive = false

        heartbeatWorkItem?.cancel()
        heartbeatWorkItem = nil

        player?.stop()

        do {
            try AVAudioSession.sharedInstance().setActive(false, options: [.notifyOthersOnDeactivation])
        } catch {
            print("[BackgroundAudioKeeper] Falha ao desativar áudio de fundo: \(error)")
        }
    }

    private func playHeartbeat() {
        guard isActive, let player = player else { return }
        player.currentTime = 0
        let started = player.play()
        if !started {
            scheduleNextHeartbeat(delay: 5.0)
        }
    }

    /// Com o ciclo infinito só acaba por erro de descodificação: volta a tentar.
    public func audioPlayerDidFinishPlaying(_ player: AVAudioPlayer, successfully flag: Bool) {
        guard isActive else { return }
        scheduleNextHeartbeat(delay: 5.0)
    }

    private func scheduleNextHeartbeat(delay: TimeInterval) {
        heartbeatWorkItem?.cancel()
        let item = DispatchWorkItem { [weak self] in
            guard let self = self, self.isActive else { return }
            self.playHeartbeat()
        }
        heartbeatWorkItem = item
        DispatchQueue.main.asyncAfter(deadline: .now() + delay, execute: item)
    }

    private func setupNotifications() {
        NotificationCenter.default.addObserver(
            self,
            selector: #selector(handleInterruption),
            name: AVAudioSession.interruptionNotification,
            object: nil
        )
    }

    @objc private func handleInterruption(notification: Notification) {
        guard let info = notification.userInfo,
              let typeValue = info[AVAudioSessionInterruptionTypeKey] as? UInt,
              let type = AVAudioSession.InterruptionType(rawValue: typeValue) else { return }

        DispatchQueue.main.async { [weak self] in
            guard let self = self else { return }
            // Depois de uma chamada ou do Siri, retoma sempre, com ou sem .shouldResume: o
            // silêncio misturado não incomoda, e sem ele o keeper ficava parado com isActive=true.
            if type == .ended && self.isActive {
                try? AVAudioSession.sharedInstance().setActive(true)
                self.playHeartbeat()
            }
        }
    }

    /// Gera em memória um arquivo WAV PCM de 16 bits mono a 8000 Hz com 1 segundo de silêncio absoluto.
    public static func makeSilenceWAV(durationSeconds: Int = 1, sampleRate: Int = 8000) -> Data {
        let numChannels: UInt16 = 1
        let bitsPerSample: UInt16 = 16
        let numSamples = sampleRate * durationSeconds
        let dataSize = UInt32(numSamples * Int(numChannels) * Int(bitsPerSample / 8))
        let byteRate = UInt32(sampleRate * Int(numChannels) * Int(bitsPerSample / 8))
        let blockAlign = UInt16(numChannels * (bitsPerSample / 8))
        let chunkSize = UInt32(36 + dataSize)

        var data = Data()
        data.reserveCapacity(Int(44 + dataSize))

        // RIFF header
        data.append(contentsOf: [0x52, 0x49, 0x46, 0x46]) // "RIFF"
        withUnsafeBytes(of: chunkSize.littleEndian) { data.append(contentsOf: $0) }
        data.append(contentsOf: [0x57, 0x41, 0x56, 0x45]) // "WAVE"

        // "fmt " chunk
        data.append(contentsOf: [0x66, 0x6D, 0x74, 0x20]) // "fmt "
        withUnsafeBytes(of: UInt32(16).littleEndian) { data.append(contentsOf: $0) }
        withUnsafeBytes(of: UInt16(1).littleEndian) { data.append(contentsOf: $0) } // PCM
        withUnsafeBytes(of: numChannels.littleEndian) { data.append(contentsOf: $0) }
        withUnsafeBytes(of: UInt32(sampleRate).littleEndian) { data.append(contentsOf: $0) }
        withUnsafeBytes(of: byteRate.littleEndian) { data.append(contentsOf: $0) }
        withUnsafeBytes(of: blockAlign.littleEndian) { data.append(contentsOf: $0) }
        withUnsafeBytes(of: bitsPerSample.littleEndian) { data.append(contentsOf: $0) }

        // "data" chunk
        data.append(contentsOf: [0x64, 0x61, 0x74, 0x61]) // "data"
        withUnsafeBytes(of: dataSize.littleEndian) { data.append(contentsOf: $0) }
        data.append(Data(repeating: 0, count: Int(dataSize)))

        return data
    }
}

type AudioContextCtor = typeof AudioContext
type GlobalWithAudio = {
  AudioContext?: AudioContextCtor
  webkitAudioContext?: AudioContextCtor
}

let ctx: AudioContext | null = null

function getContext(): AudioContext | null {
  if (ctx) return ctx
  const audioGlobal = globalThis as GlobalWithAudio
  const Ctor = audioGlobal.AudioContext ?? audioGlobal.webkitAudioContext
  if (!Ctor) return null
  try {
    ctx = new Ctor()
    return ctx
  } catch {
    return null
  }
}

// playChirp emits a short two-tone "incoming PTT" notification (Nextel-style),
// then resolves so the caller can start the actual audio playback afterwards.
// Volume is 0..1; defaults to 0.15 so it sits well below voice audio.
// Frequencies are kept below 1.2 kHz to avoid the 2–4 kHz "piercing" range.
export function playChirp(volume = 0.15): Promise<void> {
  const audioCtx = getContext()
  if (!audioCtx || volume <= 0) return Promise.resolve()
  if (audioCtx.state === 'suspended') audioCtx.resume().catch(() => {})

  const now = audioCtx.currentTime
  const peak = Math.max(0, Math.min(1, volume))

  function beep(freq: number, startOffset: number, duration: number) {
    if (!audioCtx) return
    const osc = audioCtx.createOscillator()
    const gain = audioCtx.createGain()
    osc.type = 'sine'
    osc.frequency.value = freq
    gain.gain.setValueAtTime(0, now + startOffset)
    gain.gain.linearRampToValueAtTime(peak, now + startOffset + 0.025)
    gain.gain.setValueAtTime(peak, now + startOffset + duration - 0.04)
    gain.gain.linearRampToValueAtTime(0, now + startOffset + duration)
    osc.connect(gain).connect(audioCtx.destination)
    osc.start(now + startOffset)
    osc.stop(now + startOffset + duration + 0.02)
  }

  beep(900, 0, 0.1)
  beep(1150, 0.11, 0.12)

  return new Promise((resolve) => globalThis.setTimeout(resolve, 250))
}

type AudioPart = {
  id: number
}

type PlayableAudio = Pick<HTMLAudioElement, 'onended' | 'src' | 'play'>

/** Plays a grouped call's parts in sequence using a shared audio element. */
export function playCallParts(
  audio: PlayableAudio,
  parts: AudioPart[],
  onComplete: () => void,
  onError: (error: unknown) => void,
): Promise<void> {
  let partIndex = 0

  const playPart = (): Promise<void> => {
    const part = parts[partIndex]
    audio.src = `/api/v1/calls/${part.id}/audio?play=1`
    audio.onended = () => {
      partIndex++
      if (partIndex < parts.length) {
        void playPart().catch(onError)
        return
      }
      onComplete()
    }
    return audio.play()
  }

  return playPart()
}

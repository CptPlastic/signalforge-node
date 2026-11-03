import { describe, expect, it, vi } from 'vitest'
import { playCallParts } from './callPlayback'

describe('playCallParts', () => {
  it('loads and plays each grouped call part once, in order', async () => {
    const audio: {
      src: string
      onended: (() => void) | null
      play: () => Promise<void>
    } = {
      src: '',
      onended: null,
      play: vi.fn(async () => {}),
    }
    const onComplete = vi.fn()
    const onError = vi.fn()

    await playCallParts(audio, [{ id: 101 }, { id: 202 }], onComplete, onError)
    expect(audio.src).toBe('/api/v1/calls/101/audio?play=1')
    expect(audio.play).toHaveBeenCalledTimes(1)

    audio.onended?.()
    await Promise.resolve()
    expect(audio.src).toBe('/api/v1/calls/202/audio?play=1')
    expect(audio.play).toHaveBeenCalledTimes(2)

    audio.onended?.()
    expect(onComplete).toHaveBeenCalledOnce()
    expect(onError).not.toHaveBeenCalled()
  })
})

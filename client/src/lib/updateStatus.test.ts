import { describe, expect, it } from 'vitest'
import { isNewerPublishedRelease, sameRelease } from './updateStatus'
import type { UpdateManifest } from './api'

const manifest = (overrides: Partial<UpdateManifest> = {}): UpdateManifest => ({
  product: 'p7-scanner',
  channel: 'stable',
  version: 'bca8701',
  commit: 'bca8701b06c8e27d975940ba5a4e3dd63d1bb9de',
  shortCommit: 'bca8701',
  imageRegistry: 'ghcr.io',
  imageNamespace: 'signalforge-org',
  imageTag: 'bca8701',
  publishedAt: '2026-11-20T19:58:45Z',
  images: {},
  ...overrides,
})

const current = {
  deployTag: 'a89c29e',
  commit: 'a89c29ebc4c5a63b91f4a6639de552e4eb91729c',
  buildDate: '2026-11-23T22:59:00Z',
}

describe('isNewerPublishedRelease', () => {
  it('treats the same short tag and full commit as the running release', () => {
    const latest = manifest({
      version: 'a89c29e',
      shortCommit: 'a89c29e',
      imageTag: 'a89c29e',
      commit: current.commit,
      publishedAt: '2026-11-23T23:05:00Z',
    })
    expect(sameRelease(current, latest)).toBe(true)
    expect(isNewerPublishedRelease(current, latest)).toBe(false)
  })

  it('does not offer an older published tag as an update', () => {
    expect(isNewerPublishedRelease(current, manifest())).toBe(false)
  })

  it('offers a manifest published after this image was built', () => {
    const latest = manifest({
      version: 'ccccccc',
      shortCommit: 'ccccccc',
      imageTag: 'ccccccc',
      commit: 'ccccccc1234567890ccccccc1234567890cccc',
      publishedAt: '2026-11-24T01:00:00Z',
    })
    expect(isNewerPublishedRelease(current, latest)).toBe(true)
  })

  it('does not treat an unordered mismatch as an update', () => {
    expect(isNewerPublishedRelease({ ...current, buildDate: 'unknown' }, manifest({
      imageTag: 'ccccccc',
      shortCommit: 'ccccccc',
      version: 'ccccccc',
      commit: 'ccccccc1234567890ccccccc1234567890cccc',
    }))).toBe(false)
    expect(isNewerPublishedRelease(current, manifest({ publishedAt: '' }))).toBe(false)
  })
})

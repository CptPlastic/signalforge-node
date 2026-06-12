/** Shared browser PTT recording — prefer M4A (AAC) to match mobile + hub storage. */

export const PTT_PREFERRED_MIME = 'audio/mp4'
export const PTT_FALLBACK_MIME = 'audio/webm;codecs=opus'

/** MIME types to try for MediaRecorder, best-first. */
export function pickPttMimeType(): string {
  const candidates = [
    'audio/mp4',
    'audio/mp4;codecs=mp4a',
    'audio/webm;codecs=opus',
    'audio/webm',
  ]
  for (const type of candidates) {
    if (globalThis.MediaRecorder?.isTypeSupported(type)) return type
  }
  return ''
}

export function pttBlobMimeType(recorderMimeType: string | undefined): string {
  const mime = recorderMimeType?.trim()
  if (mime) return mime
  return PTT_FALLBACK_MIME
}

/** Filename + Content-Type hint for multipart upload. */
export function pttUploadFilename(clientId: string, blobType: string): string {
  const type = blobType.toLowerCase()
  if (type.includes('mp4') || type.includes('m4a') || type.includes('aac')) {
    return `ptt-${clientId}.m4a`
  }
  if (type.includes('webm')) return `ptt-${clientId}.webm`
  if (type.includes('mpeg') || type.includes('mp3')) return `ptt-${clientId}.mp3`
  return `ptt-${clientId}.m4a`
}

/** Hub rejects clips ≤44 bytes; mobile uses 800 as a practical floor. */
export const MIN_PTT_BLOB_BYTES = 800

export function finalizePttBlob(chunks: BlobPart[], recorderMimeType: string | undefined): Blob {
  return new Blob(chunks, { type: pttBlobMimeType(recorderMimeType) })
}

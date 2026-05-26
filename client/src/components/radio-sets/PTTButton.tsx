import { useCallback, useEffect, useRef, useState } from 'react'
import { api, ApiError } from '../../lib/api'

const MIN_DURATION_MS = 300
const MAX_DURATION_MS = 30_000

type State = 'idle' | 'recording' | 'uploading' | 'error'

function pickMimeType(): string {
  const candidates = ['audio/webm;codecs=opus', 'audio/webm', 'audio/mp4']
  for (const type of candidates) {
    if (globalThis.MediaRecorder && MediaRecorder.isTypeSupported(type)) return type
  }
  return ''
}

function newClientId(): string {
  if (globalThis.crypto?.randomUUID) return globalThis.crypto.randomUUID()
  return `ptt-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`
}

type Props = Readonly<{
  radioSetId: string
  disabled?: boolean
}>

export function PTTButton({ radioSetId, disabled }: Props) {
  const [state, setState] = useState<State>('idle')
  const [error, setError] = useState<string>('')
  const recorderRef = useRef<MediaRecorder | null>(null)
  const chunksRef = useRef<Blob[]>([])
  const streamRef = useRef<MediaStream | null>(null)
  const startedAtRef = useRef<number>(0)
  const maxTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const releaseLockRef = useRef<boolean>(false)

  const stopStream = useCallback(() => {
    if (streamRef.current) {
      for (const track of streamRef.current.getTracks()) track.stop()
      streamRef.current = null
    }
    if (maxTimerRef.current) {
      clearTimeout(maxTimerRef.current)
      maxTimerRef.current = null
    }
  }, [])

  const startRecording = useCallback(async () => {
    if (state !== 'idle' || disabled || releaseLockRef.current) return
    setError('')
    try {
      const stream = await navigator.mediaDevices.getUserMedia({ audio: true })
      streamRef.current = stream
      const mimeType = pickMimeType()
      const recorder = mimeType ? new MediaRecorder(stream, { mimeType }) : new MediaRecorder(stream)
      chunksRef.current = []
      recorder.ondataavailable = (event) => {
        if (event.data.size > 0) chunksRef.current.push(event.data)
      }
      recorder.onstop = async () => {
        const elapsed = Date.now() - startedAtRef.current
        stopStream()
        if (elapsed < MIN_DURATION_MS) {
          setState('idle')
          return
        }
        const blob = new Blob(chunksRef.current, { type: recorder.mimeType || 'audio/webm' })
        chunksRef.current = []
        setState('uploading')
        try {
          await api.uploadPTT(radioSetId, blob, elapsed / 1000, newClientId())
          setState('idle')
        } catch (err) {
          const message = err instanceof ApiError ? `Upload failed (${err.status})` : 'Upload failed'
          setError(message)
          setState('error')
          globalThis.setTimeout(() => setState((current) => (current === 'error' ? 'idle' : current)), 2500)
        }
      }
      recorderRef.current = recorder
      startedAtRef.current = Date.now()
      recorder.start()
      setState('recording')
      maxTimerRef.current = globalThis.setTimeout(() => {
        if (recorderRef.current?.state === 'recording') recorderRef.current.stop()
      }, MAX_DURATION_MS)
    } catch (err) {
      stopStream()
      setError(err instanceof Error && err.name === 'NotAllowedError' ? 'Mic permission denied' : 'Mic unavailable')
      setState('error')
      globalThis.setTimeout(() => setState((current) => (current === 'error' ? 'idle' : current)), 2500)
    }
  }, [disabled, radioSetId, state, stopStream])

  const stopRecording = useCallback(() => {
    releaseLockRef.current = false
    const recorder = recorderRef.current
    if (recorder && recorder.state === 'recording') recorder.stop()
  }, [])

  useEffect(() => {
    function shouldIgnoreKey(event: KeyboardEvent): boolean {
      if (event.code !== 'Space') return true
      if (event.repeat) return true
      const target = event.target as HTMLElement | null
      if (!target) return false
      const tag = target.tagName
      if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true
      if (target.isContentEditable) return true
      return false
    }
    function onKeyDown(event: KeyboardEvent) {
      if (shouldIgnoreKey(event)) return
      event.preventDefault()
      releaseLockRef.current = false
      void startRecording()
    }
    function onKeyUp(event: KeyboardEvent) {
      if (event.code !== 'Space') return
      const target = event.target as HTMLElement | null
      if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable)) return
      event.preventDefault()
      stopRecording()
    }
    globalThis.addEventListener('keydown', onKeyDown)
    globalThis.addEventListener('keyup', onKeyUp)
    return () => {
      globalThis.removeEventListener('keydown', onKeyDown)
      globalThis.removeEventListener('keyup', onKeyUp)
    }
  }, [startRecording, stopRecording])

  useEffect(() => () => stopStream(), [stopStream])

  const labelByState: Record<State, string> = {
    idle: 'PTT — hold to talk',
    recording: 'TRANSMITTING…',
    uploading: 'SENDING…',
    error: error || 'ERROR',
  }
  const colorClass =
    state === 'recording'
      ? 'border-console-error text-console-error bg-console-error/10'
      : state === 'uploading'
        ? 'border-console-accent text-console-accent'
        : state === 'error'
          ? 'border-console-error text-console-error'
          : 'border-console-border text-console-muted hover:border-console-accent hover:text-console-accent'

  return (
    <button
      type="button"
      onMouseDown={() => void startRecording()}
      onMouseUp={stopRecording}
      onMouseLeave={() => {
        if (recorderRef.current?.state === 'recording') stopRecording()
      }}
      onTouchStart={(event) => {
        event.preventDefault()
        void startRecording()
      }}
      onTouchEnd={(event) => {
        event.preventDefault()
        stopRecording()
      }}
      onContextMenu={(event) => event.preventDefault()}
      disabled={disabled || state === 'uploading'}
      className={`px-3 py-2 sm:py-1 border rounded text-[11px] uppercase tracking-wider select-none disabled:opacity-30 disabled:cursor-not-allowed ${colorClass}`}
      title="Hold to transmit (or hold spacebar)"
    >
      {labelByState[state]}
    </button>
  )
}

export type WebSocketClientOptions<TMessage> = {
  url: string
  reconnectIntervalMs?: number
  maxReconnectAttempts?: number
  onConnect?: () => void
  onDisconnect?: () => void
  onMessage?: (message: TMessage) => void
}

export class WebSocketClient<TMessage = unknown> {
  private socket: WebSocket | null = null
  private reconnectAttempts = 0
  private isManualClose = false

  constructor(private readonly options: WebSocketClientOptions<TMessage>) {}

  connect() {
    if (this.socket && this.socket.readyState <= WebSocket.OPEN) {
      return
    }

    this.isManualClose = false
    const protocol = globalThis.window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const url = this.options.url.startsWith('ws')
      ? this.options.url
      : `${protocol}//${globalThis.window.location.host}${this.options.url}`

    this.socket = new WebSocket(url)

    this.socket.addEventListener('open', () => {
      this.reconnectAttempts = 0
      this.options.onConnect?.()
    })

    this.socket.addEventListener('message', (event) => {
      try {
        const parsed = JSON.parse(event.data) as TMessage
        this.options.onMessage?.(parsed)
      } catch {
        // ignore invalid message payloads
      }
    })

    this.socket.addEventListener('close', () => {
      this.options.onDisconnect?.()
      if (!this.isManualClose) {
        this.scheduleReconnect()
      }
    })

    this.socket.addEventListener('error', () => {
      if (!this.isManualClose) {
        this.scheduleReconnect()
      }
    })
  }

  disconnect() {
    this.isManualClose = true
    this.socket?.close()
    this.socket = null
  }

  send<TPayload>(payload: TPayload) {
    if (this.socket?.readyState !== WebSocket.OPEN) {
      return false
    }

    this.socket.send(JSON.stringify(payload))
    return true
  }

  private scheduleReconnect() {
    const maxAttempts = this.options.maxReconnectAttempts
    if (maxAttempts !== undefined && maxAttempts > 0 && this.reconnectAttempts >= maxAttempts) {
      return
    }

    this.reconnectAttempts += 1
    const baseInterval = this.options.reconnectIntervalMs ?? 1000
    const interval = Math.min(10000, baseInterval * this.reconnectAttempts)
    const jitter = Math.floor(Math.random() * 250)
    globalThis.window.setTimeout(() => this.connect(), interval + jitter)
  }
}

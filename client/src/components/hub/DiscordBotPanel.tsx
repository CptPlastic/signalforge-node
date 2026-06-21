import { useCallback, useEffect, useState } from 'react'
import { api, type DiscordBotStatusResponse } from '../../lib/api'
import { fmtDateTime } from '../../lib/format'

type Props = Readonly<{
  onNotify: (message: string) => void
}>

export function DiscordBotPanel({ onNotify }: Props) {
  const [status, setStatus] = useState<DiscordBotStatusResponse | null>(null)
  const [loading, setLoading] = useState(false)

  const refresh = useCallback(async () => {
    setLoading(true)
    try {
      setStatus(await api.discordStatus())
    } catch (err) {
      console.error(err)
      onNotify('Could not load Discord bot status')
    } finally {
      setLoading(false)
    }
  }, [onNotify])

  useEffect(() => {
    void refresh()
    const id = window.setInterval(() => void refresh(), 30_000)
    return () => window.clearInterval(id)
  }, [refresh])

  const bot = status?.status

  return (
    <div className="border border-console-border rounded p-3 flex flex-col gap-3">
      <div className="flex items-center justify-between gap-2 flex-wrap">
        <div>
          <p className="console-label text-xs">DISCORD BOT</p>
          <p className="text-[11px] text-console-muted">Community bot status and setup.</p>
        </div>
        <button
          type="button"
          onClick={() => void refresh()}
          disabled={loading}
          className="px-2 py-1 border border-console-border text-console-muted rounded text-xs hover:border-console-accent hover:text-console-accent disabled:opacity-50"
        >
          {loading ? 'REFRESHING…' : 'REFRESH'}
        </button>
      </div>

      <div className="grid gap-2 md:grid-cols-2 text-xs">
        <div>
          <span className="text-console-muted">Status: </span>
          <span className={status?.online ? 'text-emerald-400' : 'text-amber-400'}>
            {!status?.configured ? 'NOT LINKED' : status.online ? 'ONLINE' : 'OFFLINE'}
          </span>
        </div>
        <div>
          <span className="text-console-muted">Bot: </span>
          <span className="text-console-text">{bot?.botUserTag || '—'}</span>
        </div>
        <div>
          <span className="text-console-muted">Server: </span>
          <span className="text-console-text">{bot?.guildName || '—'}</span>
        </div>
        <div>
          <span className="text-console-muted">Commands: </span>
          <span className="text-console-text">{bot?.commandCount ?? '—'}</span>
        </div>
        <div>
          <span className="text-console-muted">Welcome msgs: </span>
          <span className="text-console-text">{bot?.welcomeEnabled ? 'ON' : 'OFF'}</span>
        </div>
        <div>
          <span className="text-console-muted">Last seen: </span>
          <span className="text-console-text">
            {bot?.lastSeenAt ? fmtDateTime(bot.lastSeenAt) : '—'}
          </span>
        </div>
      </div>

      {!status?.configured && (
        <div className="text-[11px] text-console-muted border border-console-border rounded p-2 space-y-1">
          <p className="text-console-text font-bold">Link bot to this hub</p>
          <p>
            Set the same random secret on <code className="text-console-accent">api</code> and{' '}
            <code className="text-console-accent">discord-bot</code>:
          </p>
          <p>
            <code className="text-console-accent">DISCORD_BOT_WORKER_TOKEN=&lt;long-random-string&gt;</code>
          </p>
          <p>Redeploy both containers. The bot reports status here automatically.</p>
        </div>
      )}

      <div className="text-[11px] text-console-muted border border-console-border rounded p-2 space-y-1">
        <p className="text-console-text font-bold">Secrets (Portainer stack env)</p>
        <ul className="list-disc list-inside space-y-0.5">
          <li>
            <code className="text-console-accent">DISCORD_TOKEN</code> — Developer Portal → Bot → Token
          </li>
          <li>
            <code className="text-console-accent">DISCORD_CLIENT_ID</code> — Application ID
          </li>
          <li>
            <code className="text-console-accent">DISCORD_GUILD_ID</code> — Server ID
          </li>
          <li>
            <code className="text-console-accent">WELCOME_CHANNEL_ID</code> — optional (needs Server Members Intent)
          </li>
        </ul>
        <a
          href="https://discord.com/developers/applications"
          target="_blank"
          rel="noreferrer"
          className="inline-block text-console-accent hover:underline mt-1"
        >
          Open Discord Developer Portal →
        </a>
      </div>
    </div>
  )
}

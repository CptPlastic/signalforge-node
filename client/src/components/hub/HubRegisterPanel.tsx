import { useCallback, useMemo, useState } from 'react'
import type { HubIdentity } from '../../lib/api'
import {
  allPreflightPassed,
  buildDirectoryListingRequest,
  buildRegisterHubUrl,
  listingPreflightChecks,
  type DirectoryListingRequest,
} from '../../lib/hubDirectoryListing'
import { directoryStatusClass, trustTextClass } from '../../lib/hubStatus'

type HubRegisterPanelProps = Readonly<{
  hubIdentity: HubIdentity | null
  hubDraft: Pick<HubIdentity, 'name' | 'publicUrl' | 'region' | 'contact'>
  hubVersion: string
  hubLoading: boolean
  onGenerateKey: () => void | Promise<void>
  onRefreshDirectory: () => void | Promise<void>
  onNotify: (message: string) => void
}>

function copyToClipboard(text: string) {
  void navigator.clipboard.writeText(text)
}

function normalizePublicUrl(value: string): string {
  return value.trim().replace(/\/+$/, '')
}

export function HubRegisterPanel({
  hubIdentity,
  hubDraft,
  hubVersion,
  hubLoading,
  onGenerateKey,
  onRefreshDirectory,
  onNotify,
}: HubRegisterPanelProps) {
  const [healthOk, setHealthOk] = useState<boolean | null>(null)
  const [healthDetail, setHealthDetail] = useState('')
  const [healthLoading, setHealthLoading] = useState(false)

  const listingHub = useMemo(() => {
    if (!hubIdentity) return null
    return {
      hubId: hubIdentity.hubId,
      name: hubDraft.name || hubIdentity.name,
      publicUrl: hubDraft.publicUrl || hubIdentity.publicUrl,
      region: hubDraft.region || hubIdentity.region,
      contact: hubDraft.contact || hubIdentity.contact,
      publicKey: hubIdentity.publicKey,
    }
  }, [hubDraft, hubIdentity])

  const checks = useMemo(
    () => (listingHub ? listingPreflightChecks(listingHub, { healthOk, healthDetail }) : []),
    [listingHub, healthOk, healthDetail],
  )
  const ready = listingHub ? allPreflightPassed(checks) : false

  const listingRequest: DirectoryListingRequest | null = useMemo(() => {
    if (!listingHub || !ready) return null
    return buildDirectoryListingRequest(listingHub, hubVersion)
  }, [listingHub, hubVersion, ready])

  const registerUrl = listingRequest ? buildRegisterHubUrl(listingRequest) : null

  const checkReachability = useCallback(async () => {
    if (!listingHub?.publicUrl.trim()) {
      setHealthOk(false)
      setHealthDetail('Set a public URL first.')
      return
    }
    const base = normalizePublicUrl(listingHub.publicUrl)
    setHealthLoading(true)
    setHealthDetail('')
    try {
      const response = await fetch(`${base}/api/v1/health`, { method: 'GET', mode: 'cors' })
      if (!response.ok) {
        setHealthOk(false)
        setHealthDetail(`Health check returned HTTP ${response.status}.`)
        return
      }
      setHealthOk(true)
      setHealthDetail(`${base}/api/v1/health responded OK.`)
    } catch {
      const sameOrigin =
        typeof window !== 'undefined' &&
        normalizePublicUrl(window.location.origin) === normalizePublicUrl(base)
      if (sameOrigin) {
        try {
          const response = await fetch('/api/v1/health')
          if (response.ok) {
            setHealthOk(true)
            setHealthDetail('Local hub health OK. Publish the same URL in your reverse proxy.')
            return
          }
        } catch {
          /* fall through */
        }
      }
      setHealthOk(false)
      setHealthDetail(
        'Could not reach the public URL from this browser. Open the hub at its public URL and retry, or verify HTTPS and CORS.',
      )
    } finally {
      setHealthLoading(false)
    }
  }, [listingHub])

  if (!hubIdentity) return null

  const directoryStatus = hubIdentity.directoryValidationStatus || 'unverified'
  const isListed = directoryStatus === 'listed' || directoryStatus === 'verified'

  return (
    <div className="border border-console-border rounded p-3 flex flex-col gap-3">
      <div>
        <p className="console-label text-xs">REGISTER IN DIRECTORY</p>
        <p className="text-[11px] text-console-muted mt-1">
          List this hub in the public SignalForge directory so mobile apps and other operators can discover it.
          Save hub identity first, complete the checklist, then use <strong>OPEN REGISTRATION FORM</strong> and
          click <strong>Submit to Directory</strong> for one-click review.
        </p>
      </div>

      <div className="grid gap-2 text-xs">
        <div className="text-console-muted">
          Status:{' '}
          <span className={`${directoryStatusClass(directoryStatus)} uppercase`}>{directoryStatus}</span>
          {' · '}
          Trust: <span className={`${trustTextClass(hubIdentity.trustLevel)} uppercase`}>{hubIdentity.trustLevel}</span>
        </div>
        {isListed ? (
          <p className="text-console-accent text-[11px]">
            This hub is in the directory feed. Use CHECK DIRECTORY after feed updates to refresh local trust fields.
          </p>
        ) : (
          <p className="text-[11px] text-console-muted">
            Unlisted hubs still work — registration adds discovery in the mobile app and public trust metadata.
          </p>
        )}
      </div>

      <ol className="flex flex-col gap-1.5 text-[11px] list-decimal list-inside">
        {checks.map((check) => (
          <li key={check.id} className={check.ok ? 'text-console-accent' : 'text-console-muted'}>
            <span className="text-console-text">{check.label}</span>
            {!check.ok && <span className="text-console-error"> — {check.detail}</span>}
          </li>
        ))}
      </ol>

      <div className="flex gap-2 flex-wrap">
        {!hubIdentity.publicKey && (
          <button
            type="button"
            onClick={() => void onGenerateKey()}
            disabled={hubLoading}
            className="px-2 py-1 border border-console-border text-console-muted rounded text-[10px] hover:border-console-accent hover:text-console-accent disabled:opacity-50"
          >
            GENERATE KEY
          </button>
        )}
        <button
          type="button"
          onClick={() => void checkReachability()}
          disabled={hubLoading || healthLoading || !listingHub?.publicUrl.trim()}
          className="px-2 py-1 border border-console-border text-console-muted rounded text-[10px] hover:border-console-accent hover:text-console-accent disabled:opacity-50"
        >
          {healthLoading ? 'CHECKING…' : 'CHECK REACHABILITY'}
        </button>
        <button
          type="button"
          onClick={() => void onRefreshDirectory()}
          disabled={hubLoading}
          className="px-2 py-1 border border-console-border text-console-muted rounded text-[10px] hover:border-console-accent hover:text-console-accent disabled:opacity-50"
        >
          CHECK DIRECTORY
        </button>
      </div>

      <div className="flex gap-2 flex-wrap">
        <button
          type="button"
          disabled={!listingRequest}
          onClick={() => {
            if (!listingRequest) return
            copyToClipboard(JSON.stringify(listingRequest, null, 2))
            onNotify('Directory listing request copied')
          }}
          className="px-2 py-1 border border-console-border text-console-muted rounded text-[10px] hover:border-console-accent hover:text-console-accent disabled:opacity-50"
        >
          COPY LISTING JSON
        </button>
        {registerUrl && (
          <a
            href={registerUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="px-2 py-1 border border-console-accent text-console-accent rounded text-[10px] hover:bg-console-accent hover:bg-opacity-10"
          >
            OPEN REGISTRATION FORM ↗
          </a>
        )}
      </div>

      {!ready && (
        <p className="text-[11px] text-console-muted">
          Complete every checklist item (including reachability) before submitting a listing request.
        </p>
      )}
    </div>
  )
}

import type { AuthUser, HubIdentity } from '../lib/api'
import type { OverallStatus } from '../types/app'
import { SignalForgeLogo } from './SignalForgeLogo'

type AppHeaderProps = Readonly<{
  authUser: AuthUser | null
  headerVersionLabel: string
  headerVersionTitle: string
  hasUpdateAvailable: boolean
  hubIdentity: HubIdentity | null
  isStandalone: boolean
  onInstallApp: () => void
  onShowHub: () => void
  overallStatus: OverallStatus
  showInstallButton: boolean
  updateError?: string
  updateTitle: string
}>

function PwaInstallControl({ isStandalone, onInstallApp, showInstallButton }: Readonly<{
  isStandalone: boolean
  onInstallApp: () => void
  showInstallButton: boolean
}>) {
  if (showInstallButton) {
    return (
      <button
        onClick={onInstallApp}
        className="px-2 py-1 border border-console-accent text-console-accent rounded uppercase tracking-wider hover:bg-console-accent hover:bg-opacity-10"
        title="Install P7 Scanner as an app"
      >
        INSTALL APP
      </button>
    )
  }

  if (isStandalone) {
    return <span className="console-label" title="running as an installed app">APP MODE</span>
  }

  return null
}

export function AppHeader({
  authUser,
  headerVersionLabel,
  headerVersionTitle,
  hasUpdateAvailable,
  hubIdentity,
  isStandalone,
  onInstallApp,
  onShowHub,
  overallStatus,
  showInstallButton,
  updateError,
  updateTitle,
}: AppHeaderProps) {
  const showUpdateBadge = hasUpdateAvailable || Boolean(updateError)
  const hubLabel = hubIdentity?.name?.trim() || 'SIGNALFORGE NODE CONSOLE'
  const trustLevel = hubIdentity?.trustLevel || 'community'
  const hubTitle = hubIdentity
    ? [
        `Hub: ${hubIdentity.name || 'unnamed'}`,
        hubIdentity.hubId ? `ID: ${hubIdentity.hubId}` : '',
        hubIdentity.publicUrl ? `URL: ${hubIdentity.publicUrl}` : '',
        `Trust: ${trustLevel}`,
      ].filter(Boolean).join('\n')
    : 'Hub identity not initialized'
  const updateBadgeClass = hasUpdateAvailable
    ? 'border-console-amber text-console-amber hover:bg-console-amber'
    : 'border-console-error text-console-error hover:bg-console-error'

  return (
    <header className="console-panel flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div className="flex items-center gap-3 min-w-0">
        <SignalForgeLogo className="h-10 w-10 flex-shrink-0 text-white drop-shadow-[0_0_10px_rgba(0,255,65,0.18)]" />
        <div className="min-w-0">
          <div className="text-lg font-bold tracking-widest truncate">P7 // SCAN</div>
          <div className="flex items-center gap-2 min-w-0">
            <div className="console-label text-[9px] truncate" title={hubTitle}>{hubLabel}</div>
            {(trustLevel === 'official' || trustLevel === 'verified') && (
              <span
                className={trustLevel === 'official' ? 'console-label text-[9px] text-console-accent' : 'console-label text-[9px] text-console-amber'}
                title={hubTitle}
              >
                {trustLevel === 'official' ? 'OFFICIAL' : 'VERIFIED'}
              </span>
            )}
          </div>
        </div>
      </div>
      <div className="flex items-center gap-2 sm:gap-4 text-xs flex-wrap min-w-0">
        <span className="console-label" title={headerVersionTitle}>{headerVersionLabel}</span>
        {showUpdateBadge && (
          <button
            onClick={onShowHub}
            className={`px-2 py-1 border rounded uppercase tracking-wider hover:bg-opacity-10 ${updateBadgeClass}`}
            title={updateTitle}
          >
            {hasUpdateAvailable ? 'UPDATE AVAILABLE' : 'UPDATE CHECK FAILED'}
          </button>
        )}
        <PwaInstallControl
          isStandalone={isStandalone}
          onInstallApp={onInstallApp}
          showInstallButton={showInstallButton}
        />
        <span className="flex items-center gap-2">
          <span
            className={`inline-block h-2.5 w-2.5 rounded-full ${overallStatus.dotClass}`}
            aria-hidden
            title={overallStatus.title}
          />
          {overallStatus.label}
        </span>
        <span
          className={`flex items-center gap-1 px-2 py-1 border rounded min-w-0 max-w-full ${authUser ? 'border-console-accent text-console-accent' : 'border-console-border text-console-muted'}`}
          title={authUser ? `Logged in as ${authUser.email}` : 'Not logged in'}
        >
          <span>{authUser ? '[●]' : '[○]'}</span>
          <span className="text-[10px] truncate">{authUser ? authUser.email : 'GUEST'}</span>
        </span>
      </div>
    </header>
  )
}
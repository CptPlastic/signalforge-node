import type { AuthUser } from '../lib/api'
import type { AppView } from '../types/app'

const APP_VIEWS: AppView[] = ['monitor', 'radio-sets', 'integrations', 'talkgroups', 'hub', 'account']

const VIEW_LABELS: Record<AppView, string> = {
  monitor: 'CALL LOG',
  'radio-sets': 'RADIO SETS',
  integrations: 'INTEGRATIONS',
  talkgroups: 'TALKGROUPS',
  hub: 'HUB',
  account: 'ACCOUNT',
}

type AppNavProps = Readonly<{
  activeView: AppView
  authUser: AuthUser | null
  onViewChange: (view: AppView) => void
}>

function getViewButtonClass(activeView: AppView, view: AppView, authUser: AuthUser | null): string {
  const isAvailable = Boolean(authUser) || view === 'account'
  const isAccountWhenLoggedOut = view === 'account' && !authUser

  if (isAccountWhenLoggedOut) {
    return 'border-console-accent text-console-accent bg-console-accent bg-opacity-5 account-tab-attn font-bold'
  }

  if (!isAvailable) {
    return 'border-console-border text-console-muted opacity-50 cursor-not-allowed'
  }

  if (activeView === view) {
    return 'border-console-accent text-console-accent bg-console-surface'
  }

  return 'border-console-border text-console-muted hover:border-console-accent hover:text-console-accent'
}

export function AppNav({ activeView, authUser, onViewChange }: AppNavProps) {
  return (
    <div className="console-panel grid grid-cols-2 gap-2 text-xs sm:flex sm:items-center sm:flex-wrap">
      {APP_VIEWS.map((view) => {
        const isAvailable = Boolean(authUser) || view === 'account'
        return (
          <button
            key={view}
            onClick={() => isAvailable && onViewChange(view)}
            className={`min-w-0 px-2.5 py-1 border rounded uppercase tracking-wider transition-all text-[10px] sm:text-xs ${getViewButtonClass(activeView, view, authUser)}`}
            disabled={!isAvailable}
          >
            {VIEW_LABELS[view]}
          </button>
        )
      })}
    </div>
  )
}
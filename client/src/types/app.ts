export type AppView = 'monitor' | 'radio-sets' | 'integrations' | 'talkgroups' | 'hub' | 'account'

const APP_VIEWS: AppView[] = ['monitor', 'radio-sets', 'integrations', 'talkgroups', 'hub', 'account']

export function parseAppViewParam(value: string | null): AppView | null {
  if (!value) return null
  return APP_VIEWS.includes(value as AppView) ? (value as AppView) : null
}

export function readInitialAppView(): AppView {
  if (typeof globalThis.window === 'undefined') return 'monitor'
  return parseAppViewParam(new URLSearchParams(globalThis.window.location.search).get('view')) ?? 'monitor'
}

export type OverallStatus = {
  dotClass: string
  title: string
  label: string
}
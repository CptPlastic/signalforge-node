import type { ReactNode } from 'react'
import type { AuthUser } from '../lib/api'
import type { AppView } from '../types/app'

type ActiveViewProps = Readonly<{
  activeView: AppView
  children: ReactNode
  view: AppView
}>

type AuthenticatedViewProps = ActiveViewProps & Readonly<{
  authUser: AuthUser | null
}>

export function ActiveView({ activeView, children, view }: ActiveViewProps) {
  if (activeView !== view) return null
  return <>{children}</>
}

export function AuthenticatedView({ activeView, authUser, children, view }: AuthenticatedViewProps) {
  if (!authUser || activeView !== view) return null
  return <>{children}</>
}
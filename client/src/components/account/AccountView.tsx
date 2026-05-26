import type { Dispatch, SetStateAction } from 'react'
import type { AuthUser, AuditLogEntry, UserRecord } from '../../lib/api'
import { fmtDateTime } from '../../lib/format'
import { updateUserRoleDraft, updateUserStatusDraft, updateUserTxEnabledDraft } from '../../lib/userDrafts'
import type { AppView } from '../../types/app'
import { ActiveView } from '../ActiveView'

type AccountViewProps = Readonly<{
  activeView: AppView
  authUser: AuthUser | null
  authLoading: boolean
  authEmail: string
  authToken: string
  authMessage: string
  authError: string
  awaitingMagicLink: boolean
  users: UserRecord[]
  setUsers: Dispatch<SetStateAction<UserRecord[]>>
  usersLoading: boolean
  userActionID: string | null
  auditLogs: AuditLogEntry[]
  auditLoading: boolean
  onAuthEmailChange: (email: string) => void
  onAuthTokenChange: (token: string) => void
  onRequestMagicLink: () => void | Promise<void>
  onVerifyMagicLinkToken: () => void | Promise<void>
  onLogoutSession: () => void | Promise<void>
  onRefreshUsers: () => void | Promise<void>
  onSaveUser: (user: UserRecord) => void | Promise<void>
  onApproveUser: (user: UserRecord) => void | Promise<void>
  onRemoveUser: (user: UserRecord) => void | Promise<void>
  onRefreshAuditLogs: () => void | Promise<void>
}>

type SessionPanelProps = Pick<AccountViewProps, 'authUser' | 'authLoading' | 'onLogoutSession'>

type AuthAccessCardProps = Pick<
  AccountViewProps,
  | 'authLoading'
  | 'authEmail'
  | 'authToken'
  | 'authMessage'
  | 'authError'
  | 'awaitingMagicLink'
  | 'onAuthEmailChange'
  | 'onAuthTokenChange'
  | 'onRequestMagicLink'
  | 'onVerifyMagicLinkToken'
>

type UserManagementPanelProps = Pick<
  AccountViewProps,
  | 'users'
  | 'setUsers'
  | 'usersLoading'
  | 'userActionID'
  | 'onRefreshUsers'
  | 'onSaveUser'
  | 'onApproveUser'
  | 'onRemoveUser'
>

type AuditLogPanelProps = Pick<AccountViewProps, 'auditLogs' | 'auditLoading' | 'onRefreshAuditLogs'>

function SessionPanel({ authUser, authLoading, onLogoutSession }: SessionPanelProps) {
  if (!authUser) return null

  return (
    <div className="border border-console-border rounded p-3">
      <p className="console-label text-xs mb-2">SESSION</p>
      <div className="text-xs flex flex-col gap-2">
        <div className="text-console-muted">Email: <span className="text-console-text">{authUser.email}</span></div>
        <div className="text-console-muted">Role: <span className="text-console-accent uppercase">{authUser.role}</span></div>
        <button
          onClick={onLogoutSession}
          className="w-fit px-2 py-1 border border-console-error text-console-error rounded text-[11px] hover:bg-console-error hover:bg-opacity-10"
          disabled={authLoading}
        >
          {authLoading ? 'WORKING...' : 'LOGOUT'}
        </button>
      </div>
    </div>
  )
}

function AuthAccessCard({
  authLoading,
  authEmail,
  authToken,
  authMessage,
  authError,
  awaitingMagicLink,
  onAuthEmailChange,
  onAuthTokenChange,
  onRequestMagicLink,
  onVerifyMagicLinkToken,
}: AuthAccessCardProps) {
  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col gap-4">
      <div className="border border-console-border rounded p-4 sm:p-5 flex flex-col gap-4">
        <div className="flex flex-col gap-2 text-center sm:text-left">
          <p className="console-label text-xs">ACCOUNT ACCESS</p>
          <h2 className="text-sm sm:text-base text-console-text">Sign in with your email and finish the token step in one place.</h2>
          <p className="text-[11px] text-console-muted">Request the magic link, then paste the token from your inbox below.</p>
        </div>

        <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)] lg:items-start">
          <div className="border border-console-border rounded p-3 flex flex-col gap-3 bg-console-bg/30">
            <p className="console-label text-xs">STEP 1: REQUEST LOGIN</p>
            <p className="text-[11px] text-console-muted">Enter your email address and request a magic link.</p>
            <input
              value={authEmail}
              onChange={(event) => onAuthEmailChange(event.target.value)}
              placeholder="your.email@example.com"
              className="bg-console-bg border border-console-border rounded px-2 py-1 text-xs outline-none focus:border-console-accent"
              disabled={authLoading}
            />
            <button
              onClick={onRequestMagicLink}
              className="w-full sm:w-fit px-2 py-1 border border-console-accent text-console-accent rounded text-xs hover:bg-console-accent hover:bg-opacity-10 disabled:opacity-50"
              disabled={authLoading || !authEmail.trim()}
            >
              {authLoading ? 'WORKING...' : 'REQUEST MAGIC LINK'}
            </button>
          </div>

          <div className="border border-console-border rounded p-3 flex flex-col gap-3 bg-console-bg/30">
            <p className="console-label text-xs">STEP 2: VERIFY TOKEN</p>
            <p className="text-[11px] text-console-muted">
              {awaitingMagicLink
                ? 'Magic link requested. Copy the token from your email and paste it below.'
                : 'Check your email for a link. Copy the token and paste it below.'}
            </p>
            <input
              value={authToken}
              onChange={(event) => onAuthTokenChange(event.target.value)}
              placeholder="paste token from email"
              className="bg-console-bg border border-console-border rounded px-2 py-1 text-xs outline-none focus:border-console-accent"
              disabled={authLoading}
            />
            <button
              onClick={onVerifyMagicLinkToken}
              className="w-full sm:w-fit px-2 py-1 border border-console-accent text-console-accent rounded text-xs hover:bg-console-accent hover:bg-opacity-10 disabled:opacity-50"
              disabled={authLoading || !authToken.trim()}
            >
              {authLoading ? 'WORKING...' : 'VERIFY & LOGIN'}
            </button>
          </div>
        </div>

        {authMessage && <div className="rounded border border-console-accent/40 bg-console-accent/10 px-3 py-2 text-[11px] text-console-accent">{authMessage}</div>}
        {authError && <div className="rounded border border-console-error/40 bg-console-error/10 px-3 py-2 text-[11px] text-console-error">{authError}</div>}
      </div>
    </div>
  )
}

function UserManagementPanel({
  users,
  setUsers,
  usersLoading,
  userActionID,
  onRefreshUsers,
  onSaveUser,
  onApproveUser,
  onRemoveUser,
}: UserManagementPanelProps) {
  return (
    <div className="border border-console-border rounded p-3 overflow-auto">
      <div className="flex items-center justify-between mb-2">
        <p className="console-label text-xs">USER MANAGEMENT</p>
        <button
          onClick={onRefreshUsers}
          className="px-2 py-1 border border-console-border text-console-muted rounded text-[10px] hover:border-console-accent hover:text-console-accent"
          disabled={usersLoading}
        >
          {usersLoading ? 'LOADING...' : 'REFRESH'}
        </button>
      </div>
      <table className="w-full border-collapse text-xs">
        <thead>
          <tr className="border-b border-console-border text-[10px] uppercase tracking-widest text-console-muted">
            <th className="py-1.5 px-2 text-left font-normal">Email</th>
            <th className="py-1.5 px-2 text-left font-normal">Role</th>
            <th className="py-1.5 px-2 text-left font-normal">Status</th>
            <th className="py-1.5 px-2 text-left font-normal">TX</th>
            <th className="py-1.5 px-2 text-left font-normal">Created</th>
            <th className="py-1.5 px-2 text-left font-normal">Updated</th>
            <th className="py-1.5 px-2 text-left font-normal">Actions</th>
          </tr>
        </thead>
        <tbody>
          {users.map((user) => (
            <tr key={user.id} className={`border-b border-console-border/70 ${user.status === 'pending' ? 'bg-console-accent/5' : ''}`}>
              <td className="py-2 px-2">{user.email}</td>
              <td className="py-2 px-2">
                <select
                  value={user.role}
                  onChange={(event) => setUsers(updateUserRoleDraft(user.id, event.target.value as UserRecord['role']))}
                  className="bg-console-bg border border-console-border rounded px-2 py-1 text-xs"
                >
                  <option value="admin">admin</option>
                  <option value="user">user</option>
                  <option value="guest">guest</option>
                </select>
              </td>
              <td className="py-2 px-2">
                <select
                  value={user.status}
                  onChange={(event) => setUsers(updateUserStatusDraft(user.id, event.target.value as UserRecord['status']))}
                  className="bg-console-bg border border-console-border rounded px-2 py-1 text-xs"
                >
                  <option value="active">active</option>
                  <option value="pending">pending</option>
                  <option value="disabled">disabled</option>
                </select>
              </td>
              <td className="py-2 px-2">
                <label className="flex items-center gap-1.5 text-[10px] text-console-muted cursor-pointer">
                  <input
                    type="checkbox"
                    checked={user.txEnabled}
                    onChange={(event) => setUsers(updateUserTxEnabledDraft(user.id, event.target.checked))}
                    className="accent-console-accent"
                  />
                  <span className="uppercase tracking-wider">{user.txEnabled ? 'On' : 'Off'}</span>
                </label>
              </td>
              <td className="py-2 px-2 text-console-muted">{fmtDateTime(user.createdAt)}</td>
              <td className="py-2 px-2 text-console-muted">{fmtDateTime(user.updatedAt)}</td>
              <td className="py-2 px-2">
                <div className="flex items-center gap-2">
                  {user.status === 'pending' && (
                    <button
                      onClick={() => onApproveUser(user)}
                      className="px-2 py-1 border border-console-accent text-console-accent rounded text-[10px] hover:bg-console-accent hover:bg-opacity-10"
                      disabled={userActionID === user.id}
                    >
                      APPROVE
                    </button>
                  )}
                  <button
                    onClick={() => onSaveUser(user)}
                    className="px-2 py-1 border border-console-accent text-console-accent rounded text-[10px] hover:bg-console-accent hover:bg-opacity-10"
                    disabled={userActionID === user.id}
                  >
                    SAVE
                  </button>
                  <button
                    onClick={() => onRemoveUser(user)}
                    className="px-2 py-1 border border-console-error text-console-error rounded text-[10px] hover:bg-console-error hover:bg-opacity-10"
                    disabled={userActionID === user.id}
                  >
                    DELETE
                  </button>
                </div>
              </td>
            </tr>
          ))}
          {users.length === 0 && (
            <tr>
              <td className="py-3 px-2 text-console-muted" colSpan={7}>No users</td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  )
}

function AuditLogPanel({ auditLogs, auditLoading, onRefreshAuditLogs }: AuditLogPanelProps) {
  return (
    <div className="border border-console-border rounded p-3 overflow-auto">
      <div className="flex items-center justify-between mb-2">
        <p className="console-label text-xs">AUDIT LOG</p>
        <button
          onClick={onRefreshAuditLogs}
          className="px-2 py-1 border border-console-border text-console-muted rounded text-[10px] hover:border-console-accent hover:text-console-accent"
          disabled={auditLoading}
        >
          {auditLoading ? 'LOADING...' : 'REFRESH'}
        </button>
      </div>
      <table className="w-full border-collapse text-xs">
        <thead>
          <tr className="border-b border-console-border text-[10px] uppercase tracking-widest text-console-muted">
            <th className="py-1.5 px-2 text-left font-normal">Time</th>
            <th className="py-1.5 px-2 text-left font-normal">Action</th>
            <th className="py-1.5 px-2 text-left font-normal">Target</th>
            <th className="py-1.5 px-2 text-left font-normal">Actor</th>
          </tr>
        </thead>
        <tbody>
          {auditLogs.map((entry) => (
            <tr key={entry.id} className="border-b border-console-border/70">
              <td className="py-2 px-2 text-console-muted">{fmtDateTime(entry.createdAt)}</td>
              <td className="py-2 px-2">{entry.action}</td>
              <td className="py-2 px-2 text-console-muted">{entry.targetType}:{entry.targetId}</td>
              <td className="py-2 px-2 text-console-muted">{entry.userId || 'system'}</td>
            </tr>
          ))}
          {auditLogs.length === 0 && (
            <tr>
              <td className="py-3 px-2 text-console-muted" colSpan={4}>No audit entries</td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  )
}

export function AccountView({
  activeView,
  authUser,
  authLoading,
  authEmail,
  authToken,
  authMessage,
  authError,
  awaitingMagicLink,
  users,
  setUsers,
  usersLoading,
  userActionID,
  auditLogs,
  auditLoading,
  onAuthEmailChange,
  onAuthTokenChange,
  onRequestMagicLink,
  onVerifyMagicLinkToken,
  onLogoutSession,
  onRefreshUsers,
  onSaveUser,
  onApproveUser,
  onRemoveUser,
  onRefreshAuditLogs,
}: AccountViewProps) {
  const showAdminPanels = authUser?.role === 'admin'

  return (
    <ActiveView activeView={activeView} view="account">
      <main className="console-panel flex flex-col gap-4">
        {authUser ? (
          <SessionPanel authUser={authUser} authLoading={authLoading} onLogoutSession={onLogoutSession} />
        ) : (
          <AuthAccessCard
            authLoading={authLoading}
            authEmail={authEmail}
            authToken={authToken}
            authMessage={authMessage}
            authError={authError}
            awaitingMagicLink={awaitingMagicLink}
            onAuthEmailChange={onAuthEmailChange}
            onAuthTokenChange={onAuthTokenChange}
            onRequestMagicLink={onRequestMagicLink}
            onVerifyMagicLinkToken={onVerifyMagicLinkToken}
          />
        )}

        {authUser && authMessage && <div className="text-[11px] text-console-accent">{authMessage}</div>}
        {authUser && authError && <div className="text-[11px] text-console-error">{authError}</div>}

        {showAdminPanels && (
          <UserManagementPanel
            users={users}
            setUsers={setUsers}
            usersLoading={usersLoading}
            userActionID={userActionID}
            onRefreshUsers={onRefreshUsers}
            onSaveUser={onSaveUser}
            onApproveUser={onApproveUser}
            onRemoveUser={onRemoveUser}
          />
        )}

        {showAdminPanels && (
          <AuditLogPanel
            auditLogs={auditLogs}
            auditLoading={auditLoading}
            onRefreshAuditLogs={onRefreshAuditLogs}
          />
        )}
      </main>
    </ActiveView>
  )
}

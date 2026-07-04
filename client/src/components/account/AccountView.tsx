import { useMemo, useState, type Dispatch, type SetStateAction } from 'react'
import type { AuthCapabilities, AuthUser, AuditLogEntry, UserRecord } from '../../lib/api'
import { fmtDateTime } from '../../lib/format'
import {
  updateUserDispatcherEnabledDraft,
  updateUserRoleDraft,
  updateUserStatusDraft,
  updateUserTxEnabledDraft,
} from '../../lib/userDrafts'
import type { AppView } from '../../types/app'
import { ActiveView } from '../ActiveView'
import { CallStoragePanel } from './CallStoragePanel'

type AccountViewProps = Readonly<{
  activeView: AppView
  authUser: AuthUser | null
  authLoading: boolean
  authEmail: string
  authToken: string
  authMessage: string
  authError: string
  awaitingMagicLink: boolean
  authCapabilities: AuthCapabilities | null
  authPassword: string
  users: UserRecord[]
  setUsers: Dispatch<SetStateAction<UserRecord[]>>
  usersLoading: boolean
  userActionID: string | null
  auditLogs: AuditLogEntry[]
  auditLoading: boolean
  onAuthEmailChange: (email: string) => void
  onAuthTokenChange: (token: string) => void
  onAuthPasswordChange: (password: string) => void
  onRequestMagicLink: () => void | Promise<void>
  onVerifyMagicLinkToken: () => void | Promise<void>
  onPasswordLogin: () => void | Promise<void>
  onLogoutSession: () => void | Promise<void>
  onRefreshUsers: () => void | Promise<void>
  onSaveUser: (user: UserRecord) => void | Promise<void>
  onApproveUser: (user: UserRecord) => void | Promise<void>
  onSetUserPassword: (user: UserRecord, password: string) => void | Promise<void>
  onRemoveUser: (user: UserRecord) => void | Promise<void>
  onRefreshAuditLogs: () => void | Promise<void>
  onOpenHub?: () => void
}>

type SessionPanelProps = Pick<AccountViewProps, 'authUser' | 'authLoading' | 'onLogoutSession'>

type AuthAccessCardProps = Pick<
  AccountViewProps,
  | 'authLoading'
  | 'authEmail'
  | 'authToken'
  | 'authPassword'
  | 'authMessage'
  | 'authError'
  | 'awaitingMagicLink'
  | 'authCapabilities'
  | 'onAuthEmailChange'
  | 'onAuthTokenChange'
  | 'onAuthPasswordChange'
  | 'onRequestMagicLink'
  | 'onVerifyMagicLinkToken'
  | 'onPasswordLogin'
>

type UserManagementPanelProps = Pick<
  AccountViewProps,
  | 'authCapabilities'
  | 'users'
  | 'setUsers'
  | 'usersLoading'
  | 'userActionID'
  | 'onRefreshUsers'
  | 'onSaveUser'
  | 'onApproveUser'
  | 'onSetUserPassword'
  | 'onRemoveUser'
>

type AuditLogPanelProps = Pick<AccountViewProps, 'auditLogs' | 'auditLoading' | 'onRefreshAuditLogs'>

function formatAuditIdentity(email: string | undefined, id: string | undefined): string {
  const normalizedEmail = (email || '').trim()
  const normalizedID = (id || '').trim()
  if (normalizedEmail && normalizedID) return `${normalizedEmail} (${normalizedID})`
  if (normalizedEmail) return normalizedEmail
  if (normalizedID) return normalizedID
  return 'system'
}

function AdminOperatorPanel({ onOpenHub }: { onOpenHub?: () => void }) {
  return (
    <div className="border border-console-border rounded p-3 flex flex-col gap-2">
      <p className="console-label text-xs">ADMIN OPERATOR TASKS</p>
      <p className="text-[11px] text-console-muted">
        Manage users below, then use the HUB tab to register this cell in the public directory or connect federation peers.
      </p>
      <div className="flex gap-2 flex-wrap">
        {onOpenHub && (
          <button
            type="button"
            onClick={onOpenHub}
            className="px-2 py-1 border border-console-accent text-console-accent rounded text-[10px] hover:bg-console-accent hover:bg-opacity-10"
          >
            OPEN HUB ADMIN
          </button>
        )}
        <a
          href="https://signalforge.org/register-hub.html"
          target="_blank"
          rel="noopener noreferrer"
          className="px-2 py-1 border border-console-border text-console-muted rounded text-[10px] hover:border-console-accent hover:text-console-accent"
        >
          DIRECTORY REGISTRATION ↗
        </a>
      </div>
    </div>
  )
}

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
  authPassword,
  authMessage,
  authError,
  awaitingMagicLink,
  authCapabilities,
  onAuthEmailChange,
  onAuthTokenChange,
  onAuthPasswordChange,
  onRequestMagicLink,
  onVerifyMagicLinkToken,
  onPasswordLogin,
}: AuthAccessCardProps) {
  const passwordEnabled = authCapabilities?.passwordLoginEnabled ?? false
  const emailEnabled = authCapabilities?.emailDeliveryConfigured ?? true

  return (
    <div className="mx-auto flex w-full max-w-2xl flex-col gap-4">
      {passwordEnabled && (
        <div className="border border-console-border rounded p-4 sm:p-5 flex flex-col gap-3">
          <div className="flex flex-col gap-2 text-center sm:text-left">
            <p className="console-label text-xs">PASSWORD SIGN IN</p>
            <h2 className="text-sm sm:text-base text-console-text">Sign in with email and password.</h2>
            <p className="text-[11px] text-console-muted">For off-grid hubs without email delivery.</p>
          </div>
          <input
            value={authEmail}
            onChange={(event) => onAuthEmailChange(event.target.value)}
            placeholder="your.email@example.com"
            type="email"
            autoComplete="email"
            inputMode="email"
            className="bg-console-bg border border-console-border rounded px-2 py-1 text-xs outline-none focus:border-console-accent"
            disabled={authLoading}
          />
          <input
            value={authPassword}
            onChange={(event) => onAuthPasswordChange(event.target.value)}
            placeholder="Password"
            type="password"
            autoComplete="current-password"
            className="bg-console-bg border border-console-border rounded px-2 py-1 text-xs outline-none focus:border-console-accent"
            disabled={authLoading}
          />
          <button
            onClick={onPasswordLogin}
            className="w-full sm:w-fit px-2 py-1 border border-console-accent text-console-accent rounded text-xs hover:bg-console-accent hover:bg-opacity-10 disabled:opacity-50"
            disabled={authLoading || !authEmail.trim() || !authPassword}
          >
            {authLoading ? 'WORKING...' : 'SIGN IN'}
          </button>
        </div>
      )}

      {emailEnabled && (
      <div className="border border-console-border rounded p-4 sm:p-5 flex flex-col gap-4">
        <div className="flex flex-col gap-2 text-center sm:text-left">
          <p className="console-label text-xs">CREATE ACCOUNT / SIGN IN</p>
          <h2 className="text-sm sm:text-base text-console-text">New here? Enter your email — we&apos;ll create your account and send a code.</h2>
          <p className="text-[11px] text-console-muted">Returning users: same flow — request a code, then enter the 6 digits from your email.</p>
        </div>

        <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)] lg:items-start">
          <div className="border border-console-border rounded p-3 flex flex-col gap-3 bg-console-bg/30">
            <p className="console-label text-xs">STEP 1: YOUR EMAIL</p>
            <p className="text-[11px] text-console-muted">First-time sign-up starts here — no separate registration form.</p>
            <input
              value={authEmail}
              onChange={(event) => onAuthEmailChange(event.target.value)}
              placeholder="your.email@example.com"
              type="email"
              autoComplete="email"
              inputMode="email"
              className="bg-console-bg border border-console-border rounded px-2 py-1 text-xs outline-none focus:border-console-accent"
              disabled={authLoading}
            />
            <button
              onClick={onRequestMagicLink}
              className="w-full sm:w-fit px-2 py-1 border border-console-accent text-console-accent rounded text-xs hover:bg-console-accent hover:bg-opacity-10 disabled:opacity-50"
              disabled={authLoading || !authEmail.trim()}
            >
              {authLoading ? 'WORKING...' : 'SEND CODE'}
            </button>
            <a
              href="https://signalforge.org/join.html"
              target="_blank"
              rel="noopener noreferrer"
              className="text-[10px] text-console-muted hover:text-console-accent underline-offset-2 hover:underline"
            >
              Step-by-step join guide ↗
            </a>
          </div>

          <div className="border border-console-border rounded p-3 flex flex-col gap-3 bg-console-bg/30">
            <p className="console-label text-xs">STEP 2: ENTER CODE</p>
            <p className="text-[11px] text-console-muted">
              {awaitingMagicLink
                ? 'Code sent. Enter the 6-digit code from your email below.'
                : 'Check your email for a 6-digit code and enter it below.'}
            </p>
            <input
              value={authToken}
              onChange={(event) => onAuthTokenChange(event.target.value)}
              placeholder="123456"
              type="text"
              inputMode="numeric"
              autoComplete="one-time-code"
              maxLength={64}
              className="bg-console-bg border border-console-border rounded px-2 py-1 text-xs tracking-widest outline-none focus:border-console-accent"
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
      )}

      {authCapabilities && !passwordEnabled && (
        <p className="text-[10px] text-console-muted text-center sm:text-left">
          Password sign-in is off on this hub (<code className="text-console-text">passwordLoginEnabled: false</code>).
          Create <code className="text-console-text">.env.production</code> beside your compose file on the server with{' '}
          <code className="text-console-text">AUTH_BOOTSTRAP_EMAIL</code>, <code className="text-console-text">AUTH_BOOTSTRAP_PASSWORD</code>, and{' '}
          <code className="text-console-text">AUTH_PASSWORD_LOGIN_ENABLED=true</code>, then <strong>Update stack → force-recreate</strong> (restart alone is not enough).
        </p>
      )}

      {!passwordEnabled && !emailEnabled && (
        <div className="border border-console-error/40 rounded p-4 text-[11px] text-console-error">
          No sign-in methods are configured on this hub. Set password login or Mailjet credentials.
        </div>
      )}
    </div>
  )
}

function UserManagementPanel({
  authCapabilities,
  users,
  setUsers,
  usersLoading,
  userActionID,
  onRefreshUsers,
  onSaveUser,
  onApproveUser,
  onSetUserPassword,
  onRemoveUser,
}: UserManagementPanelProps) {
  const passwordLoginEnabled = authCapabilities?.passwordLoginEnabled ?? false
  const [passwordUserID, setPasswordUserID] = useState<string | null>(null)
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')

  function openPasswordForm(userID: string) {
    setPasswordUserID(userID)
    setNewPassword('')
    setConfirmPassword('')
  }

  function closePasswordForm() {
    setPasswordUserID(null)
    setNewPassword('')
    setConfirmPassword('')
  }

  async function submitPassword(user: UserRecord) {
    if (newPassword.length < 8) {
      globalThis.window.alert('Password must be at least 8 characters.')
      return
    }
    if (newPassword !== confirmPassword) {
      globalThis.window.alert('Passwords do not match.')
      return
    }
    await onSetUserPassword(user, newPassword)
    closePasswordForm()
  }

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
            <th className="py-1.5 px-2 text-left font-normal">ID</th>
            <th className="py-1.5 px-2 text-left font-normal">Email</th>
            <th className="py-1.5 px-2 text-left font-normal">Role</th>
            <th className="py-1.5 px-2 text-left font-normal">Status</th>
            <th className="py-1.5 px-2 text-left font-normal">TX</th>
            <th className="py-1.5 px-2 text-left font-normal">Dispatch</th>
            {passwordLoginEnabled && <th className="py-1.5 px-2 text-left font-normal">Password</th>}
            <th className="py-1.5 px-2 text-left font-normal">Created</th>
            <th className="py-1.5 px-2 text-left font-normal">Updated</th>
            <th className="py-1.5 px-2 text-left font-normal">Actions</th>
          </tr>
        </thead>
        <tbody>
          {users.map((user) => (
            <tr key={user.id} className={`border-b border-console-border/70 ${user.status === 'pending' ? 'bg-console-accent/5' : ''}`}>
              <td className="py-2 px-2 font-mono text-[10px] text-console-muted">{user.id}</td>
              <td className="py-2 px-2">{user.email}</td>
              <td className="py-2 px-2">
                <select
                  value={user.role}
                  onChange={(event) => setUsers(updateUserRoleDraft(user.id, event.target.value as UserRecord['role']))}
                  className="bg-console-bg border border-console-border rounded px-2 py-1 text-xs"
                >
                  <option value="admin">admin</option>
                  <option value="incident_handler">incident handler</option>
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
              <td className="py-2 px-2">
                <label className="flex items-center gap-1.5 text-[10px] text-console-muted cursor-pointer">
                  <input
                    type="checkbox"
                    checked={user.dispatcherEnabled}
                    onChange={(event) =>
                      setUsers(updateUserDispatcherEnabledDraft(user.id, event.target.checked))
                    }
                    className="accent-console-accent"
                  />
                  <span className="uppercase tracking-wider">{user.dispatcherEnabled ? 'On' : 'Off'}</span>
                </label>
              </td>
              {passwordLoginEnabled && (
                <td className="py-2 px-2">
                  <span className={`text-[10px] uppercase tracking-wider ${user.passwordConfigured ? 'text-console-accent' : 'text-console-muted'}`}>
                    {user.passwordConfigured ? 'Set' : 'None'}
                  </span>
                </td>
              )}
              <td className="py-2 px-2 text-console-muted">{fmtDateTime(user.createdAt)}</td>
              <td className="py-2 px-2 text-console-muted">{fmtDateTime(user.updatedAt)}</td>
              <td className="py-2 px-2">
                <div className="flex flex-col gap-2">
                <div className="flex items-center gap-2 flex-wrap">
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
                  {passwordLoginEnabled && (
                    <button
                      onClick={() => (passwordUserID === user.id ? closePasswordForm() : openPasswordForm(user.id))}
                      className="px-2 py-1 border border-console-border text-console-muted rounded text-[10px] hover:border-console-accent hover:text-console-accent"
                      disabled={userActionID === user.id}
                    >
                      {passwordUserID === user.id ? 'CANCEL PW' : user.passwordConfigured ? 'RESET PW' : 'SET PW'}
                    </button>
                  )}
                  <button
                    onClick={() => onRemoveUser(user)}
                    className="px-2 py-1 border border-console-error text-console-error rounded text-[10px] hover:bg-console-error hover:bg-opacity-10"
                    disabled={userActionID === user.id}
                  >
                    DELETE
                  </button>
                </div>
                {passwordLoginEnabled && passwordUserID === user.id && (
                  <div className="flex flex-col gap-2 rounded border border-console-border/70 bg-console-bg/40 p-2 max-w-xs">
                    <input
                      type="password"
                      value={newPassword}
                      onChange={(event) => setNewPassword(event.target.value)}
                      placeholder="New password (min 8 chars)"
                      autoComplete="new-password"
                      className="bg-console-bg border border-console-border rounded px-2 py-1 text-xs outline-none focus:border-console-accent"
                    />
                    <input
                      type="password"
                      value={confirmPassword}
                      onChange={(event) => setConfirmPassword(event.target.value)}
                      placeholder="Confirm password"
                      autoComplete="new-password"
                      className="bg-console-bg border border-console-border rounded px-2 py-1 text-xs outline-none focus:border-console-accent"
                    />
                    <button
                      onClick={() => void submitPassword(user)}
                      className="w-fit px-2 py-1 border border-console-accent text-console-accent rounded text-[10px] hover:bg-console-accent hover:bg-opacity-10"
                      disabled={userActionID === user.id}
                    >
                      {userActionID === user.id ? 'SAVING...' : 'SAVE PASSWORD'}
                    </button>
                  </div>
                )}
                </div>
              </td>
            </tr>
          ))}
          {users.length === 0 && (
            <tr>
              <td className="py-3 px-2 text-console-muted" colSpan={passwordLoginEnabled ? 10 : 9}>No users</td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  )
}

function AuditLogPanel({ auditLogs, auditLoading, onRefreshAuditLogs }: AuditLogPanelProps) {
  const [actionFilter, setActionFilter] = useState<string>('all')
  const [textFilter, setTextFilter] = useState('')

  const actionOptions = useMemo(() => {
    const unique = new Set<string>()
    for (const entry of auditLogs) unique.add(entry.action)
    return Array.from(unique).sort((a, b) => a.localeCompare(b))
  }, [auditLogs])

  const normalizedTextFilter = textFilter.trim().toLowerCase()
  const filteredAuditLogs = useMemo(() => {
    return auditLogs.filter((entry) => {
      if (actionFilter !== 'all' && entry.action !== actionFilter) return false
      if (!normalizedTextFilter) return true
      const actor = `${entry.userEmail || ''} ${entry.userId || ''}`.toLowerCase()
      const target = `${entry.targetEmail || ''} ${entry.targetId || ''} ${entry.targetType || ''}`.toLowerCase()
      return actor.includes(normalizedTextFilter) || target.includes(normalizedTextFilter)
    })
  }, [auditLogs, actionFilter, normalizedTextFilter])

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
      <div className="mb-3 grid gap-2 md:grid-cols-[180px_minmax(0,1fr)]">
        <select
          value={actionFilter}
          onChange={(event) => setActionFilter(event.target.value)}
          className="bg-console-bg border border-console-border rounded px-2 py-1 text-xs"
        >
          <option value="all">All actions</option>
          {actionOptions.map((action) => (
            <option key={action} value={action}>{action}</option>
          ))}
        </select>
        <input
          value={textFilter}
          onChange={(event) => setTextFilter(event.target.value)}
          placeholder="Filter by email or ID"
          className="bg-console-bg border border-console-border rounded px-2 py-1 text-xs outline-none focus:border-console-accent"
        />
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
          {filteredAuditLogs.map((entry) => (
            <tr key={entry.id} className="border-b border-console-border/70">
              <td className="py-2 px-2 text-console-muted">{fmtDateTime(entry.createdAt)}</td>
              <td className="py-2 px-2">{entry.action}</td>
              <td className="py-2 px-2 text-console-muted">
                {entry.targetType}: {formatAuditIdentity(entry.targetEmail, entry.targetId)}
              </td>
              <td className="py-2 px-2 text-console-muted">{formatAuditIdentity(entry.userEmail, entry.userId)}</td>
            </tr>
          ))}
          {filteredAuditLogs.length === 0 && (
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
  authCapabilities,
  authPassword,
  users,
  setUsers,
  usersLoading,
  userActionID,
  auditLogs,
  auditLoading,
  onAuthEmailChange,
  onAuthTokenChange,
  onAuthPasswordChange,
  onRequestMagicLink,
  onVerifyMagicLinkToken,
  onPasswordLogin,
  onLogoutSession,
  onRefreshUsers,
  onSaveUser,
  onApproveUser,
  onSetUserPassword,
  onRemoveUser,
  onRefreshAuditLogs,
  onOpenHub,
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
            authCapabilities={authCapabilities}
            authPassword={authPassword}
            onAuthEmailChange={onAuthEmailChange}
            onAuthTokenChange={onAuthTokenChange}
            onAuthPasswordChange={onAuthPasswordChange}
            onRequestMagicLink={onRequestMagicLink}
            onVerifyMagicLinkToken={onVerifyMagicLinkToken}
            onPasswordLogin={onPasswordLogin}
          />
        )}

        {authUser && authMessage && <div className="text-[11px] text-console-accent">{authMessage}</div>}
        {authUser && authError && <div className="text-[11px] text-console-error">{authError}</div>}

        {showAdminPanels && <AdminOperatorPanel onOpenHub={onOpenHub} />}

        {showAdminPanels && <CallStoragePanel />}

        {showAdminPanels && (
          <UserManagementPanel
            authCapabilities={authCapabilities}
            users={users}
            setUsers={setUsers}
            usersLoading={usersLoading}
            userActionID={userActionID}
            onRefreshUsers={onRefreshUsers}
            onSaveUser={onSaveUser}
            onApproveUser={onApproveUser}
            onSetUserPassword={onSetUserPassword}
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

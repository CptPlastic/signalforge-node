import type { UserRecord } from './api'

export function updateUserRoleDraft(userID: string, role: UserRecord['role']) {
  return (users: UserRecord[]): UserRecord[] => users.map((user) => user.id === userID ? { ...user, role } : user)
}

export function updateUserStatusDraft(userID: string, status: UserRecord['status']) {
  return (users: UserRecord[]): UserRecord[] => users.map((user) => user.id === userID ? { ...user, status } : user)
}

export function updateUserTxEnabledDraft(userID: string, txEnabled: boolean) {
  return (users: UserRecord[]): UserRecord[] => users.map((user) => user.id === userID ? { ...user, txEnabled } : user)
}

export function updateUserDispatcherEnabledDraft(userID: string, dispatcherEnabled: boolean) {
  return (users: UserRecord[]): UserRecord[] =>
    users.map((user) => user.id === userID ? { ...user, dispatcherEnabled } : user)
}
import { createContext, useContext } from 'react'
import type { AuthMeResponse, Workspace } from './openapi'

export type AdminContextValue = {
  workspaceID: string
  workspaces: Workspace[]
  activeWorkspace: Workspace | null
  auth: AuthMeResponse
  selectWorkspace: (workspaceID: string) => Promise<void>
}

export const AdminContext = createContext<AdminContextValue | null>(null)

const adminWorkspaceCookie = 'admin_workspace_id'
export const allWorkspacesValue = '__all__'

export function useAdmin (): AdminContextValue {
  const ctx = useContext(AdminContext)
  if (!ctx) throw new Error('useAdmin must be used within AdminLayout')
  return ctx
}

export function getPreferredAdminWorkspaceID () {
  if (typeof document === 'undefined') {
    return ''
  }

  const cookie = document.cookie
    .split('; ')
    .find(part => part.startsWith(`${adminWorkspaceCookie}=`))

  return cookie ? decodeURIComponent(cookie.split('=').slice(1).join('=')) : ''
}

export function setPreferredAdminWorkspaceID (workspaceID: string) {
  if (typeof document === 'undefined') {
    return
  }

  document.cookie = `${adminWorkspaceCookie}=${encodeURIComponent(
    workspaceID || allWorkspacesValue
  )}; Path=/; Max-Age=31536000; SameSite=Lax`
}

export function formatDate (value: string) {
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: 'medium',
    timeStyle: 'short'
  }).format(new Date(value))
}

export function getErrorMessage (error: unknown, fallback: string) {
  if (error instanceof Error) return error.message
  return fallback
}

export function formatNumber (value: number) {
  return new Intl.NumberFormat().format(value)
}

export function formatPercent (value: number) {
  return `${Math.round(value * 100)}%`
}

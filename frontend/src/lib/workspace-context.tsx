import { createContext, useContext } from 'react'
import type {
  AuthMeResponse,
  Conversation,
  User,
  Workspace,
  WorkspaceMember,
} from './openapi'

export type WorkspaceAppContextValue = {
  auth: AuthMeResponse | null
  authPending: boolean
  workspaces: Workspace[]
  workspacesPending: boolean
  preferredWorkspaceID: string
  selectWorkspace: (workspaceID: string) => Promise<void>
  logout: () => Promise<void>
  isSigningOut: boolean
}

type ConversationRouteContextValue = {
  conversations: Conversation[]
  conversationsPending: boolean
  conversationsError: string
  memberUsersById: Map<string, User>
  selectedConversationId: string
  selectedConversation: Conversation | null
}

export type WorkspaceRouteContextValue = ConversationRouteContextValue & {
  workspace: Workspace
  members: WorkspaceMember[]
  membersPending: boolean
  membersError: string
}

export type MeRouteContextValue = ConversationRouteContextValue

export const WorkspaceAppContext =
  createContext<WorkspaceAppContextValue | null>(null)

export const WorkspaceRouteContext =
  createContext<WorkspaceRouteContextValue | null>(null)

export const MeRouteContext =
  createContext<MeRouteContextValue | null>(null)

const workspacePreferenceCookie = 'workspace_id'

export function useWorkspaceApp() {
  const context = useContext(WorkspaceAppContext)
  if (!context) {
    throw new Error('useWorkspaceApp must be used within a workspace route')
  }

  return context
}

export function useReadyWorkspaceApp() {
  const context = useWorkspaceApp()
  if (context.auth == null) {
    throw new Error('useReadyWorkspaceApp requires an active workspace session')
  }

  return {
    ...context,
    auth: context.auth,
  }
}

export function useWorkspaceRoute() {
  const context = useContext(WorkspaceRouteContext)
  if (!context) {
    throw new Error('useWorkspaceRoute must be used within a workspace shell')
  }

  return context
}

export function useMeRoute() {
  const context = useContext(MeRouteContext)
  if (!context) {
    throw new Error('useMeRoute must be used within the @me workspace shell')
  }

  return context
}

export function getPreferredWorkspaceID() {
  if (typeof document === 'undefined') {
    return ''
  }

  const cookie = document.cookie
    .split('; ')
    .find((part) => part.startsWith(`${workspacePreferenceCookie}=`))

  return cookie ? decodeURIComponent(cookie.split('=').slice(1).join('=')) : ''
}

export function setPreferredWorkspaceID(workspaceID: string) {
  if (typeof document === 'undefined') {
    return
  }

  document.cookie = `${workspacePreferenceCookie}=${encodeURIComponent(
    workspaceID,
  )}; Path=/; Max-Age=31536000; SameSite=Lax`
}

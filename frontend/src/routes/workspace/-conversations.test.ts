import { describe, expect, it } from 'vitest'
import {
  getExternalMemberLabel,
  getExternalWorkspaceLabel,
} from './conversations'

describe('conversation external-member helpers', () => {
  it('prefers the richest external member label', () => {
    expect(
      getExternalMemberLabel({
        account_id: 'A123',
        account: {
          display_name: 'Display Name',
          real_name: 'Real Name',
          name: 'login',
          email: 'user@example.com',
        },
      } as never),
    ).toBe('Display Name')

    expect(
      getExternalMemberLabel({
        account_id: 'A123',
        account: {
          display_name: '',
          real_name: '',
          name: '',
          email: 'user@example.com',
        },
      } as never),
    ).toBe('user@example.com')

    expect(
      getExternalMemberLabel({
        account_id: 'A123',
      } as never),
    ).toBe('A123')
  })

  it('resolves connected external workspace names before falling back to ids', () => {
    const workspaces = [
      {
        external_workspace_id: 'T_EXT',
        name: 'Partner Workspace',
      },
    ]

    expect(
      getExternalWorkspaceLabel(workspaces as never, 'T_EXT'),
    ).toBe('Partner Workspace')
    expect(
      getExternalWorkspaceLabel(workspaces as never, 'T_UNKNOWN'),
    ).toBe('T_UNKNOWN')
  })
})

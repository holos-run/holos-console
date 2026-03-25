import { describe, it, expect } from 'vitest'
import { isOwner } from './isOwner'
import { Role } from '@/gen/holos/console/v1/rbac_pb'

describe('isOwner', () => {
  describe('user grants', () => {
    it('returns true when email matches a user grant with owner role', () => {
      expect(
        isOwner(
          'alice@example.com',
          [],
          [{ principal: 'alice@example.com', role: Role.OWNER }],
          [],
        ),
      ).toBe(true)
    })

    it('returns false when email matches a user grant with non-owner role', () => {
      expect(
        isOwner(
          'alice@example.com',
          [],
          [{ principal: 'alice@example.com', role: Role.EDITOR }],
          [],
        ),
      ).toBe(false)
    })

    it('returns false when email does not match any user grant', () => {
      expect(
        isOwner(
          'bob@example.com',
          [],
          [{ principal: 'alice@example.com', role: Role.OWNER }],
          [],
        ),
      ).toBe(false)
    })

    it('returns false when email is undefined', () => {
      expect(
        isOwner(
          undefined,
          [],
          [{ principal: 'alice@example.com', role: Role.OWNER }],
          [],
        ),
      ).toBe(false)
    })
  })

  describe('role grants (the bug: groups with owner permission)', () => {
    it('returns true when user group matches a role grant with owner role', () => {
      expect(
        isOwner(
          'alice@example.com',
          ['platform-owner'],
          [],
          [{ principal: 'platform-owner', role: Role.OWNER }],
        ),
      ).toBe(true)
    })

    it('returns false when user group matches a role grant with non-owner role', () => {
      expect(
        isOwner(
          'alice@example.com',
          ['platform-owner'],
          [],
          [{ principal: 'platform-owner', role: Role.EDITOR }],
        ),
      ).toBe(false)
    })

    it('returns false when user has no groups matching any role grant', () => {
      expect(
        isOwner(
          'alice@example.com',
          ['dev-team'],
          [],
          [{ principal: 'platform-owner', role: Role.OWNER }],
        ),
      ).toBe(false)
    })

    it('returns true when one of multiple user groups matches owner role grant', () => {
      expect(
        isOwner(
          'alice@example.com',
          ['dev-team', 'platform-owner'],
          [],
          [{ principal: 'platform-owner', role: Role.OWNER }],
        ),
      ).toBe(true)
    })

    it('returns false when groups array is empty', () => {
      expect(
        isOwner(
          'alice@example.com',
          [],
          [],
          [{ principal: 'platform-owner', role: Role.OWNER }],
        ),
      ).toBe(false)
    })
  })

  describe('combined user and role grants', () => {
    it('returns true when email matches owner user grant even without matching role grant', () => {
      expect(
        isOwner(
          'alice@example.com',
          ['dev-team'],
          [{ principal: 'alice@example.com', role: Role.OWNER }],
          [{ principal: 'platform-owner', role: Role.OWNER }],
        ),
      ).toBe(true)
    })

    it('returns true when group matches owner role grant even without matching user grant', () => {
      expect(
        isOwner(
          'alice@example.com',
          ['platform-owner'],
          [{ principal: 'bob@example.com', role: Role.OWNER }],
          [{ principal: 'platform-owner', role: Role.OWNER }],
        ),
      ).toBe(true)
    })

    it('returns false when no email or group match owner role', () => {
      expect(
        isOwner(
          'alice@example.com',
          ['dev-team'],
          [{ principal: 'bob@example.com', role: Role.OWNER }],
          [{ principal: 'platform-owner', role: Role.OWNER }],
        ),
      ).toBe(false)
    })
  })
})

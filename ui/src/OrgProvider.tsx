import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { useAuth } from './auth'
import { organizationsClient } from './client'
import type { Organization } from './gen/holos/console/v1/organizations_pb'

const SESSION_STORAGE_KEY = 'holos-selected-org'

export interface OrgContextValue {
  organizations: Organization[]
  selectedOrg: string | null
  setSelectedOrg: (name: string | null) => void
  isLoading: boolean
}

export const OrgContext = createContext<OrgContextValue | null>(null)

export function useOrg(): OrgContextValue {
  const ctx = useContext(OrgContext)
  if (!ctx) {
    throw new Error('useOrg must be used within an OrgProvider')
  }
  return ctx
}

interface OrgProviderProps {
  children: ReactNode
}

export function OrgProvider({ children }: OrgProviderProps) {
  const { isAuthenticated, getAccessToken } = useAuth()
  const [organizations, setOrganizations] = useState<Organization[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [selectedOrg, setSelectedOrgState] = useState<string | null>(() => {
    return sessionStorage.getItem(SESSION_STORAGE_KEY)
  })

  const setSelectedOrg = useCallback((name: string | null) => {
    setSelectedOrgState(name)
    if (name) {
      sessionStorage.setItem(SESSION_STORAGE_KEY, name)
    } else {
      sessionStorage.removeItem(SESSION_STORAGE_KEY)
    }
  }, [])

  useEffect(() => {
    if (!isAuthenticated) {
      setIsLoading(false)
      return
    }

    let cancelled = false

    const fetchOrgs = async () => {
      setIsLoading(true)
      try {
        const token = getAccessToken()
        const response = await organizationsClient.listOrganizations(
          {},
          {
            headers: {
              Authorization: `Bearer ${token}`,
            },
          },
        )
        if (!cancelled) {
          setOrganizations(response.organizations)
        }
      } catch {
        // Silently fail - orgs will be empty
      } finally {
        if (!cancelled) {
          setIsLoading(false)
        }
      }
    }

    fetchOrgs()
    return () => {
      cancelled = true
    }
  }, [isAuthenticated, getAccessToken])

  const value = useMemo<OrgContextValue>(
    () => ({
      organizations,
      selectedOrg,
      setSelectedOrg,
      isLoading,
    }),
    [organizations, selectedOrg, setSelectedOrg, isLoading],
  )

  return <OrgContext.Provider value={value}>{children}</OrgContext.Provider>
}

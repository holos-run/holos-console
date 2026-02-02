import {
  createContext,
  useCallback,
  useContext,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { useListOrganizations } from './queries/organizations'
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
  const { data, isLoading } = useListOrganizations()
  const organizations = data?.organizations ?? []

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

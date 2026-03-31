import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'
import { useListProjects } from '@/queries/projects'
import type { Project } from '@/gen/holos/console/v1/projects_pb'
import { useOrg } from '@/lib/org-context'

const STORAGE_KEY = 'holos-selected-project'

export interface ProjectContextValue {
  projects: Project[]
  selectedProject: string | null
  setSelectedProject: (name: string | null) => void
  isLoading: boolean
}

export const ProjectContext = createContext<ProjectContextValue | null>(null)

export function useProject(): ProjectContextValue {
  const ctx = useContext(ProjectContext)
  if (!ctx) {
    throw new Error('useProject must be used within a ProjectProvider')
  }
  return ctx
}

export function ProjectProvider({ children }: { children: ReactNode }) {
  const { selectedOrg } = useOrg()
  const { data, isLoading } = useListProjects(selectedOrg ?? '')
  const projects = data?.projects ?? []

  const [selectedProject, setSelectedProjectState] = useState<string | null>(() => {
    return localStorage.getItem(STORAGE_KEY)
  })

  // Clear selected project when org changes, but not on initial mount.
  const isMounted = useRef(false)
  useEffect(() => {
    if (!isMounted.current) {
      isMounted.current = true
      return
    }
    setSelectedProjectState(null)
    localStorage.removeItem(STORAGE_KEY)
  }, [selectedOrg])

  const setSelectedProject = useCallback((name: string | null) => {
    setSelectedProjectState(name)
    if (name) {
      localStorage.setItem(STORAGE_KEY, name)
    } else {
      localStorage.removeItem(STORAGE_KEY)
    }
  }, [])

  const value = useMemo<ProjectContextValue>(
    () => ({
      projects,
      selectedProject,
      setSelectedProject,
      isLoading,
    }),
    [projects, selectedProject, setSelectedProject, isLoading],
  )

  return <ProjectContext.Provider value={value}>{children}</ProjectContext.Provider>
}

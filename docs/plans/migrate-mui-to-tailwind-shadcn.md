# Plan: Migrate from Material UI to Tailwind CSS + shadcn/ui

> **Status:** UNREVIEWED / UNAPPROVED
>
> This plan requires review and approval before implementation.

## Overview

This plan migrates the Holos Console frontend from Material UI (MUI v7) to Tailwind CSS with shadcn/ui components, matching the technology stack used by Claude.ai.

### Research Findings

According to [Anthropic's engineering documentation](https://www.anthropic.com/engineering) and the [Claude blog on frontend design](https://claude.com/blog/improving-frontend-design-through-skills), the Claude.ai interface is built with:
- **React** - UI framework
- **Next.js** - React framework (we'll continue with Vite for simplicity)
- **Tailwind CSS** - Utility-first CSS framework
- **shadcn/ui** - Component library built on Radix UI primitives

### Goals

1. Replace Material UI with Tailwind CSS + shadcn/ui
2. Implement Claude-style navigation sidebar with:
   - Organization selector at the top
   - Projects hierarchy for navigation
   - Profile avatar with user initials at the bottom left
3. Maintain existing functionality (OIDC auth, RPC, routing)
4. Preserve development workflow and testing infrastructure

## Current State

### Dependencies to Remove
```json
{
  "@mui/material": "^7.3.6",
  "@mui/icons-material": "^7.3.6",
  "@emotion/react": "^11.14.0",
  "@emotion/styled": "^11.14.0"
}
```

### Dependencies to Add
```json
{
  "tailwindcss": "^4.0",
  "postcss": "^8.5",
  "autoprefixer": "^10.4",
  "clsx": "^2.1",
  "tailwind-merge": "^2.5",
  "class-variance-authority": "^0.7",
  "@radix-ui/react-avatar": "^1.1",
  "@radix-ui/react-dropdown-menu": "^2.1",
  "@radix-ui/react-scroll-area": "^1.2",
  "@radix-ui/react-separator": "^1.1",
  "@radix-ui/react-tooltip": "^1.1",
  "@radix-ui/react-collapsible": "^1.1",
  "@radix-ui/react-slot": "^1.1",
  "lucide-react": "^0.469"
}
```

### Files to Modify
- `ui/package.json` - Update dependencies
- `ui/vite.config.ts` - Add PostCSS/Tailwind plugin
- `ui/src/App.tsx` - Replace MUI layout with Tailwind + shadcn components
- `ui/src/main.tsx` - Remove MUI imports
- `ui/src/components/*.tsx` - Migrate all components

### Files to Add
- `ui/tailwind.config.ts` - Tailwind configuration
- `ui/postcss.config.js` - PostCSS configuration
- `ui/src/index.css` - Tailwind base styles
- `ui/src/lib/utils.ts` - cn() utility function
- `ui/src/components/ui/*.tsx` - shadcn/ui components

## Design: Claude-Style Sidebar

### Sidebar Structure

```
┌─────────────────────────────────────────────────────────────┐
│ ┌─────────────────────────┐                                 │
│ │ Organization Selector ▼ │   New Chat Button               │
│ └─────────────────────────┘                                 │
├─────────────────────────────────────────────────────────────┤
│                                                             │
│  Today                                                      │
│    ○ Chat title 1                                          │
│    ○ Chat title 2                                          │
│                                                             │
│  Yesterday                                                  │
│    ○ Chat title 3                                          │
│                                                             │
│  ▸ Project: Infrastructure                                  │
│    ○ Chat in project                                       │
│                                                             │
│  ▸ Project: Platform                                        │
│                                                             │
├─────────────────────────────────────────────────────────────┤
│ ┌────┐                                                      │
│ │ JD │  Jeff Doe                                           │
│ └────┘  jeff@example.com                                    │
└─────────────────────────────────────────────────────────────┘
```

### Component Hierarchy

```
<AppLayout>
  <Sidebar>
    <SidebarHeader>
      <OrganizationSelector />
      <NewChatButton />
    </SidebarHeader>
    <SidebarContent>
      <ScrollArea>
        <NavigationSection title="Today">
          <NavigationItem />
        </NavigationSection>
        <NavigationSection title="Projects">
          <ProjectGroup>
            <NavigationItem />
          </ProjectGroup>
        </NavigationSection>
      </ScrollArea>
    </SidebarContent>
    <SidebarFooter>
      <UserProfile />
    </SidebarFooter>
  </Sidebar>
  <MainContent>
    <Routes />
  </MainContent>
</AppLayout>
```

## Phase 1: Tailwind CSS Setup

### 1.1 Install Tailwind CSS and PostCSS

Install core dependencies:

```bash
cd ui
npm install -D tailwindcss postcss autoprefixer
npm install clsx tailwind-merge class-variance-authority
npx tailwindcss init -p --ts
```

### 1.2 Configure Tailwind

Create `ui/tailwind.config.ts`:

```typescript
import type { Config } from 'tailwindcss'

const config: Config = {
  darkMode: ['class'],
  content: ['./index.html', './src/**/*.{js,ts,jsx,tsx}'],
  theme: {
    extend: {
      colors: {
        border: 'hsl(var(--border))',
        input: 'hsl(var(--input))',
        ring: 'hsl(var(--ring))',
        background: 'hsl(var(--background))',
        foreground: 'hsl(var(--foreground))',
        primary: {
          DEFAULT: 'hsl(var(--primary))',
          foreground: 'hsl(var(--primary-foreground))',
        },
        secondary: {
          DEFAULT: 'hsl(var(--secondary))',
          foreground: 'hsl(var(--secondary-foreground))',
        },
        muted: {
          DEFAULT: 'hsl(var(--muted))',
          foreground: 'hsl(var(--muted-foreground))',
        },
        accent: {
          DEFAULT: 'hsl(var(--accent))',
          foreground: 'hsl(var(--accent-foreground))',
        },
        destructive: {
          DEFAULT: 'hsl(var(--destructive))',
          foreground: 'hsl(var(--destructive-foreground))',
        },
        card: {
          DEFAULT: 'hsl(var(--card))',
          foreground: 'hsl(var(--card-foreground))',
        },
        sidebar: {
          DEFAULT: 'hsl(var(--sidebar-background))',
          foreground: 'hsl(var(--sidebar-foreground))',
          primary: 'hsl(var(--sidebar-primary))',
          'primary-foreground': 'hsl(var(--sidebar-primary-foreground))',
          accent: 'hsl(var(--sidebar-accent))',
          'accent-foreground': 'hsl(var(--sidebar-accent-foreground))',
          border: 'hsl(var(--sidebar-border))',
          ring: 'hsl(var(--sidebar-ring))',
        },
      },
      borderRadius: {
        lg: 'var(--radius)',
        md: 'calc(var(--radius) - 2px)',
        sm: 'calc(var(--radius) - 4px)',
      },
    },
  },
  plugins: [],
}

export default config
```

### 1.3 Create CSS Variables

Create `ui/src/index.css`:

```css
@tailwind base;
@tailwind components;
@tailwind utilities;

@layer base {
  :root {
    --background: 0 0% 100%;
    --foreground: 222.2 84% 4.9%;
    --card: 0 0% 100%;
    --card-foreground: 222.2 84% 4.9%;
    --primary: 222.2 47.4% 11.2%;
    --primary-foreground: 210 40% 98%;
    --secondary: 210 40% 96.1%;
    --secondary-foreground: 222.2 47.4% 11.2%;
    --muted: 210 40% 96.1%;
    --muted-foreground: 215.4 16.3% 46.9%;
    --accent: 210 40% 96.1%;
    --accent-foreground: 222.2 47.4% 11.2%;
    --destructive: 0 84.2% 60.2%;
    --destructive-foreground: 210 40% 98%;
    --border: 214.3 31.8% 91.4%;
    --input: 214.3 31.8% 91.4%;
    --ring: 222.2 84% 4.9%;
    --radius: 0.5rem;

    /* Sidebar specific */
    --sidebar-background: 0 0% 98%;
    --sidebar-foreground: 240 5.3% 26.1%;
    --sidebar-primary: 240 5.9% 10%;
    --sidebar-primary-foreground: 0 0% 98%;
    --sidebar-accent: 240 4.8% 95.9%;
    --sidebar-accent-foreground: 240 5.9% 10%;
    --sidebar-border: 220 13% 91%;
    --sidebar-ring: 217.2 91.2% 59.8%;
  }

  .dark {
    --background: 222.2 84% 4.9%;
    --foreground: 210 40% 98%;
    --card: 222.2 84% 4.9%;
    --card-foreground: 210 40% 98%;
    --primary: 210 40% 98%;
    --primary-foreground: 222.2 47.4% 11.2%;
    --secondary: 217.2 32.6% 17.5%;
    --secondary-foreground: 210 40% 98%;
    --muted: 217.2 32.6% 17.5%;
    --muted-foreground: 215 20.2% 65.1%;
    --accent: 217.2 32.6% 17.5%;
    --accent-foreground: 210 40% 98%;
    --destructive: 0 62.8% 30.6%;
    --destructive-foreground: 210 40% 98%;
    --border: 217.2 32.6% 17.5%;
    --input: 217.2 32.6% 17.5%;
    --ring: 212.7 26.8% 83.9%;

    /* Sidebar specific (dark) */
    --sidebar-background: 240 5.9% 10%;
    --sidebar-foreground: 240 4.8% 95.9%;
    --sidebar-primary: 224.3 76.3% 48%;
    --sidebar-primary-foreground: 0 0% 100%;
    --sidebar-accent: 240 3.7% 15.9%;
    --sidebar-accent-foreground: 240 4.8% 95.9%;
    --sidebar-border: 240 3.7% 15.9%;
    --sidebar-ring: 217.2 91.2% 59.8%;
  }
}

@layer base {
  * {
    @apply border-border;
  }
  body {
    @apply bg-background text-foreground;
  }
}
```

### 1.4 Create Utility Functions

Create `ui/src/lib/utils.ts`:

```typescript
import { type ClassValue, clsx } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
```

## Phase 2: shadcn/ui Components

### 2.1 Install Radix UI Primitives

```bash
cd ui
npm install @radix-ui/react-avatar @radix-ui/react-dropdown-menu \
  @radix-ui/react-scroll-area @radix-ui/react-separator \
  @radix-ui/react-tooltip @radix-ui/react-collapsible \
  @radix-ui/react-slot lucide-react
```

### 2.2 Create Button Component

Create `ui/src/components/ui/button.tsx`:

```typescript
import * as React from 'react'
import { Slot } from '@radix-ui/react-slot'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '@/lib/utils'

const buttonVariants = cva(
  'inline-flex items-center justify-center whitespace-nowrap rounded-md text-sm font-medium ring-offset-background transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50',
  {
    variants: {
      variant: {
        default: 'bg-primary text-primary-foreground hover:bg-primary/90',
        destructive: 'bg-destructive text-destructive-foreground hover:bg-destructive/90',
        outline: 'border border-input bg-background hover:bg-accent hover:text-accent-foreground',
        secondary: 'bg-secondary text-secondary-foreground hover:bg-secondary/80',
        ghost: 'hover:bg-accent hover:text-accent-foreground',
        link: 'text-primary underline-offset-4 hover:underline',
      },
      size: {
        default: 'h-10 px-4 py-2',
        sm: 'h-9 rounded-md px-3',
        lg: 'h-11 rounded-md px-8',
        icon: 'h-10 w-10',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'default',
    },
  }
)

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean
}

const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, ...props }, ref) => {
    const Comp = asChild ? Slot : 'button'
    return (
      <Comp
        className={cn(buttonVariants({ variant, size, className }))}
        ref={ref}
        {...props}
      />
    )
  }
)
Button.displayName = 'Button'

export { Button, buttonVariants }
```

### 2.3 Create Avatar Component

Create `ui/src/components/ui/avatar.tsx`:

```typescript
import * as React from 'react'
import * as AvatarPrimitive from '@radix-ui/react-avatar'
import { cn } from '@/lib/utils'

const Avatar = React.forwardRef<
  React.ElementRef<typeof AvatarPrimitive.Root>,
  React.ComponentPropsWithoutRef<typeof AvatarPrimitive.Root>
>(({ className, ...props }, ref) => (
  <AvatarPrimitive.Root
    ref={ref}
    className={cn(
      'relative flex h-10 w-10 shrink-0 overflow-hidden rounded-full',
      className
    )}
    {...props}
  />
))
Avatar.displayName = AvatarPrimitive.Root.displayName

const AvatarImage = React.forwardRef<
  React.ElementRef<typeof AvatarPrimitive.Image>,
  React.ComponentPropsWithoutRef<typeof AvatarPrimitive.Image>
>(({ className, ...props }, ref) => (
  <AvatarPrimitive.Image
    ref={ref}
    className={cn('aspect-square h-full w-full', className)}
    {...props}
  />
))
AvatarImage.displayName = AvatarPrimitive.Image.displayName

const AvatarFallback = React.forwardRef<
  React.ElementRef<typeof AvatarPrimitive.Fallback>,
  React.ComponentPropsWithoutRef<typeof AvatarPrimitive.Fallback>
>(({ className, ...props }, ref) => (
  <AvatarPrimitive.Fallback
    ref={ref}
    className={cn(
      'flex h-full w-full items-center justify-center rounded-full bg-muted',
      className
    )}
    {...props}
  />
))
AvatarFallback.displayName = AvatarPrimitive.Fallback.displayName

export { Avatar, AvatarImage, AvatarFallback }
```

### 2.4 Create Additional shadcn Components

Create the following components following shadcn/ui patterns:
- [ ] 2.4a: `ui/src/components/ui/card.tsx` - Card component
- [ ] 2.4b: `ui/src/components/ui/scroll-area.tsx` - Scroll area component
- [ ] 2.4c: `ui/src/components/ui/separator.tsx` - Separator component
- [ ] 2.4d: `ui/src/components/ui/dropdown-menu.tsx` - Dropdown menu component
- [ ] 2.4e: `ui/src/components/ui/tooltip.tsx` - Tooltip component
- [ ] 2.4f: `ui/src/components/ui/collapsible.tsx` - Collapsible component

## Phase 3: Sidebar Implementation

### 3.1 Create Sidebar Components

Create `ui/src/components/sidebar/Sidebar.tsx`:

```typescript
import * as React from 'react'
import { cn } from '@/lib/utils'

interface SidebarProps extends React.HTMLAttributes<HTMLDivElement> {
  children: React.ReactNode
}

export function Sidebar({ className, children, ...props }: SidebarProps) {
  return (
    <aside
      className={cn(
        'flex h-screen w-64 flex-col border-r border-sidebar-border bg-sidebar',
        className
      )}
      {...props}
    >
      {children}
    </aside>
  )
}

export function SidebarHeader({
  className,
  children,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn('flex flex-col gap-2 p-4', className)}
      {...props}
    >
      {children}
    </div>
  )
}

export function SidebarContent({
  className,
  children,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div className={cn('flex-1 overflow-auto', className)} {...props}>
      {children}
    </div>
  )
}

export function SidebarFooter({
  className,
  children,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn('border-t border-sidebar-border p-4', className)}
      {...props}
    >
      {children}
    </div>
  )
}
```

### 3.2 Create Organization Selector

Create `ui/src/components/sidebar/OrganizationSelector.tsx`:

```typescript
import * as React from 'react'
import { ChevronDown, Building2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

interface Organization {
  id: string
  name: string
}

interface OrganizationSelectorProps {
  organizations: Organization[]
  selectedOrg: Organization
  onSelect: (org: Organization) => void
}

export function OrganizationSelector({
  organizations,
  selectedOrg,
  onSelect,
}: OrganizationSelectorProps) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button
          variant="ghost"
          className="w-full justify-between px-2 hover:bg-sidebar-accent"
        >
          <div className="flex items-center gap-2">
            <Building2 className="h-4 w-4" />
            <span className="truncate">{selectedOrg.name}</span>
          </div>
          <ChevronDown className="h-4 w-4 opacity-50" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-56">
        <DropdownMenuLabel>Organizations</DropdownMenuLabel>
        <DropdownMenuSeparator />
        {organizations.map((org) => (
          <DropdownMenuItem
            key={org.id}
            onClick={() => onSelect(org)}
            className={org.id === selectedOrg.id ? 'bg-accent' : ''}
          >
            <Building2 className="mr-2 h-4 w-4" />
            {org.name}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
```

### 3.3 Create User Profile Component

Create `ui/src/components/sidebar/UserProfile.tsx`:

```typescript
import * as React from 'react'
import { LogOut, Settings, User } from 'lucide-react'
import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { useAuth } from '@/auth'

function getInitials(name: string | undefined, email: string | undefined): string {
  if (name) {
    const parts = name.split(' ')
    if (parts.length >= 2) {
      return `${parts[0][0]}${parts[parts.length - 1][0]}`.toUpperCase()
    }
    return name.slice(0, 2).toUpperCase()
  }
  if (email) {
    return email.slice(0, 2).toUpperCase()
  }
  return 'U'
}

export function UserProfile() {
  const { user, logout } = useAuth()

  const name = user?.profile?.name
  const email = user?.profile?.email
  const initials = getInitials(name, email)

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button className="flex w-full items-center gap-3 rounded-md p-2 hover:bg-sidebar-accent">
          <Avatar className="h-8 w-8">
            <AvatarImage src={user?.profile?.picture} />
            <AvatarFallback className="bg-primary text-primary-foreground text-sm">
              {initials}
            </AvatarFallback>
          </Avatar>
          <div className="flex flex-1 flex-col items-start text-left">
            <span className="text-sm font-medium text-sidebar-foreground">
              {name || 'User'}
            </span>
            <span className="text-xs text-muted-foreground truncate max-w-[140px]">
              {email}
            </span>
          </div>
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-56">
        <DropdownMenuLabel>My Account</DropdownMenuLabel>
        <DropdownMenuSeparator />
        <DropdownMenuItem>
          <User className="mr-2 h-4 w-4" />
          Profile
        </DropdownMenuItem>
        <DropdownMenuItem>
          <Settings className="mr-2 h-4 w-4" />
          Settings
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={() => logout()}>
          <LogOut className="mr-2 h-4 w-4" />
          Sign out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
```

### 3.4 Create Project Navigation

Create `ui/src/components/sidebar/ProjectNavigation.tsx`:

```typescript
import * as React from 'react'
import { ChevronRight, Folder, FolderOpen, FileText } from 'lucide-react'
import { Link, useLocation } from 'react-router-dom'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { cn } from '@/lib/utils'

interface NavItem {
  id: string
  title: string
  href: string
}

interface Project {
  id: string
  name: string
  items: NavItem[]
}

interface ProjectNavigationProps {
  projects: Project[]
}

export function ProjectNavigation({ projects }: ProjectNavigationProps) {
  const location = useLocation()
  const [openProjects, setOpenProjects] = React.useState<string[]>([])

  const toggleProject = (projectId: string) => {
    setOpenProjects((prev) =>
      prev.includes(projectId)
        ? prev.filter((id) => id !== projectId)
        : [...prev, projectId]
    )
  }

  return (
    <div className="space-y-1 px-2">
      <div className="px-2 py-1.5 text-xs font-semibold text-muted-foreground uppercase tracking-wider">
        Projects
      </div>
      {projects.map((project) => {
        const isOpen = openProjects.includes(project.id)
        return (
          <Collapsible
            key={project.id}
            open={isOpen}
            onOpenChange={() => toggleProject(project.id)}
          >
            <CollapsibleTrigger asChild>
              <button className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm hover:bg-sidebar-accent">
                <ChevronRight
                  className={cn(
                    'h-4 w-4 transition-transform',
                    isOpen && 'rotate-90'
                  )}
                />
                {isOpen ? (
                  <FolderOpen className="h-4 w-4" />
                ) : (
                  <Folder className="h-4 w-4" />
                )}
                <span className="truncate">{project.name}</span>
              </button>
            </CollapsibleTrigger>
            <CollapsibleContent className="pl-8 space-y-0.5">
              {project.items.map((item) => {
                const isActive = location.pathname === item.href
                return (
                  <Link
                    key={item.id}
                    to={item.href}
                    className={cn(
                      'flex items-center gap-2 rounded-md px-2 py-1.5 text-sm',
                      isActive
                        ? 'bg-sidebar-accent text-sidebar-accent-foreground'
                        : 'hover:bg-sidebar-accent/50'
                    )}
                  >
                    <FileText className="h-4 w-4" />
                    <span className="truncate">{item.title}</span>
                  </Link>
                )
              })}
            </CollapsibleContent>
          </Collapsible>
        )
      })}
    </div>
  )
}
```

## Phase 4: Migrate Existing Components

### 4.1 Migrate VersionCard

Update `ui/src/components/VersionCard.tsx`:
- Replace MUI `Card`, `CardContent`, `Typography` with Tailwind classes
- Use shadcn Card component

### 4.2 Migrate ProfilePage

Update `ui/src/components/ProfilePage.tsx`:
- Replace MUI `Accordion`, `Alert`, `Button` with Tailwind/shadcn equivalents
- Maintain OIDC claims display functionality

### 4.3 Update App Layout

Update `ui/src/App.tsx`:
- Remove all MUI imports
- Implement new `<AppLayout>` with sidebar
- Wire up organization selector, projects, and user profile
- Maintain existing routing structure

### 4.4 Update Entry Point

Update `ui/src/main.tsx`:
- Remove MUI ThemeProvider
- Import Tailwind CSS (`import './index.css'`)
- Add dark mode class toggle support

## Phase 5: Configure Build System

### 5.1 Update Vite Configuration

Update `ui/vite.config.ts`:
- Add path alias for `@/` imports
- Ensure CSS is properly processed

### 5.2 Update TypeScript Configuration

Update `ui/tsconfig.json`:
- Add path alias mapping for `@/`

```json
{
  "compilerOptions": {
    "baseUrl": ".",
    "paths": {
      "@/*": ["./src/*"]
    }
  }
}
```

## Phase 6: Testing and Cleanup

### 6.1 Update Component Tests

Update tests in `ui/src/components/`:
- Remove MUI testing utilities
- Update component selectors for new structure

### 6.2 Update E2E Tests

Review and update Playwright tests:
- Update selectors for new sidebar structure
- Add tests for organization selector
- Add tests for project navigation
- Add tests for user profile dropdown

### 6.3 Remove MUI Dependencies

After all components are migrated:
```bash
cd ui
npm uninstall @mui/material @mui/icons-material @emotion/react @emotion/styled
```

### 6.4 Final Verification

- [ ] 6.4a: Verify all pages render correctly
- [ ] 6.4b: Verify OIDC authentication flow works
- [ ] 6.4c: Verify RPC calls work (version, etc.)
- [ ] 6.4d: Verify responsive behavior
- [ ] 6.4e: Run full E2E test suite

## TODO (Implementation Checklist)

### Phase 1: Tailwind CSS Setup
- [ ] 1.1: Install Tailwind CSS and PostCSS dependencies
- [ ] 1.2: Create `ui/tailwind.config.ts` with theme configuration
- [ ] 1.3: Create `ui/src/index.css` with CSS variables and Tailwind directives
- [ ] 1.4: Create `ui/src/lib/utils.ts` with cn() helper

### Phase 2: shadcn/ui Components
- [ ] 2.1: Install Radix UI primitives and lucide-react
- [ ] 2.2: Create `ui/src/components/ui/button.tsx`
- [ ] 2.3: Create `ui/src/components/ui/avatar.tsx`
- [ ] 2.4a: Create `ui/src/components/ui/card.tsx`
- [ ] 2.4b: Create `ui/src/components/ui/scroll-area.tsx`
- [ ] 2.4c: Create `ui/src/components/ui/separator.tsx`
- [ ] 2.4d: Create `ui/src/components/ui/dropdown-menu.tsx`
- [ ] 2.4e: Create `ui/src/components/ui/tooltip.tsx`
- [ ] 2.4f: Create `ui/src/components/ui/collapsible.tsx`

### Phase 3: Sidebar Implementation
- [ ] 3.1: Create `ui/src/components/sidebar/Sidebar.tsx` with layout components
- [ ] 3.2: Create `ui/src/components/sidebar/OrganizationSelector.tsx`
- [ ] 3.3: Create `ui/src/components/sidebar/UserProfile.tsx` with initials avatar
- [ ] 3.4: Create `ui/src/components/sidebar/ProjectNavigation.tsx`

### Phase 4: Migrate Existing Components
- [ ] 4.1: Migrate `ui/src/components/VersionCard.tsx` to Tailwind
- [ ] 4.2: Migrate `ui/src/components/ProfilePage.tsx` to Tailwind
- [ ] 4.3: Update `ui/src/App.tsx` with new layout and sidebar
- [ ] 4.4: Update `ui/src/main.tsx` entry point

### Phase 5: Build System
- [ ] 5.1: Update `ui/vite.config.ts` with path aliases
- [ ] 5.2: Update `ui/tsconfig.json` with path mappings

### Phase 6: Testing and Cleanup
- [ ] 6.1: Update component unit tests
- [ ] 6.2: Update Playwright E2E tests
- [ ] 6.3: Remove MUI dependencies from package.json
- [ ] 6.4a: Verify all pages render correctly
- [ ] 6.4b: Verify OIDC authentication flow
- [ ] 6.4c: Verify RPC calls work
- [ ] 6.4d: Verify responsive behavior
- [ ] 6.4e: Run full E2E test suite

---

## Appendix: Key Differences from Material UI

| Aspect | Material UI | Tailwind + shadcn |
|--------|-------------|-------------------|
| Styling | CSS-in-JS (Emotion) | Utility classes |
| Theming | `createTheme()` + ThemeProvider | CSS variables + `tailwind.config.ts` |
| Components | Pre-built, opinionated | Copy-paste, customizable |
| Bundle size | Larger (includes all styles) | Smaller (purges unused) |
| Customization | Override theme tokens | Direct class manipulation |
| Dark mode | Theme mode prop | CSS class toggle |

## Appendix: Resources

- [Tailwind CSS Documentation](https://tailwindcss.com/docs)
- [shadcn/ui Documentation](https://ui.shadcn.com)
- [Radix UI Primitives](https://www.radix-ui.com/primitives)
- [Lucide Icons](https://lucide.dev/icons/)
- [How Anthropic Built Artifacts](https://newsletter.pragmaticengineer.com/p/how-anthropic-built-artifacts)

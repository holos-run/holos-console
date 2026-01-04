# Plan: Display ID Token Claims on Profile Page

> **Status:** APPROVED
>
> This plan has been reviewed and approved for implementation.

## Overview

Add a section to the Profile page that displays the OIDC ID Token claims in pretty-printed JSON format. This provides transparency to users about what identity information the application receives from the OIDC provider.

## Goal

When a user is authenticated, the Profile page will show:
1. The existing profile information (Name, Email, Subject)
2. A new collapsible section at the bottom showing the raw ID Token claims as formatted JSON

## Design Decisions

| Topic            | Decision                            | Rationale                                                  |
| ---------------- | ----------------------------------- | ---------------------------------------------------------- |
| Display location | Bottom of Profile page              | Non-intrusive; primary profile info remains prominent      |
| Format           | Pretty-printed JSON                 | Human-readable; shows all claims including custom ones     |
| UI component     | MUI Accordion (collapsible)         | Clean UI; collapsed by default to not overwhelm users      |
| Data source      | `user.profile` from oidc-client-ts  | Contains all ID token claims merged with userinfo response |
| Styling          | Monospace font, syntax highlighting | Developer-friendly for debugging token contents            |

## Current State

The Profile page (`ui/src/components/ProfilePage.tsx`) currently:
- Shows loading state while checking auth
- Shows "Sign In" button when unauthenticated
- When authenticated, displays:
  - Name: `user?.profile.name`
  - Email: `user?.profile.email`
  - Subject: `user?.profile.sub`
  - Sign Out button

The `user.profile` object from oidc-client-ts contains all OIDC standard claims:
- `sub`, `name`, `given_name`, `family_name`, `email`, `email_verified`
- `iss`, `aud`, `exp`, `iat`, `auth_time`, `nonce`, `acr`, `amr`, `azp`
- Plus any custom claims from the IDP

## Changes Required

### Modify (Existing Files)
- `ui/src/components/ProfilePage.tsx` - Add claims display section

### Add (New Files)
- None required (all changes in ProfilePage.tsx)

### E2E Tests
- `ui/e2e/auth.spec.ts` - Add test case with screenshot capture

## Implementation

### Phase 1: Add Claims Display Component

#### 1.1 Add claims section to ProfilePage

Update `ui/src/components/ProfilePage.tsx` to add a collapsible section showing ID token claims:

```tsx
import {
  Card, CardContent, Typography, Stack, Box, Button,
  Accordion, AccordionSummary, AccordionDetails
} from '@mui/material'
import ExpandMoreIcon from '@mui/icons-material/ExpandMore'

// In the authenticated return block, after the Sign Out button Box:
<Accordion sx={{ mt: 2 }}>
  <AccordionSummary expandIcon={<ExpandMoreIcon />}>
    <Typography variant="subtitle2">ID Token Claims</Typography>
  </AccordionSummary>
  <AccordionDetails>
    <Box
      component="pre"
      sx={{
        fontFamily: 'monospace',
        fontSize: '0.75rem',
        backgroundColor: 'grey.100',
        p: 2,
        borderRadius: 1,
        overflow: 'auto',
        maxHeight: 400,
      }}
    >
      {JSON.stringify(user?.profile, null, 2)}
    </Box>
  </AccordionDetails>
</Accordion>
```

#### 1.2 Import required MUI components

Add imports for Accordion components and ExpandMore icon.

### Phase 2: E2E Test with Screenshot

#### 2.1 Add test case for claims display

Add a new test to `ui/e2e/auth.spec.ts` that:
1. Logs in with valid credentials
2. Navigates to profile page
3. Expands the claims accordion
4. Takes a screenshot for visual verification

```typescript
test('should display ID token claims in profile page', async ({ page }) => {
  // Login flow (reuse from existing test)
  await page.goto('/ui/profile')
  await page.getByRole('button', { name: 'Sign In' }).click()

  // Complete login...
  await page.waitForURL(/\/dex\//, { timeout: 5000 })

  const connectorLink = page.locator('a[href*="connector"]').first()
  if ((await connectorLink.count()) > 0) {
    await connectorLink.click()
    await page.waitForLoadState('networkidle')
  }

  await page.locator('input[name="login"]').fill(DEFAULT_USERNAME)
  await page.locator('input[name="password"]').fill(DEFAULT_PASSWORD)
  await page.locator('button[type="submit"]').click()

  // Wait for profile page
  await page.waitForURL(/\/ui\/profile/, { timeout: 15000 })

  // Expand claims accordion
  await page.getByText('ID Token Claims').click()

  // Verify JSON content is visible
  await expect(page.locator('pre')).toBeVisible()
  await expect(page.locator('pre')).toContainText('"sub"')
  await expect(page.locator('pre')).toContainText('"iss"')

  // Take screenshot for visual verification
  await page.screenshot({
    path: 'e2e/screenshots/profile-claims.png',
    fullPage: true
  })
})
```

#### 2.2 Update Playwright config for screenshot output

Ensure screenshots directory exists and is configured in `.gitignore` if needed.

### Phase 3: Documentation

#### 3.1 Update any relevant docs

No documentation changes required - this is a simple UI enhancement.

---

## TODO (Implementation Checklist)

### Phase 1: Add Claims Display Component
- [x] 1.1: Add Accordion with claims JSON to ProfilePage.tsx
- [x] 1.2: Add required MUI imports (Accordion, ExpandMoreIcon)

### Phase 2: E2E Test with Screenshot
- [x] 2.1: Add E2E test case for claims display with screenshot
- [x] 2.2: Create e2e/screenshots directory (if needed)

---

## Testing

Run E2E tests to verify:
```bash
make test-e2e
```

After tests complete, examine the screenshot at:
```
ui/e2e/screenshots/profile-claims.png
```

The screenshot should show:
- Profile page header
- Name, Email, Subject fields populated
- Expanded "ID Token Claims" accordion
- Pretty-printed JSON with claims like `sub`, `iss`, `aud`, `exp`, etc.

## Security Considerations

- ID token claims are already available client-side (the SPA has access to them)
- No new information is exposed; this just makes existing data visible in the UI
- Useful for debugging and transparency
- Production deployments may want to reconsider if showing raw claims is appropriate for their use case

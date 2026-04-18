import {
  ENABLED_TOGGLE_ACTIVE_DESCRIPTION,
  ENABLED_TOGGLE_INACTIVE_DESCRIPTION,
  ORG_SCOPE_INDEX_DESCRIPTION,
  FOLDER_SCOPE_INDEX_DESCRIPTION,
  REQUIRE_RULE_DESCRIPTION,
  EXCLUDE_RULE_DESCRIPTION,
  enabledToggleDescription,
} from './platform-template-copy'

describe('platform-template-copy', () => {
  const constants: Record<string, string> = {
    ENABLED_TOGGLE_ACTIVE_DESCRIPTION,
    ENABLED_TOGGLE_INACTIVE_DESCRIPTION,
    ORG_SCOPE_INDEX_DESCRIPTION,
    FOLDER_SCOPE_INDEX_DESCRIPTION,
    REQUIRE_RULE_DESCRIPTION,
    EXCLUDE_RULE_DESCRIPTION,
  }

  it('exports non-empty strings for every copy constant', () => {
    for (const [name, value] of Object.entries(constants)) {
      expect(value, name).toEqual(expect.any(String))
      expect(value.trim().length, name).toBeGreaterThan(0)
    }
  })

  it('describes the active enabled state with eligibility and render-time language', () => {
    expect(ENABLED_TOGGLE_ACTIVE_DESCRIPTION.toLowerCase()).toContain('eligible')
    expect(ENABLED_TOGGLE_ACTIVE_DESCRIPTION.toLowerCase()).toContain('render')
  })

  it('describes the inactive enabled state with exclusion language', () => {
    const lower = ENABLED_TOGGLE_INACTIVE_DESCRIPTION.toLowerCase()
    expect(lower).toContain('disabled')
    expect(lower).toMatch(/hidden|excluded/)
  })

  it('does not contain the misleading "applied to" phrase anywhere', () => {
    for (const [name, value] of Object.entries(constants)) {
      expect(value.toLowerCase(), name).not.toContain('applied to')
    }
  })

  it('scopes org and folder index descriptions to their respective scopes', () => {
    expect(ORG_SCOPE_INDEX_DESCRIPTION.toLowerCase()).toContain('organization')
    expect(FOLDER_SCOPE_INDEX_DESCRIPTION.toLowerCase()).toContain('folder')
  })

  it('describes REQUIRE rules as affecting render-time inclusion only', () => {
    const lower = REQUIRE_RULE_DESCRIPTION.toLowerCase()
    expect(lower).toContain('require')
    expect(lower).toContain('render')
    expect(lower).not.toContain('force')
  })

  it('describes EXCLUDE rules as affecting render-time ref removal', () => {
    const lower = EXCLUDE_RULE_DESCRIPTION.toLowerCase()
    expect(lower).toContain('exclude')
    expect(lower).toContain('render')
  })

  describe('enabledToggleDescription', () => {
    it('returns the active description when enabled is true', () => {
      expect(enabledToggleDescription(true)).toBe(ENABLED_TOGGLE_ACTIVE_DESCRIPTION)
    })

    it('returns the inactive description when enabled is false', () => {
      expect(enabledToggleDescription(false)).toBe(ENABLED_TOGGLE_INACTIVE_DESCRIPTION)
    })
  })
})

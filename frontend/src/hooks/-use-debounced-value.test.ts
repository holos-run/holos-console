import { renderHook, act } from '@testing-library/react'
import { vi } from 'vitest'
import { useDebouncedValue } from './use-debounced-value'

describe('useDebouncedValue', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('returns the initial value immediately', () => {
    const { result } = renderHook(() => useDebouncedValue('hello', 500))
    expect(result.current).toBe('hello')
  })

  it('does not update the value before the delay elapses', () => {
    const { result, rerender } = renderHook(
      ({ value, delay }) => useDebouncedValue(value, delay),
      { initialProps: { value: 'hello', delay: 500 } }
    )
    rerender({ value: 'world', delay: 500 })
    act(() => {
      vi.advanceTimersByTime(499)
    })
    expect(result.current).toBe('hello')
  })

  it('updates the value after the delay elapses', () => {
    const { result, rerender } = renderHook(
      ({ value, delay }) => useDebouncedValue(value, delay),
      { initialProps: { value: 'hello', delay: 500 } }
    )
    rerender({ value: 'world', delay: 500 })
    act(() => {
      vi.advanceTimersByTime(500)
    })
    expect(result.current).toBe('world')
  })

  it('resets the timer when value changes again before delay', () => {
    const { result, rerender } = renderHook(
      ({ value, delay }) => useDebouncedValue(value, delay),
      { initialProps: { value: 'hello', delay: 500 } }
    )
    rerender({ value: 'world', delay: 500 })
    act(() => {
      vi.advanceTimersByTime(300)
    })
    rerender({ value: 'final', delay: 500 })
    act(() => {
      vi.advanceTimersByTime(300)
    })
    // 600ms total but timer was reset at 300ms, so only 300ms elapsed since last change
    expect(result.current).toBe('hello')
    act(() => {
      vi.advanceTimersByTime(200)
    })
    // Now 500ms elapsed since last change
    expect(result.current).toBe('final')
  })

  it('uses default delay when none provided', () => {
    const { result, rerender } = renderHook(
      ({ value }) => useDebouncedValue(value),
      { initialProps: { value: 'hello' } }
    )
    rerender({ value: 'world' })
    act(() => {
      vi.advanceTimersByTime(499)
    })
    expect(result.current).toBe('hello')
    act(() => {
      vi.advanceTimersByTime(1)
    })
    expect(result.current).toBe('world')
  })
})

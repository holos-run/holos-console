import { useState, useEffect } from 'react'

/**
 * Returns a debounced copy of `value` that only updates after `delay` ms of
 * inactivity.  Useful for deferring expensive side-effects (e.g. RPCs) until
 * the user stops typing.
 *
 * @param value - The value to debounce.
 * @param delay - Milliseconds to wait after the last change before updating
 *                the returned value. Defaults to 500 ms.
 */
export function useDebouncedValue<T>(value: T, delay = 500): T {
  const [debouncedValue, setDebouncedValue] = useState<T>(value)

  useEffect(() => {
    const timer = setTimeout(() => {
      setDebouncedValue(value)
    }, delay)

    return () => clearTimeout(timer)
  }, [value, delay])

  return debouncedValue
}

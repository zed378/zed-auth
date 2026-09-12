import { useEffect, useState } from "react";

/**
 * A value that lags behind, for search inputs that reach the server (P2-12).
 *
 * `P2-12` step 2 asks for the user search to be debounced server-side. Without
 * it every keystroke is a request: typing an eight-character address issues
 * eight, seven of which are already stale when they arrive, and they can come
 * back out of order — so the list can settle on the results for `budi@ex`
 * after the results for `budi@example.test` have already rendered.
 *
 * 250ms is the usual compromise: below about 150ms a moderate typist still
 * fires per keystroke, and above about 400ms the list feels detached from the
 * typing. The **input itself is never debounced** — it is controlled by the
 * raw value, so the field never lags behind the keyboard. Only the request
 * waits.
 */
export function useDebounced<T>(value: T, delayMs = 250): T {
  const [settled, setSettled] = useState(value);

  useEffect(() => {
    const timer = setTimeout(() => setSettled(value), delayMs);
    return () => clearTimeout(timer);
  }, [value, delayMs]);

  return settled;
}

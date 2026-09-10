import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";

import { authConfig, isConfigured } from "./config";
import { onUnauthorized } from "../api/client";
import { AuthError, beginLogin, logout as buildLogoutUrl, renewSilently } from "./oidc";
import { claimsFrom, clearToken, currentExpiry, currentToken, RENEW_MARGIN_MS } from "./tokens";
import type { Claims } from "./tokens";

/**
 * The console's authentication state (P1-21).
 *
 * Four states, and the distinction between the last two is what `docs/UI-UX/14`
 * asks for: "we are finding out" and "you are signed out" look different to a
 * user, and collapsing them produces the blank screen that document is about.
 */
export type Status =
  /** The first silent renewal has not finished. Render a loading state. */
  | "restoring"
  /** There is a usable token. */
  | "authenticated"
  /** There is no session. The user must log in. */
  | "anonymous"
  /** Something went wrong that logging in again will not fix. */
  | "failed";

interface AuthState {
  status: Status;
  claims: Claims | null;
  error: AuthError | null;

  /** Sends the browser to the identity provider. */
  login: (returnTo?: string) => Promise<void>;

  /** Ends the session here and there. */
  logout: () => void;

  /** Renews now. Used by the API client when a request comes back 401. */
  renew: () => Promise<boolean>;

  /** Whether the token carries a role. UI affordances only — never a control. */
  hasRole: (...roles: string[]) => boolean;
}

const AuthContext = createContext<AuthState | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const config = useMemo(authConfig, []);
  const [status, setStatus] = useState<Status>("restoring");
  const [claims, setClaims] = useState<Claims | null>(null);
  const [error, setError] = useState<AuthError | null>(null);

  // One renewal at a time. Without this, a burst of 401s produces a burst of
  // iframes, each racing the others to store a token.
  const inFlight = useRef<Promise<boolean> | null>(null);

  const adopt = useCallback(() => {
    const token = currentToken();
    setClaims(claimsFrom(token));
    setStatus(token === null ? "anonymous" : "authenticated");
  }, []);

  const renew = useCallback(async (): Promise<boolean> => {
    if (inFlight.current !== null) return inFlight.current;

    const attempt = (async () => {
      try {
        await renewSilently(config);
        adopt();
        return true;
      } catch (caught) {
        clearToken();
        setClaims(null);

        const authError = caught instanceof AuthError ? caught : null;
        if (authError !== null && authError.needsInteractiveLogin) {
          // Not an error state. There is simply no session, which is the
          // ordinary condition of a first visit — and the fallback is an
          // interactive login rather than a blank screen (ADR-019).
          setStatus("anonymous");
          setError(null);
        } else {
          setStatus("failed");
          setError(authError ?? new AuthError("renewal_failed", String(caught)));
        }
        return false;
      } finally {
        inFlight.current = null;
      }
    })();

    inFlight.current = attempt;
    return attempt;
  }, [adopt, config]);

  // A 401 from the Management API means the token expired between the timer's
  // last renewal and this request — a mid-action expiry, which is the case
  // P1-21 step 8 and docs/UI-UX/14 are about. Renewing here means the caller's
  // retry succeeds instead of the user losing what they were doing.
  useEffect(() => {
    onUnauthorized(renew);
  }, [renew]);

  // The first attempt: recover a session on load, because the token lives only
  // in memory and a reload has none (ADR-019).
  useEffect(() => {
    if (!isConfigured(config)) {
      setStatus("failed");
      setError(
        new AuthError(
          "not_configured",
          "VITE_AUTH_CLIENT_ID is not set, so the console does not know which application it is",
        ),
      );
      return;
    }
    if (currentToken() !== null) {
      adopt();
      return;
    }
    void renew();
  }, [adopt, config, renew]);

  // Renew ahead of expiry. A token that expires mid-action produces a 401 the
  // user did not cause and cannot understand.
  useEffect(() => {
    if (status !== "authenticated") return;

    const dueIn = Math.max(currentExpiry() - Date.now() - RENEW_MARGIN_MS, 0);
    const timer = window.setTimeout(() => void renew(), dueIn);
    return () => window.clearTimeout(timer);
  }, [status, claims, renew]);

  const login = useCallback(
    async (returnTo?: string) => {
      const destination = returnTo ?? `${window.location.pathname}${window.location.search}`;
      window.location.assign(await beginLogin(config, destination));
    },
    [config],
  );

  const logout = useCallback(() => {
    setClaims(null);
    setStatus("anonymous");
    window.location.assign(buildLogoutUrl(config, window.location.origin));
  }, [config]);

  const hasRole = useCallback(
    (...roles: string[]) => {
      if (claims === null) return false;
      return roles.some((role) => claims.roles.includes(role));
    },
    [claims],
  );

  const value = useMemo<AuthState>(
    () => ({ status, claims, error, login, logout, renew, hasRole }),
    [status, claims, error, login, logout, renew, hasRole],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthState {
  const value = useContext(AuthContext);
  if (value === null) {
    throw new Error("useAuth was called outside an AuthProvider");
  }
  return value;
}

import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";

import { authConfig } from "../lib/auth/config";
import { AuthError, completeLogin } from "../lib/auth/oidc";

/**
 * Where the identity provider sends the browser back (P1-21).
 *
 * It runs once, exchanges the code, and leaves. Nothing about it is a screen
 * the user should read — but it is a screen they will see if the exchange is
 * slow or fails, so it says what is happening rather than showing nothing
 * (`docs/UI-UX/14`).
 */
export function CallbackPage() {
  const navigate = useNavigate();
  const [error, setError] = useState<AuthError | null>(null);

  // React 18's StrictMode runs effects twice in development. An authorization
  // code is single-use, so the second run would fail against a code the first
  // one already spent — and the user would see an error on a login that
  // worked. This makes the exchange happen once per mount either way.
  const started = useRef(false);

  useEffect(() => {
    if (started.current) return;
    started.current = true;

    void (async () => {
      try {
        const { returnTo } = await completeLogin(authConfig(), window.location.search);

        // `replace`, so the back button does not return to a callback URL
        // whose code has been spent. And the code is gone from the address
        // bar, which keeps it out of the history and out of any referrer.
        navigate(returnTo, { replace: true });
      } catch (caught) {
        setError(
          caught instanceof AuthError ? caught : new AuthError("callback_failed", String(caught)),
        );
      }
    })();
  }, [navigate]);

  if (error !== null) {
    return (
      <main className="card">
        <h1>Sign-in did not complete</h1>
        <p>{describe(error)}</p>
        <p>
          <a href="/">Start again</a>
        </p>
      </main>
    );
  }

  return (
    <main className="card" aria-busy="true">
      <h1>Signing you in</h1>
      <p>One moment.</p>
    </main>
  );
}

/**
 * What to tell the user.
 *
 * The provider's `error_description` is not shown. It is written for a
 * developer, it can carry detail about why an authorization failed, and on a
 * page anybody can reach it is a small disclosure surface for no benefit — the
 * user's next action is the same in every case.
 */
function describe(error: AuthError): string {
  switch (error.code) {
    case "invalid_state":
      return "The response did not match the request this tab made. Start again from the beginning.";
    case "access_denied":
      return "Sign-in was refused.";
    case "invalid_request":
      return "This link is no longer valid. Start again from the beginning.";
    default:
      return "Something went wrong while signing you in. Start again from the beginning.";
  }
}

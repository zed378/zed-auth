import { useState } from "react";

import { useAuth } from "../lib/auth/AuthProvider";

/**
 * What an anonymous visitor sees (P1-21).
 *
 * It collects nothing. There is no password field here and there never will
 * be: the console authenticates through the same hosted login page every other
 * client uses (`docs/PLAN/02` § Constraints), and a console with its own
 * credential form would be exactly the backdoor that constraint forbids.
 */
export function SignInPage() {
  const { login } = useAuth();
  const [going, setGoing] = useState(false);

  return (
    <main className="card">
      <h1>Sign in</h1>
      <p>You need to sign in to use the console.</p>
      <button
        type="button"
        disabled={going}
        onClick={() => {
          setGoing(true);
          // Where they were headed, so a deep link survives the round trip
          // rather than dropping them on the overview.
          void login();
        }}
      >
        {going ? "Taking you there…" : "Continue to sign in"}
      </button>
    </main>
  );
}

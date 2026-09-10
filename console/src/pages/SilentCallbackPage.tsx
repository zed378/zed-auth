import { useEffect } from "react";

/**
 * The redirect target inside the hidden renewal iframe (P1-21, ADR-019).
 *
 * It reads the query string and posts the result to the parent window. It does
 * NOT exchange the code: the exchange happens in the parent, where the
 * verifier is — the frame never sees it, so a frame that somehow ran somebody
 * else's page could not complete an exchange with what it has.
 *
 * The user never sees this. It renders nothing.
 */
export function SilentCallbackPage() {
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const message = {
      type: "zedauth:silent-renewal",
      state: params.get("state") ?? "",
      code: params.get("code") ?? undefined,
      error: params.get("error") ?? undefined,
    };

    // **Targeted at this exact origin, not `*`.** A `*` target posts the
    // authorization code to whatever happens to be embedding this page, which
    // for a page whose entire job is to carry a code is the whole problem.
    window.parent.postMessage(message, window.location.origin);
  }, []);

  return null;
}

// The SPA's whole client: Authorization Code + PKCE, run in the browser.
//
// **Where the token lives is the interesting part.** It is a module-scoped
// variable and nothing else — not `localStorage`, not a cookie this script can
// read. A token in `localStorage` survives the tab, which sounds like a
// feature until an injected script reads it; ADR-019 made exactly this call
// for the console and the reasoning is identical here.
//
// The PKCE verifier, state and nonce DO go to `sessionStorage`, because they
// must survive a full-page redirect to the identity provider and back. They
// are single-use, consumed on return, and useless to anybody who reads them:
// the verifier without the code proves nothing.

// The ID token and the nonce that was asked for when it was minted, in memory,
// for this tab only. They travel together because the API requires both.
let held = null;

const PENDING = "demo_spa_pending";

const el = (id) => document.getElementById(id);

function show(problem) {
  el("problem").textContent = problem;
  el("problem").hidden = false;
}

async function config() {
  const response = await fetch("/config.json");
  if (!response.ok) throw new Error("this application is misconfigured");
  return response.json();
}

const base64url = (bytes) =>
  btoa(String.fromCharCode(...new Uint8Array(bytes)))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/, "");

function random() {
  return base64url(crypto.getRandomValues(new Uint8Array(32)));
}

async function challenge(verifier) {
  // S256, never "plain". A plain challenge is the verifier, so an attacker who
  // can see the authorization request can complete the exchange.
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(verifier));
  return base64url(digest);
}

async function beginLogin() {
  const cfg = await config();
  const verifier = random();
  const state = random();
  const nonce = random();

  sessionStorage.setItem(PENDING, JSON.stringify({ verifier, state, nonce }));

  const params = new URLSearchParams({
    response_type: "code",
    client_id: cfg.client_id,
    redirect_uri: cfg.redirect_uri,
    scope: "openid",
    state,
    nonce,
    code_challenge: await challenge(verifier),
    code_challenge_method: "S256",
  });
  window.location.assign(`${cfg.issuer}/oauth/authorize?${params}`);
}

async function completeLogin(query) {
  const raw = sessionStorage.getItem(PENDING);
  // Consumed on use, whatever happens next. A pending request left in place is
  // one that can be completed twice.
  sessionStorage.removeItem(PENDING);

  // Clean the code and state out of the address bar before anything else, so a
  // copied URL or a referrer cannot carry them.
  window.history.replaceState({}, "", window.location.pathname);

  if (raw === null) throw new Error("no sign-in is in progress");
  const pending = JSON.parse(raw);

  if (query.get("state") !== pending.state) {
    // Not the request this tab started. That is login CSRF, not a glitch.
    throw new Error("that sign-in did not start here");
  }

  const error = query.get("error");
  if (error !== null) throw new Error(`the sign-in was refused: ${error}`);

  const code = query.get("code");
  if (code === null) throw new Error("no authorization code came back");

  const cfg = await config();
  const response = await fetch(`${cfg.issuer}/oauth/token`, {
    method: "POST",
    headers: { "Content-Type": "application/x-www-form-urlencoded" },
    // No client_secret: there is none. `client_id` in the body is how a public
    // client identifies itself, and PKCE is what proves it is the same client
    // that started the flow.
    body: new URLSearchParams({
      grant_type: "authorization_code",
      code,
      redirect_uri: cfg.redirect_uri,
      client_id: cfg.client_id,
      code_verifier: pending.verifier,
    }),
  });

  if (!response.ok) throw new Error("the token exchange failed");
  const body = await response.json();
  if (!body.id_token) throw new Error("no id_token came back");

  held = { token: body.id_token, nonce: pending.nonce };
}

async function loadSession() {
  const response = await fetch("/api/me", {
    headers: {
      Authorization: `Bearer ${held.token}`,
      // What this tab expects the token to say. The API enforces it; the
      // browser cannot, because it never opens the token.
      "X-Demo-Expected-Nonce": held.nonce,
    },
    cache: "no-store",
  });
  const body = await response.json();
  if (!response.ok) {
    held = null;
    throw new Error(body.error ?? "this application would not accept that token");
  }
  return body;
}

function renderSignedIn(session) {
  el("subject").textContent = session.subject;
  el("org").textContent = session.org_id;
  el("expires").textContent = session.expires_at;
  el("verified-by").textContent = session.verified_by;
  el("signed-in").hidden = false;
  el("signed-out").hidden = true;
  el("status").textContent = "";
}

function renderSignedOut() {
  el("signed-out").hidden = false;
  el("signed-in").hidden = true;
  el("status").textContent = "";
}

el("signin").addEventListener("click", () => {
  beginLogin().catch((problem) => show(problem.message));
});

el("signout").addEventListener("click", async () => {
  // The token goes first. If the redirect below fails to happen — a blocked
  // navigation, a slow network — this tab must already be signed out rather
  // than still holding a usable credential.
  held = null;
  const cfg = await config();
  const params = new URLSearchParams({
    post_logout_redirect_uri: cfg.redirect_uri,
    client_id: cfg.client_id,
  });
  window.location.assign(`${cfg.issuer}/oidc/logout?${params}`);
});

async function start() {
  const query = new URLSearchParams(window.location.search);
  if (!query.has("code") && !query.has("error")) {
    renderSignedOut();
    return;
  }
  try {
    await completeLogin(query);
    renderSignedIn(await loadSession());
  } catch (problem) {
    renderSignedOut();
    show(problem.message);
  }
}

start();

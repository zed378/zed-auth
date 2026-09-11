// Demo application B: a public SPA and the resource API behind it (P1-26).
//
// The other client profile. There is no client secret: the browser IS the
// OAuth client, it runs the Authorization Code flow with PKCE itself, and the
// token it receives lives in a JavaScript variable that dies with the tab —
// never in `localStorage`, which any injected script can read (ADR-019 made
// the same call for the console).
//
// This binary is two things that a real SPA deployment also keeps together:
//
//	/         the static files, served as they are
//	/api/me   this application's OWN resource server
//
// **The authorization decision is made here, in the API, not in the browser.**
// A SPA can render whatever it likes from a token it holds; that is a display
// choice, not a control. The same rule the console follows (`CLAUDE.md`: a
// hidden button is not a security control) applies to a consumer application,
// so the check that matters runs server-side — and it runs locally, against a
// cached key set, with no call back to the auth service.
package main

import (
	"crypto/subtle"
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/zed378/zed-auth/demo/internal/verify"
)

//go:embed static
var staticFiles embed.FS

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		healthcheck(env("DEMO_ADDR", ":8091"))
		return
	}

	issuer := required("DEMO_ISSUER")
	clientID := required("DEMO_CLIENT_ID")
	baseURL := required("DEMO_BASE_URL")
	addr := env("DEMO_ADDR", ":8091")

	app := &spa{verifier: verify.New(issuer, clientID)}

	assets, err := fs.Sub(staticFiles, "static")
	if err != nil {
		log.Fatalf("embedded assets: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(assets)))
	mux.HandleFunc("/api/me", app.me)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Configuration reaches the browser as data, not as a template
	// substitution into a script. A client_id interpolated into JavaScript is
	// a string that has to be escaped correctly forever; a JSON document is
	// one that never has to be.
	//
	// Nothing here is secret: a public client has no secret to leak, which is
	// the whole reason PKCE exists.
	mux.HandleFunc("/config.json", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{
			"issuer":       strings.TrimRight(issuer, "/"),
			"client_id":    clientID,
			"redirect_uri": strings.TrimRight(baseURL, "/") + "/",
		})
	})

	log.Printf("demo SPA listening on %s as %s", addr, clientID)
	server := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(server.ListenAndServe())
}

type spa struct{ verifier *verify.Verifier }

// nonceHeader carries what the browser expects the token's nonce to be.
//
// The nonce is the client's replay defence, and in this profile the client is
// a browser that never parses the token — so the check has to happen here,
// against a value only the tab that started the sign-in knows. A token lifted
// from somewhere else arrives without it and is refused.
const nonceHeader = "X-Demo-Expected-Nonce"

// me answers the SPA's one API call.
//
// Every failure answers 401 with a reason the browser can show. The reason
// names the CHECK that failed, never the token — a resource server that echoes
// a rejected credential puts it in the browser's console, its own access log
// and whatever aggregates them (`CLAUDE.md`: never log tokens).
func (s *spa) me(w http.ResponseWriter, r *http.Request) {
	header := r.Header.Get("Authorization")
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || token == "" {
		w.Header().Set("WWW-Authenticate", `Bearer realm="demo-spa"`)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "no bearer token"})
		return
	}

	expectedNonce := r.Header.Get(nonceHeader)
	if expectedNonce == "" {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_request"`)
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "no expected nonce; this API will not accept a token on its own",
		})
		return
	}

	claims, err := s.verifier.Verify(r.Context(), token)
	if err != nil {
		// Logged without the token, and deliberately WITH the reason: the
		// wrong-audience refusal is the one an operator most needs to see, and
		// "401" alone would make it indistinguishable from an expiry.
		log.Printf("refused a token: %v", err)
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}

	// Constant time, because this compares a secret the caller supplied with
	// one the token carries. A length-revealing early exit here is a smaller
	// leak than most, and it costs nothing to not have it.
	if subtle.ConstantTimeCompare([]byte(claims.Nonce), []byte(expectedNonce)) != 1 {
		log.Printf("refused a token: the nonce is not the one this sign-in asked for")
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "that token was not minted for this sign-in",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"subject":      claims.Subject,
		"org_id":       claims.OrgID,
		"expires_at":   time.Unix(claims.ExpiresAt, 0).UTC().Format(time.RFC3339),
		"auth_methods": claims.AuthMethods,
		"verified_by":  "the demo SPA's own API, locally, against the cached key set",
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	// No caching of an authorization decision, ever.
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("writing a response: %v", err)
	}
}

func required(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatalf("%s is required", name)
	}
	return value
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

// healthcheck lets the container probe itself.
//
// The runtime image is distroless: no shell, no curl, no wget. The service's
// own image solved this the same way (ADR-008) — the binary is the probe. A
// compose healthcheck that has to be `disable: true` because the image is
// minimal is a container whose failures are invisible until someone looks.
func healthcheck(addr string) {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		log.Fatalf("cannot probe %q: %v", addr, err)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Get("http://127.0.0.1:" + port + "/healthz")
	if err != nil {
		log.Fatalf("unhealthy: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		log.Fatalf("unhealthy: /healthz answered %d", response.StatusCode)
	}
}

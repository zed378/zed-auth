package mfa

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/zed378/zed-auth/backend/internal/storage/postgres"
)

// WebAuthn as a second factor (P3-05).
//
// Specification: MEMORY/specs/P3-05-webauthn.md.
//
// The property this exists for is that it **cannot be phished**. TOTP can: a
// convincing lookalike asks for six digits and relays them inside the
// thirty-second window, and the user has no way to tell. An authenticator signs
// over the origin it is actually talking to, so a credential registered for
// this service produces nothing a lookalike can use.
//
// Everything below that reads as fussiness — the origin check, the challenge
// binding, the RP ID — is that property. Skip any one of them and what is left
// is a second factor that is merely inconvenient.
//
// **A library, deliberately**, and the opposite call from `P3-02`, where TOTP
// was written into the repository rather than vendored. The reasoning there was
// that thirty frozen lines are not worth a dependency in the authentication
// path. This is CBOR, COSE key parsing, five attestation formats and three
// signature algorithms against an evolving specification — and, decisively, the
// two get things wrong differently. A hand-rolled TOTP bug produces codes no
// app agrees with, which somebody notices within a day. A hand-rolled COSE bug
// produces a signature check that accepts forgeries and looks perfect.
//
// `docs/PLAN/07` asks for exactly this call: don't reinvent cryptography.

// WebAuthnFactors is the credential store this verifier needs.
type WebAuthnFactors interface {
	Credentials(ctx context.Context, tx *postgres.Tx, userID string) ([]StoredCredential, error)
	InsertCredential(ctx context.Context, tx *postgres.Tx, in NewCredential, now time.Time) (string, error)
	RecordSignCount(ctx context.Context, tx *postgres.Tx, factorID string, count uint32, at time.Time) error
	Activate(ctx context.Context, tx *postgres.Tx, factorID string) error
	Delete(ctx context.Context, tx *postgres.Tx, factorID string) error
}

// StoredCredential is one registered authenticator, as this package holds it.
type StoredCredential struct {
	FactorID     string
	CredentialID []byte
	PublicKey    []byte
	SignCount    uint32
	Status       Status
	Label        string

	// UserVerified records whether the authenticator verified the USER at
	// registration — a PIN, a fingerprint, a face — rather than merely that
	// somebody was present to touch it (card step 6).
	//
	// The distinction is the difference between a true second factor and a
	// second step. A security key that only proves presence proves that the key
	// is plugged in, which is not a factor if the key never leaves the laptop.
	UserVerified bool
}

// NewCredential is a registration about to be stored.
type NewCredential struct {
	UserID       string
	OrgID        string
	Label        string
	CredentialID []byte
	PublicKey    []byte
	SignCount    uint32
	UserVerified bool
	AAGUID       []byte
	Transports   []string
}

// Errors this ceremony distinguishes from a wrong answer.
var (
	// ErrOriginMismatch is an assertion signed for a different origin.
	//
	// **Never a typo and never a user's mistake.** It is a phishing attempt or a
	// misconfigured deployment, and those are the only two things it can be —
	// which is why it is distinguishable in the log even though the browser is
	// told the same thing as for any other failure.
	ErrOriginMismatch = errors.New("mfa: the assertion was signed for a different origin")

	// ErrClonedAuthenticator is a signature counter that went backwards.
	//
	// Not a wrong answer: it means two authenticators are presenting one
	// credential, which is either a clone or a malfunction. Refused either way.
	ErrClonedAuthenticator = errors.New("mfa: the signature counter went backwards, so the authenticator may be cloned")
)

// maxAssertionBytes bounds an assertion before it reaches a CBOR parser.
//
// CBOR is an attacker-supplied encoding and a parser is an attack surface. A
// real assertion is a couple of kilobytes; 64 KiB is far beyond any of them and
// small enough that this endpoint is not a place to push bytes at a decoder.
const maxAssertionBytes = 64 << 10

// WebAuthnVerifier performs both ceremonies.
type WebAuthnVerifier struct {
	Store WebAuthnFactors
	DB    Tenant
	Log   *slog.Logger

	// RP is the configured Relying Party. Built once at startup from the
	// deployment's own origin, never from a request — see NewWebAuthn.
	RP *webauthn.WebAuthn

	Now func() time.Time
}

func (w *WebAuthnVerifier) now() time.Time {
	if w.Now != nil {
		return w.Now()
	}
	return time.Now()
}

func (w *WebAuthnVerifier) log() *slog.Logger {
	if w.Log != nil {
		return w.Log
	}
	return slog.Default()
}

// NewWebAuthn builds the Relying Party from a deployment's own issuer URL.
//
// **The RP ID and the origin come from configuration, never from a request**,
// which is the whole phishing defence. A service that took its expected origin
// from the `Origin` header would accept whatever a lookalike sent, and every
// check below would pass while proving nothing.
//
// The RP ID is the issuer's host with no scheme and no port, per the
// specification. `docs/PLAN/09` and `P2-09`'s tenant resolution interact here
// and the interaction is worth naming: a credential is scoped to its RP ID, so
// **a subdomain-per-organization deployment would give each organization
// credentials that do not work on any other subdomain**. This service resolves
// tenants by path and token rather than by subdomain (`PG-33`), so one RP ID
// covers the estate — which is the reason that resolution choice is load
// bearing rather than cosmetic.
func NewWebAuthn(issuer, displayName string) (*webauthn.WebAuthn, error) {
	origin := strings.TrimRight(strings.TrimSpace(issuer), "/")
	if origin == "" {
		return nil, fmt.Errorf("mfa: a WebAuthn relying party needs the deployment's own origin")
	}

	host := origin
	for _, scheme := range []string{"https://", "http://"} {
		host = strings.TrimPrefix(host, scheme)
	}
	if idx := strings.IndexAny(host, "/:"); idx >= 0 {
		host = host[:idx]
	}
	if host == "" {
		return nil, fmt.Errorf("mfa: %q has no host to use as a relying party id", issuer)
	}

	return webauthn.New(&webauthn.Config{
		RPID:          host,
		RPDisplayName: displayName,
		RPOrigins:     []string{origin},
	})
}

func (w *WebAuthnVerifier) Type() Type { return TypeWebAuthn }

// --- the registration ceremony ------------------------------------------------

// BeginRegistration starts enrolling an authenticator.
//
// **It writes nothing.** Unlike `P3-02`'s TOTP enrolment, which stores a
// pending factor because generating a secret is not proof the user can produce
// codes from it, there is no half-finished state to keep here: the row is
// created by FinishRegistration, from a ceremony that has already proven
// itself. Nothing about the account changes until then.
//
// The returned options and session are what the browser and the finish step
// need. The session carries the challenge and **must not be given to the
// client to hold** — it goes in the per-login server-side state.
func (w *WebAuthnVerifier) BeginRegistration(
	ctx context.Context, userID, orgID, label, displayName string,
) (options []byte, session *webauthn.SessionData, err error) {
	user := &ceremonyUser{id: userID, name: displayName, display: displayName}

	// Existing credentials are excluded, so an authenticator already registered
	// to this account cannot be registered twice — the browser tells the user
	// rather than silently creating a second row for one device.
	err = w.DB.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		stored, err := w.Store.Credentials(ctx, tx, userID)
		if err != nil {
			return err
		}
		for _, c := range stored {
			user.credentials = append(user.credentials, webauthn.Credential{
				ID:        c.CredentialID,
				PublicKey: c.PublicKey,
			})
		}
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("mfa: reading existing credentials: %w", err)
	}

	creation, session, err := w.RP.BeginRegistration(
		user,
		webauthn.WithExclusions(user.CredentialDescriptors()),
		// Preferred rather than required: an authenticator that can verify the
		// user should, and one that cannot is still useful as a second factor.
		// What is NOT done is pretending afterwards that it did — the flag is
		// recorded and `UserVerified` says which happened.
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			UserVerification: protocol.VerificationPreferred,
		}),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("mfa: beginning a WebAuthn registration: %w", err)
	}

	options, err = json.Marshal(creation)
	if err != nil {
		return nil, nil, fmt.Errorf("mfa: encoding registration options: %w", err)
	}
	return options, session, nil
}

// FinishRegistration stores a proven credential.
func (w *WebAuthnVerifier) FinishRegistration(
	ctx context.Context, userID, orgID, label, displayName string,
	session webauthn.SessionData, response []byte,
) (factorID string, err error) {
	parsed, err := protocol.ParseCredentialCreationResponseBytes(bounded(response))
	if err != nil {
		return "", fmt.Errorf("%w: the registration response could not be read", ErrWrongCode)
	}

	user := &ceremonyUser{id: userID, name: displayName, display: displayName}

	credential, err := w.RP.CreateCredential(user, session, parsed)
	if err != nil {
		return "", w.classify(err)
	}

	err = w.DB.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		id, err := w.Store.InsertCredential(ctx, tx, NewCredential{
			UserID:       userID,
			OrgID:        orgID,
			Label:        label,
			CredentialID: credential.ID,
			PublicKey:    credential.PublicKey,
			SignCount:    credential.Authenticator.SignCount,
			UserVerified: credential.Flags.UserVerified,
			AAGUID:       credential.Authenticator.AAGUID,
			Transports:   transportsOf(credential),
		}, w.now())
		if err != nil {
			return err
		}
		factorID = id

		// Active immediately, unlike TOTP. The difference is real rather than
		// an inconsistency: a TOTP enrolment is `pending` because generating a
		// secret does not prove the user can produce codes from it, so a second
		// step asks them to. A registration ceremony IS that proof — the
		// authenticator signed this service's challenge — so there is nothing
		// left to confirm.
		return w.Store.Activate(ctx, tx, id)
	})
	if err != nil {
		return "", fmt.Errorf("mfa: storing a WebAuthn credential: %w", err)
	}

	return factorID, nil
}

// --- the authentication ceremony --------------------------------------------------

// BeginLogin builds the options for a challenge.
//
// Returns nil options when the user holds no active credential, so a caller can
// tell "no passkey" from "a passkey and something went wrong".
func (w *WebAuthnVerifier) BeginLogin(
	ctx context.Context, userID, orgID string,
) (options []byte, session *webauthn.SessionData, err error) {
	user := &ceremonyUser{id: userID}

	err = w.DB.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		stored, err := w.Store.Credentials(ctx, tx, userID)
		if err != nil {
			return err
		}
		for _, c := range stored {
			if c.Status != StatusActive {
				continue
			}
			user.credentials = append(user.credentials, webauthn.Credential{
				ID:        c.CredentialID,
				PublicKey: c.PublicKey,
				Authenticator: webauthn.Authenticator{
					SignCount: c.SignCount,
				},
			})
		}
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("mfa: reading credentials: %w", err)
	}

	if len(user.credentials) == 0 {
		return nil, nil, nil
	}

	assertion, session, err := w.RP.BeginLogin(user)
	if err != nil {
		return nil, nil, fmt.Errorf("mfa: beginning a WebAuthn login: %w", err)
	}

	options, err = json.Marshal(assertion)
	if err != nil {
		return nil, nil, fmt.Errorf("mfa: encoding assertion options: %w", err)
	}
	return options, session, nil
}

// FinishLogin verifies an assertion and reports which factor answered.
//
// The counter is checked and recorded here rather than left to the caller,
// because a caller that forgot would lose cloned-authenticator detection
// silently — the ceremony would still verify, since a clone holds the real key.
func (w *WebAuthnVerifier) FinishLogin(
	ctx context.Context, userID, orgID string, session webauthn.SessionData, response []byte,
) (factorID string, err error) {
	parsed, err := protocol.ParseCredentialRequestResponseBytes(bounded(response))
	if err != nil {
		return "", fmt.Errorf("%w: the assertion could not be read", ErrWrongCode)
	}

	user := &ceremonyUser{id: userID}
	byCredential := map[string]StoredCredential{}

	err = w.DB.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		stored, err := w.Store.Credentials(ctx, tx, userID)
		if err != nil {
			return err
		}
		for _, c := range stored {
			// **The second of two status filters, and today the redundant one.**
			//
			// BeginLogin applies the same test, and the session's allowed-
			// credential list is built from what it returned — so a pending
			// credential can never appear in a ceremony this service issued, and
			// the library refuses it before reaching here. A mutation run
			// confirmed that: removing this changes no outcome.
			//
			// It stays for the path that does not exist yet. A discoverable
			// ("passkey") login carries no allow-list, because the authenticator
			// chooses the credential — so whenever that lands, this becomes the
			// only status check between a half-finished registration and a
			// session. Cheaper to keep than to remember.
			if c.Status != StatusActive {
				continue
			}
			byCredential[string(c.CredentialID)] = c
			user.credentials = append(user.credentials, webauthn.Credential{
				ID:        c.CredentialID,
				PublicKey: c.PublicKey,
				Authenticator: webauthn.Authenticator{
					SignCount: c.SignCount,
				},
			})
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("mfa: reading credentials: %w", err)
	}

	// **The user's own credentials, and only those.** A credential belonging to
	// somebody else is not in this list, so the library resolves it to nothing
	// rather than to its real owner — abuse case A-4, closed by what is loaded
	// rather than by a check afterwards.
	credential, err := w.RP.ValidateLogin(user, session, parsed)
	if err != nil {
		return "", w.classify(err)
	}

	stored, known := byCredential[string(credential.ID)]
	if !known {
		// **Defence in depth, and unreachable today.** The library resolves the
		// assertion against the credential list this function just loaded, so a
		// credential it matched is one this map holds; a mutation run confirmed
		// that removing this changes no test outcome.
		//
		// It stays because of what the NEXT line would otherwise do: `stored`
		// would be a zero value, the counter check would compare against zero,
		// and RecordSignCount would be handed an empty factor id. That is a
		// silent write to nothing rather than a refusal. The guard costs a map
		// lookup and converts a library change from a corrupt update into an
		// error.
		//
		// What actually enforces "another user's credential does not work" is
		// the query that loaded the map — scoped to this user — and that is
		// what the mutation list targets.
		return "", ErrWrongCode
	}

	// The signature counter (card step 4, F-4).
	//
	// An authenticator that counts increments on every assertion, so a counter
	// that did not move means a second copy of the private key signed something
	// this service has already seen. Many platform authenticators — phones,
	// Touch ID — always report zero, and refusing those would exclude most
	// users; so a stored-and-reported zero is accepted and anything else must
	// strictly increase.
	reported := credential.Authenticator.SignCount
	if !counterAcceptable(stored.SignCount, reported) {
		w.log().Warn("a WebAuthn signature counter went backwards; the authenticator may be cloned",
			"factor_id", stored.FactorID, "stored", stored.SignCount, "presented", reported)
		return "", ErrClonedAuthenticator
	}

	if err := w.DB.WithTenant(ctx, orgID, func(tx *postgres.Tx) error {
		return w.Store.RecordSignCount(ctx, tx, stored.FactorID, reported, w.now())
	}); err != nil {
		// Logged, not fatal. The assertion verified; failing the login over a
		// counter write would refuse somebody who authenticated correctly.
		w.log().Warn("recording a WebAuthn signature counter failed", "error", err.Error())
	}

	return stored.FactorID, nil
}

// counterAcceptable reports whether a presented counter may follow a stored one.
//
// Both zero means the authenticator does not count — accepted, because most
// platform authenticators do not and excluding them would exclude most users.
// Otherwise it must strictly increase: equal is a replay, lower is a clone.
func counterAcceptable(stored, presented uint32) bool {
	if stored == 0 && presented == 0 {
		return true
	}
	return presented > stored
}

// classify turns a library error into one this package's callers understand.
func (w *WebAuthnVerifier) classify(err error) error {
	var protocolErr *protocol.Error
	if errors.As(err, &protocolErr) {
		// The origin check is the phishing defence, so it is distinguishable in
		// the log even though the browser is told what every other failure is
		// told. A mismatch is never a typo: it is a lookalike, or a deployment
		// whose configured origin is wrong.
		detail := strings.ToLower(protocolErr.Details + " " + protocolErr.DevInfo)
		if strings.Contains(detail, "origin") {
			w.log().Warn("a WebAuthn assertion was signed for a different origin",
				"detail", protocolErr.Details)
			return ErrOriginMismatch
		}
	}
	return fmt.Errorf("%w: %s", ErrWrongCode, err.Error())
}

// bounded caps what reaches a CBOR parser.
//
// Truncating rather than refusing, deliberately: a truncated assertion fails to
// parse, which is the same answer an oversized one would get, and it means this
// function cannot become a second place where a valid request is rejected for a
// reason the caller cannot see.
func bounded(response []byte) []byte {
	if len(response) > maxAssertionBytes {
		return response[:maxAssertionBytes]
	}
	return response
}

func transportsOf(c *webauthn.Credential) []string {
	out := make([]string, 0, len(c.Transport))
	for _, t := range c.Transport {
		out = append(out, string(t))
	}
	return out
}

// --- the library's User, over this service's own -------------------------------------

// ceremonyUser adapts a user id to what the library asks for.
//
// The WebAuthn user handle is **this service's user id**, not an email and not
// a display name. The specification is explicit that authentication decisions
// must be made on the handle rather than on anything human-readable, and a
// handle that changed when somebody changed their address would invalidate
// every credential they hold.
type ceremonyUser struct {
	id          string
	name        string
	display     string
	credentials []webauthn.Credential
}

func (c *ceremonyUser) WebAuthnID() []byte { return []byte(c.id) }

func (c *ceremonyUser) WebAuthnName() string {
	if c.name != "" {
		return c.name
	}
	return c.id
}

func (c *ceremonyUser) WebAuthnDisplayName() string {
	if c.display != "" {
		return c.display
	}
	return c.WebAuthnName()
}

func (c *ceremonyUser) WebAuthnCredentials() []webauthn.Credential { return c.credentials }

// CredentialDescriptors lists what this user already holds, for exclusion.
func (c *ceremonyUser) CredentialDescriptors() []protocol.CredentialDescriptor {
	out := make([]protocol.CredentialDescriptor, 0, len(c.credentials))
	for _, cred := range c.credentials {
		out = append(out, protocol.CredentialDescriptor{
			Type:         protocol.PublicKeyCredentialType,
			CredentialID: cred.ID,
		})
	}
	return out
}

// EncodeCredentialID renders a credential id for a log or a page.
//
// base64url, which is what the browser uses, so an operator comparing a log
// line against a devtools panel is comparing the same string.
func EncodeCredentialID(id []byte) string {
	return base64.RawURLEncoding.EncodeToString(id)
}

// decodeCredentialID reads a stored credential id back to bytes.
func decodeCredentialID(encoded string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(encoded)
}

// --- the framework's seam --------------------------------------------------------

// Options begins a ceremony for a user's registered credentials.
//
// The session is returned as JSON so the framework can put it in the challenge
// without this package's types leaking into the challenge's encoding.
func (w *WebAuthnVerifier) Options(ctx context.Context, orgID, userID string) ([]byte, []byte, error) {
	options, session, err := w.BeginLogin(ctx, userID, orgID)
	if err != nil || options == nil {
		return nil, nil, err
	}

	encoded, err := json.Marshal(session)
	if err != nil {
		return nil, nil, fmt.Errorf("mfa: encoding a passkey session: %w", err)
	}
	return options, encoded, nil
}

// Verify checks an assertion against the session that issued it.
func (w *WebAuthnVerifier) Verify(
	ctx context.Context, orgID, userID string, session, assertion []byte,
) (string, error) {
	var decoded webauthn.SessionData
	if err := json.Unmarshal(session, &decoded); err != nil {
		// The session is this service's own, so a session that will not decode
		// is a bug or a corrupted store — never something a caller did. It must
		// not read as a wrong answer.
		return "", fmt.Errorf("mfa: decoding a passkey session: %w", err)
	}
	return w.FinishLogin(ctx, userID, orgID, decoded, assertion)
}

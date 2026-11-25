# Social login configuration and outage recovery

Status: P4-10 implementation in review; live-provider acceptance pending. P4-11
authenticated linking is not implemented. Keep providers disabled on deployed
environments until acceptance and account linking are available. Do not insert
identity links manually as a substitute for authenticated linking.

References: `TASKS/specs/P4-10-social-login-federation.md`, ADR-028/030,
`docs/PLAN/05-API-CONTRACT.md` (social login), and `docs/PLAN/08-AUTHORIZATION.md`
(organization policy). This runbook describes operator steps, not evidence that
they have been executed.

## Configure a provider

Use the environment file consumed by the selected deployment workflow. Both VM
Compose manifests explicitly pass the optional settings in `env.example` to the
service. Leave all settings for unused providers blank. Partial configuration
is rejected. The local HTTP Compose stack does not enable social login.

Register a separate application for each environment at the provider. Set its
callback to the exact HTTPS issuer origin followed by the path below. Do not
include a query, fragment, trailing slash, or wildcard.

| Provider | Callback path | Required settings |
|---|---|---|
| Google | `/login/social/google/callback` | `AUTH_SOCIAL_GOOGLE_CLIENT_ID`, `AUTH_SOCIAL_GOOGLE_CLIENT_SECRET_REF` |
| Microsoft | `/login/social/microsoft/callback` | `AUTH_SOCIAL_MICROSOFT_CLIENT_ID`, `AUTH_SOCIAL_MICROSOFT_CLIENT_SECRET_REF`, `AUTH_SOCIAL_MICROSOFT_TENANT_ID` |
| GitHub | `/login/social/github/callback` | `AUTH_SOCIAL_GITHUB_CLIENT_ID`, `AUTH_SOCIAL_GITHUB_CLIENT_SECRET_REF` |

Microsoft requires a single canonical tenant UUID; `common` and `organizations`
are unsupported. GitHub uses OAuth profile/email APIs. Google and GitHub require
verified email evidence, but matching email never creates or links an account.

Store each client secret through the existing private secret-file workflow in
the mounted host secrets directory. Set the reference to its **container** path,
for example `file:/etc/zed-auth/secrets/social-google-client-secret`. Run the
existing `secrets.sh fix` with the correct explicit `AUTH_SECRETS_DIR` after adding
files; the service runs as uid 65532 and refuses group/world-readable files. See
the ownership instructions in `README.md`. Keep credentials out of command
arguments, logs, tickets, and rendered Compose output. VM manifests support file
references through the existing read-only mount; they do not forward arbitrary
environment variables named by an `env:` reference.

Use the established deployment/recreation workflow to apply environment changes.
Keep `password` in the organization's `allowed_login_methods` and verify that
members have a working local password and required MFA before allowing provider
names (`google`, `microsoft`, `github`) through organization settings. Configuration
alone does not make an unlinked account eligible to sign in.

## Acceptance record required before enablement

Use dedicated non-production accounts after authenticated linking is available.
For each provider, record the build, environment, time, internal test account ID,
and pass/fail results without recording codes, state, cookies, or provider tokens.

1. Complete the normal application authorization flow through the hosted login
   button and verify the local session's provider method and application `amr`.
2. With MFA required, verify a provider login still requires the local factor.
3. Refuse an unlinked provider account without creating a user or matching email.
4. Disallow the provider after starting a flow; verify its callback is refused.
5. Replay a completed callback; verify no second session is issued.
6. Verify password login and MFA remain usable when the provider is unavailable.
7. Check success/failure audit events for internal IDs and absence of credentials.

Automated mock-provider integration tests support this record, but do not replace
provider registration and live browser acceptance.

## Provider outage or secret rotation

Keep password login available and direct affected users to it. Social attempts
fail closed; the service does not skip identity checks or MFA during an outage.
A user without a usable password must follow the existing password reset flow
and satisfy MFA. If that recovery path is also unavailable, use the organization's
established identity-verification/support procedure; do not attach an identity
by matching an email address or editing the database.

To suspend one provider, remove it from the organization's allowed methods while
retaining `password`. To disable it service-wide, clear **all** settings for that
provider and recreate the service through the deployment workflow. These actions
stop new social sign-ins; existing sessions require separate revocation if the
incident warrants it. For rotation, replace the private secret file and apply
the provider's rotation procedure; verify a fresh sign-in before retiring the
old credential. Never share provider registrations or secrets across environments.

For a code rollback to a version without social primary-method support, first
disable social starts and allow pending state, MFA challenges, and enrollment
flows to expire (at least 15 minutes). Retain the additive identity table unless
its separate migration rollback has been deliberately assessed.

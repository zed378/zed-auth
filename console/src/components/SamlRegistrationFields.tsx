import type { components } from "../lib/api/schema.gen";

export type SamlAttribute = components["schemas"]["SamlAttribute"];
export type SamlRegistration = components["schemas"]["SamlRegistration"];
export type SamlRegistrationInput = components["schemas"]["SamlRegistrationInput"];

/**
 * What each releasable attribute is, in the words an administrator reads.
 *
 * A `Record` over the generated enum rather than an array of names, so it is
 * exhaustive by construction: a sixth attribute added to the contract fails the
 * type check here until somebody decides how to describe it. A list would let
 * the new attribute exist in the API and be impossible to tick in the console.
 */
const attributeLabels: Record<SamlAttribute, string> = {
  email: "Email address",
  display_name: "Display name",
  username: "Username",
  role_keys: "Roles in this project",
};

const attributes = Object.keys(attributeLabels) as SamlAttribute[];

export type SamlDraft = {
  source: "metadata" | "manual";
  metadataXml: string;
  entityId: string;
  acsUrl: string;
  certificate: string;
  attributeRelease: SamlAttribute[];
  wantSignedRequests: boolean;
  allowIdpInitiated: boolean;
};

export function emptySamlDraft(): SamlDraft {
  return {
    source: "metadata",
    metadataXml: "",
    entityId: "",
    acsUrl: "",
    certificate: "",
    attributeRelease: [],
    wantSignedRequests: false,
    allowIdpInitiated: false,
  };
}

/** A draft seeded from a stored registration, for editing. */
export function draftFrom(registration: SamlRegistration): SamlDraft {
  return {
    source: "manual",
    metadataXml: "",
    entityId: registration.entity_id,
    acsUrl: registration.acs_url,
    certificate: registration.certificate ?? "",
    attributeRelease: [...registration.attribute_release],
    wantSignedRequests: registration.want_signed_requests,
    allowIdpInitiated: registration.allow_idp_initiated,
  };
}

/**
 * The request body for a draft.
 *
 * Metadata OR the individual fields, never both — the service refuses a body
 * carrying both rather than guessing which one was meant, so the console never
 * sends the half the administrator is not looking at.
 */
export function inputFrom(draft: SamlDraft): SamlRegistrationInput {
  const shared = {
    attribute_release: draft.attributeRelease,
    allow_idp_initiated: draft.allowIdpInitiated,
  };
  if (draft.source === "metadata") {
    // want_signed_requests is left to the document: the service provider is
    // the party that knows whether it signs, and the metadata says so.
    return { ...shared, metadata_xml: draft.metadataXml };
  }
  return {
    ...shared,
    entity_id: draft.entityId.trim(),
    acs_url: draft.acsUrl.trim(),
    want_signed_requests: draft.wantSignedRequests,
    ...(draft.certificate.trim() === "" ? {} : { certificate: draft.certificate }),
  };
}

/** Whether a draft has enough in it to send. The server decides the rest. */
export function draftIsComplete(draft: SamlDraft): boolean {
  if (draft.source === "metadata") return draft.metadataXml.trim() !== "";
  return draft.entityId.trim() !== "" && draft.acsUrl.trim() !== "";
}

/**
 * The SAML half of registering or editing an application (P4-09).
 *
 * Two things in this form cost something, and each says what rather than
 * leaving the checkbox to speak for itself — a toggle whose consequence is
 * invisible is a toggle an administrator flips for the wrong reason:
 *
 * - Requiring signed requests is enforced on HTTP-POST and **refused** on
 *   HTTP-Redirect. A service provider that signs and sends on Redirect will
 *   stop being able to sign anyone in.
 * - Allowing sign-on started from this service means an assertion the service
 *   provider never asked for, which it cannot tell apart from one somebody else
 *   caused.
 */
export function SamlRegistrationFields({
  idPrefix,
  draft,
  onChange,
  allowMetadata = true,
}: {
  idPrefix: string;
  draft: SamlDraft;
  onChange: (next: SamlDraft) => void;
  allowMetadata?: boolean;
}) {
  const set = (patch: Partial<SamlDraft>) => onChange({ ...draft, ...patch });
  const id = (name: string) => `${idPrefix}-${name}`;
  const fieldClass =
    "mt-1 w-full rounded border border-border bg-bg-base px-3 py-2 text-body text-text-primary";
  const monoClass =
    "mt-1 w-full rounded border border-border bg-bg-base px-3 py-2 font-mono text-small text-text-primary";

  return (
    <>
      {allowMetadata ? (
        <fieldset className="mt-4">
          <legend className="text-small font-medium text-text-secondary">
            How to describe the service provider
          </legend>
          <div className="mt-1 flex flex-wrap gap-4">
            <label className="flex items-center gap-2 text-body text-text-primary">
              <input
                type="radio"
                name={id("source")}
                checked={draft.source === "metadata"}
                onChange={() => set({ source: "metadata" })}
              />
              Paste its metadata
            </label>
            <label className="flex items-center gap-2 text-body text-text-primary">
              <input
                type="radio"
                name={id("source")}
                checked={draft.source === "manual"}
                onChange={() => set({ source: "manual" })}
              />
              Enter the details
            </label>
          </div>
        </fieldset>
      ) : null}

      {draft.source === "metadata" && allowMetadata ? (
        <>
          <label htmlFor={id("metadata")} className="mt-4 block text-small font-medium text-text-secondary">
            Metadata document
          </label>
          <textarea
            id={id("metadata")}
            rows={6}
            value={draft.metadataXml}
            onChange={(event) => set({ metadataXml: event.currentTarget.value })}
            aria-describedby={id("metadata-help")}
            className={monoClass}
          />
          <p id={id("metadata-help")} className="mt-1 text-small text-text-secondary">
            The XML the other party publishes. Its entity ID, where assertions go, and its
            signing certificate are read from it here, so nothing has to be retyped.
          </p>
        </>
      ) : (
        <>
          <label htmlFor={id("entity")} className="mt-4 block text-small font-medium text-text-secondary">
            Entity ID
          </label>
          <input
            id={id("entity")}
            value={draft.entityId}
            onChange={(event) => set({ entityId: event.currentTarget.value })}
            aria-describedby={id("entity-help")}
            className={fieldClass}
          />
          <p id={id("entity-help")} className="mt-1 text-small text-text-secondary">
            Compared exactly against the Issuer of every sign-in request it sends.
          </p>

          <label htmlFor={id("acs")} className="mt-4 block text-small font-medium text-text-secondary">
            Assertion Consumer Service URL
          </label>
          <input
            id={id("acs")}
            value={draft.acsUrl}
            onChange={(event) => set({ acsUrl: event.currentTarget.value })}
            aria-describedby={id("acs-help")}
            className={fieldClass}
          />
          <p id={id("acs-help")} className="mt-1 text-small text-text-secondary">
            The only address a signed assertion is ever sent to, whatever a request asks for.
            Must be https.
          </p>

          <label htmlFor={id("certificate")} className="mt-4 block text-small font-medium text-text-secondary">
            Signing certificate (optional)
          </label>
          <textarea
            id={id("certificate")}
            rows={4}
            value={draft.certificate}
            onChange={(event) => set({ certificate: event.currentTarget.value })}
            aria-describedby={id("certificate-help")}
            className={monoClass}
          />
          <p id={id("certificate-help")} className="mt-1 text-small text-text-secondary">
            PEM. Needed only if its sign-in requests must be signed.
          </p>

          <label className="mt-4 flex items-start gap-2 text-body text-text-primary">
            <input
              type="checkbox"
              className="mt-1"
              checked={draft.wantSignedRequests}
              onChange={(event) => set({ wantSignedRequests: event.currentTarget.checked })}
              aria-describedby={id("signed-help")}
            />
            Require its sign-in requests to be signed
          </label>
          <p id={id("signed-help")} className="mt-1 text-small text-text-secondary">
            Checked on the HTTP-POST binding. A signed request sent on HTTP-Redirect is refused,
            so a service provider that uses that binding will stop being able to sign anyone in.
          </p>
        </>
      )}

      <fieldset className="mt-4">
        <legend className="text-small font-medium text-text-secondary">What it receives</legend>
        <p className="mt-1 text-small text-text-secondary">
          A stable identifier for each person is always sent. Nothing else is, unless ticked.
        </p>
        <div className="mt-1 flex flex-col gap-1">
          {attributes.map((name) => (
            <label key={name} className="flex items-center gap-2 text-body text-text-primary">
              <input
                type="checkbox"
                checked={draft.attributeRelease.includes(name)}
                onChange={(event) =>
                  set({
                    attributeRelease: event.currentTarget.checked
                      ? [...draft.attributeRelease, name]
                      : draft.attributeRelease.filter((existing) => existing !== name),
                  })
                }
              />
              {attributeLabels[name]}
            </label>
          ))}
        </div>
      </fieldset>

      <label className="mt-4 flex items-start gap-2 text-body text-text-primary">
        <input
          type="checkbox"
          className="mt-1"
          checked={draft.allowIdpInitiated}
          onChange={(event) => set({ allowIdpInitiated: event.currentTarget.checked })}
          aria-describedby={id("idp-help")}
        />
        Allow sign-on started from this service
      </label>
      <p id={id("idp-help")} className="mt-1 text-small text-text-secondary">
        Lets a link here sign someone in without the service provider asking. It cannot tell
        such a sign-in apart from one somebody else caused, so leave this off unless it needs
        portal links.
      </p>
    </>
  );
}

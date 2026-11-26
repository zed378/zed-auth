import type { ChangeEvent } from "react";
import type { components } from "../lib/api/schema.gen";
import { ChoiceField, FormField, TextField } from "./FormField";

export type SamlAttribute = components["schemas"]["SamlAttribute"];
export type SamlRegistration = components["schemas"]["SamlRegistration"];
export type SamlRegistrationInput = components["schemas"]["SamlRegistrationInput"];

export type SamlFieldName = "saml.metadata_xml" | "saml.entity_id" | "saml.acs_url"
  | "saml.certificate" | "saml.attribute_release" | "saml.want_signed_requests" | "saml.allow_idp_initiated";
export type SamlFormValues = Record<SamlFieldName, string>;
type SamlFieldBinding = {
  name: string;
  value: string;
  error?: string;
  disabled: boolean;
  onChange: (event: ChangeEvent<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>) => void;
  onBlur: () => void;
};
export type SamlFieldBindings = Record<SamlFieldName, SamlFieldBinding>;

export function samlFieldBindings(field: (name: SamlFieldName) => SamlFieldBinding): SamlFieldBindings {
  return Object.fromEntries(Object.keys(samlFormValues(emptySamlDraft())).map((name) => [name, field(name as SamlFieldName)])) as SamlFieldBindings;
}

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

export function samlFormValues(draft: SamlDraft): SamlFormValues {
  return {
    "saml.metadata_xml": draft.metadataXml,
    "saml.entity_id": draft.entityId,
    "saml.acs_url": draft.acsUrl,
    "saml.certificate": draft.certificate,
    "saml.attribute_release": "",
    "saml.want_signed_requests": "",
    "saml.allow_idp_initiated": "",
  };
}

export function samlDraftValues(draft: SamlDraft, values: SamlFormValues): SamlDraft {
  return { ...draft, metadataXml: values["saml.metadata_xml"], entityId: values["saml.entity_id"],
    acsUrl: values["saml.acs_url"], certificate: values["saml.certificate"] };
}

/** Only presence is checked here; protocol and certificate validation stays server-side. */
export function samlFormRules(draft: SamlDraft, active: (values: Record<string, string>) => boolean) {
  return {
    "saml.metadata_xml": { label: "Metadata document", validate: (value: string, values: Record<string, string>) =>
      active(values) && draft.source === "metadata" && !value.trim() ? "Paste the service provider's metadata document." : undefined },
    "saml.entity_id": { label: "Entity ID", validate: (value: string, values: Record<string, string>) =>
      active(values) && draft.source === "manual" && !value.trim() ? "Enter the service provider's entity ID." : undefined },
    "saml.acs_url": { label: "Assertion Consumer Service URL", validate: (value: string, values: Record<string, string>) =>
      active(values) && draft.source === "manual" && !value.trim() ? "Enter the Assertion Consumer Service URL." : undefined },
    "saml.certificate": { label: "Signing certificate" },
    "saml.attribute_release": { label: "What it receives" },
    "saml.want_signed_requests": { label: "Signed sign-in requests" },
    "saml.allow_idp_initiated": { label: "Sign-on started from this service" },
  };
}

/** A metadata-derived error belongs to the document; indexed attributes share one editor. */
export function samlFormFailure(failure: unknown, draft: SamlDraft): unknown {
  if (typeof failure !== "object" || failure === null || !("error" in failure)) return failure;
  const error = failure.error;
  if (typeof error !== "object" || error === null || !("details" in error) || !Array.isArray(error.details)) return failure;
  return { ...failure, error: { ...error, details: error.details.map((detail: unknown) => {
    if (typeof detail !== "object" || detail === null || !("field" in detail) || typeof detail.field !== "string") return detail;
    let field = detail.field.replace(/^saml\.attribute_release(?:\[\d+\]|\.\d+)$/, "saml.attribute_release");
    if (draft.source === "metadata" && ["saml.entity_id", "saml.acs_url", "saml.certificate", "saml.want_signed_requests"].includes(field)) field = "saml.metadata_xml";
    return { ...detail, field };
  }) } };
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
  fields,
}: {
  idPrefix: string;
  draft: SamlDraft;
  onChange: (next: SamlDraft) => void;
  allowMetadata?: boolean;
  fields?: SamlFieldBindings;
}) {
  const set = (patch: Partial<SamlDraft>) => onChange({ ...draft, ...patch });
  const id = (name: string) => `${idPrefix}-${name}`;
  const text = (name: SamlFieldName, key: "metadataXml" | "entityId" | "acsUrl" | "certificate") => ({
    name, value: fields?.[name].value ?? draft[key], disabled: fields?.[name].disabled,
    onBlur: fields?.[name].onBlur,
    onChange: fields?.[name].onChange ?? ((event: ChangeEvent<HTMLInputElement | HTMLTextAreaElement>) => set({ [key]: event.currentTarget.value })),
  });
  const choices = (name: SamlFieldName) => ({ name, error: fields?.[name].error, onBlur: fields?.[name].onBlur });
  const monoClass = "min-h-24 w-full rounded border border-border bg-bg-base px-3 py-2 font-mono text-small text-text-primary focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent disabled:cursor-not-allowed disabled:opacity-60";

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
                className="size-6 accent-accent"
                name={id("source")}
                checked={draft.source === "metadata"}
                onChange={() => set({ source: "metadata" })}
              />
              Paste its metadata
            </label>
            <label className="flex items-center gap-2 text-body text-text-primary">
              <input
                type="radio"
                className="size-6 accent-accent"
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
        <div className="mt-4"><FormField id={id("metadata")} label="Metadata document" error={fields?.["saml.metadata_xml"].error}
          helperText="The XML the other party publishes. Its entity ID, where assertions go, and its signing certificate are read from it here, so nothing has to be retyped.">
          {({ id, describedBy }) => <textarea {...text("saml.metadata_xml", "metadataXml")} id={id} rows={6}
            className={monoClass} aria-invalid={Boolean(fields?.["saml.metadata_xml"].error)} aria-describedby={describedBy} />}
        </FormField></div>
      ) : (
        <div className="mt-4 space-y-4">
          <TextField id={id("entity")} label="Entity ID" {...text("saml.entity_id", "entityId")} error={fields?.["saml.entity_id"].error}
            helperText="Compared exactly against the Issuer of every sign-in request it sends." />
          <TextField id={id("acs")} label="Assertion Consumer Service URL" {...text("saml.acs_url", "acsUrl")} error={fields?.["saml.acs_url"].error}
            helperText="The only address a signed assertion is ever sent to, whatever a request asks for. Must be https." />
          <FormField id={id("certificate")} label="Signing certificate (optional)" error={fields?.["saml.certificate"].error}
            helperText="PEM. Needed only if its sign-in requests must be signed.">
            {({ id, describedBy }) => <textarea {...text("saml.certificate", "certificate")} id={id} rows={4}
              className={monoClass} aria-invalid={Boolean(fields?.["saml.certificate"].error)} aria-describedby={describedBy} />}
          </FormField>
          <ChoiceField id={id("signed")} label="Require its sign-in requests to be signed"
            {...choices("saml.want_signed_requests")} checked={draft.wantSignedRequests}
            onChange={(event) => set({ wantSignedRequests: event.currentTarget.checked })}
            helperText="Checked on the HTTP-POST binding. A signed request sent on HTTP-Redirect is refused, so a service provider that uses that binding will stop being able to sign anyone in." />
        </div>
      )}

      <fieldset className="mt-4" aria-describedby={fields?.["saml.attribute_release"].error ? id("attributes-error") : undefined}>
        <legend className="text-small font-medium text-text-secondary">What it receives</legend>
        <p id={id("attributes-help")} className="mt-1 text-small text-text-secondary">
          A stable identifier for each person is always sent. Nothing else is, unless ticked.
        </p>
        <div className="mt-1 flex flex-col gap-1">
          {attributes.map((name) => (
            <label key={name} className="flex min-h-11 items-center gap-3 text-body text-text-primary">
              <input type="checkbox" className="size-6 accent-accent focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent"
                name="saml.attribute_release" onBlur={fields?.["saml.attribute_release"].onBlur}
                aria-invalid={Boolean(fields?.["saml.attribute_release"].error)}
                aria-describedby={id(fields?.["saml.attribute_release"].error ? "attributes-error" : "attributes-help")}
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
        {fields?.["saml.attribute_release"].error ? <p id={id("attributes-error")} className="text-small text-danger">{fields["saml.attribute_release"].error}</p> : null}
      </fieldset>

      <div className="mt-4"><ChoiceField id={id("idp")} label="Allow sign-on started from this service"
          {...choices("saml.allow_idp_initiated")}
          checked={draft.allowIdpInitiated}
          onChange={(event) => set({ allowIdpInitiated: event.currentTarget.checked })}
          helperText="Lets a link here sign someone in without the service provider asking. It cannot tell such a sign-in apart from one somebody else caused, so leave this off unless it needs portal links."
        /></div>
    </>
  );
}

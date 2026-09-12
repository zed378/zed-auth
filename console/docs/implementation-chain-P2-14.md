# Implementation chain — P2-14

`docs/UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md`, run for the Policies (Access) screen.

"Not applicable" is an acceptable answer; silence is not.

---

## Screen: Policies — Access

| Step | Answer |
|---|---|
| **Design** | `docs/UI-UX/08` § Policies — Access tab, mapping onto `organizations.settings` (`docs/PLAN/08` Part B). Four grouped sections in the order an administrator thinks about them: passwords, sessions, sign-in methods, multi-factor |
| **Component** | Shared `Button`, `Badge`, `ConfirmDialog`, `ErrorState`, `Skeleton`, and two local field components. No design-system addition: a labelled number input with a range and a "what it is today" line has one caller, and a component with one caller is a generalisation nobody asked for |
| **State** | Loading, loaded, refused, editing, invalid per field, unchanged (nothing to save), changed harmlessly, changed in a way that reduces access, confirming, saving, save refused by the server, saved |
| **Interaction** | Edits are local until saved, so nothing is applied halfway. Save is disabled until something changed AND everything validates. A change that takes access away opens a confirmation naming who it affects; one that does not is applied directly. Clearing a field means "use the service default" and says so |
| **API Dependency** | `GET`/`PATCH /v1/organizations/{org_id}`. The PATCH sends the **whole** settings document, not the changed fields — so what the screen shows and what is stored are the same thing. The API merges key by key, deeply since this task fixed the shallow merge |
| **Loading** | One skeleton the height of the form. Not per-field: the form is a single read, and four skeletons for one request implies four things arriving separately |
| **Error** | `ErrorState` for the read, distinguishing refusal from failure. A refused save renders the server's own `details[0].issue` where there is one, inline, and **leaves the form as it was** — discarding somebody's edits because the server disagreed with one of them is the worst possible response to a validation error |
| **Empty** | Not applicable: an organization always has settings, whether stored or defaulted. The nearest thing is a setting with no stored value, which is a **third state** rather than an empty one and gets its own sentence: "Not set — the service applies 12" |
| **Permission** | The route is guarded to `ORG_OWNER`/`INSTANCE_OWNER`, which matches the API's requirement for `PATCH /v1/organizations/{org_id}`. UX only; the API refuses independently |
| **Responsive** | `max-w-2xl` on the form, `max-w-prose` on every explanatory paragraph, number inputs at a fixed `w-32` so they do not stretch to the width of a section. Nothing is a table, so no column decision arises |
| **Accessibility** | Each section is a real `fieldset` with a `legend`, so a screen-reader user hears which policy a field belongs to; errors use `aria-invalid` + `aria-describedby` and **replace** the helper text; the save confirmation lists consequences as a real `ul`; the saved notice is `role="status"`; axe runs over the populated form |
| **Test** | 16 tests in `src/pages/policies.test.tsx`, plus 7 mutations each turning its own test red |

---

## Where the numbers come from

Three places already held the instance defaults and they already knew it: the `organizations.settings` column DEFAULT, and `authn.DefaultPolicy` / `authn.DefaultLoginPolicy`. `authn/policy.go` says as much in a comment — the two cover different failures, so neither is redundant.

Step 6 asks this screen to show "the current effective values", which means the console needs them too. Restating them here would have made a **fourth** copy, in the surface furthest from the enforcement.

So they went into the contract — `default:` on each property of `OrganizationSettings`, which is where a consumer would look for them anyway — and `console/scripts/gen-patterns.mjs` now emits them to `console/src/lib/api/settings.gen.ts` alongside the bounds. `TestSpecDefaultsMatchTheService` fails if what the contract publishes stops matching what the service applies.

That is a drift gate, not a fourth copy: the generated Go file is used by nothing except the test that compares it to the real values.

### One side effect worth knowing

Adding `default:` made `openapi-typescript` mark those properties **required** in the generated types — correct for a response, wrong for a partial `PATCH` body, and it broke the build immediately. `--default-non-nullable false` restores the previous typing. The flag is in `package.json` so `scripts/check.sh`'s regenerate-and-diff gate uses it too.

---

## The bug this screen found

`openapi/openapi.yaml` has always promised of the settings PATCH:

> `settings` is merged key by key, so an update naming one setting leaves the rest as they were.

`jsonb || jsonb` is a **shallow** merge. `{"password_policy": {"min_length": 16}}` replaced the entire `password_policy` object, discarding `require_uppercase` and `max_age_days`.

Raising `min_length` from 12 to 16 is an unambiguous tightening, and it silently reset a deliberate `require_uppercase: false` back to the instance default and dropped a deliberate `max_age_days: 0`. Nothing reported it. The policy in force afterwards was one nobody chose and looked exactly like the one they did.

Fixed with a general `jsonb_deep_merge` rather than a special case for `password_policy`, so the next nested setting does not rediscover it. Objects recurse; **everything else is replaced**, which is what keeps `allowed_login_methods` removable — a merge that concatenated arrays would make it impossible to ever take a login method away.

This screen sends the whole document anyway, so it would not have hit the bug. Every other API consumer would.

# Implementation chain — P4-06

`docs/UI-UX/19-FRONTEND-IMPLEMENTATION-CHAIN.md`, run for Granted Projects — the
receiving side of delegation.

"Not applicable" is an acceptable answer; silence is not.

---

## Screen: Granted Projects

| Step | Answer |
|---|---|
| **Design** | `docs/UI-UX/04` Flow 3, `docs/UI-UX/08` § Granted Projects list, `docs/UI-UX/06` § Role-Source Visual Treatment. One addition, decided rather than drifted: an **ended** grant that still has holders gets a "Review" action opening the same panel with the assignment form removed. `docs/UI-UX/08` says an ended grant offers no assignment control; it does not say the rows left behind should be unreachable, and P4-02 lets an administrator clear them |
| **Component** | Shared `Table`, `Badge`, `Button`, `SidePanel`, `ConfirmDialog`, `RoleList`/`RoleSourceBadge`. No new shared component, and none needed fixing this time |
| **State** | Loading, loaded, refused, genuinely empty, panel open on an active grant (nobody assigned; holders listed; a person chosen; roles ticked; assigning; server refused), panel open on an ended grant (state explained, no form, holders removable), removal confirmation, removal refused, no permission to manage |
| **Interaction** | A row's "Assign roles" opens a side panel rather than a modal: the list stays readable, which matters when several grants come from the same partner. The panel holds the current holders above the form, so the answer to "who already has this" is visible before adding another. Assign is disabled until both a person and at least one role are chosen. Removal is a confirmation naming the person, the roles, and the partner's project |
| **API Dependency** | `GET /v1/organizations/{org_id}/project-grants` — **added by this task** (migration 039 for the names, P4-06 for the route). `GET`/`POST .../project-grants/{grant_id}/user-grants` and `DELETE .../user-grants/{user_id}` from P4-02. `GET .../users` for the person picker. The project's own role list is **never** requested: it belongs to the granting organization, and the grant's `granted_role_keys` is the only role source this screen has |
| **Loading** | Three skeleton rows in the table. Inside the panel, the holder list says "Loading…" rather than rendering as empty, which would read as "nobody has these roles" |
| **Error** | `ErrorState` in place of the table, with retry. A refused assignment or removal renders the server's `details[0].issue`, and the panel keeps the choices. A `409` from a grant revoked a moment ago arrives as that sentence rather than a generic failure (A-5) |
| **Empty** | Not "nothing has been created here": this organization **cannot** create one. The empty state says only another organization can grant a project, and what will appear when one does. No action button, because there is no action |
| **Permission** | Route and controls gated to `ORG_ADMIN`/`ORG_OWNER`/`INSTANCE_OWNER`. The API enforces `ORG_ADMIN` over the organization in the path independently, and answers another organization's path with `404` rather than `403` — an organization the caller has no role in is not confirmed to exist |
| **Responsive** | Project and From cells stack their ID beneath the name rather than adding columns. Status and Granted are `secondary`, so they drop at tablet width; the roles, the holder count and the actions never do. Role badges wrap and are never truncated |
| **Accessibility** | Table caption; the roles in a real `fieldset`/`legend`; each checkbox labelled by its role badge; the person picker is a labelled `select`; the ended-grant explanation is `role="status"` so it is announced when the panel opens; the delegated badge's source organization is in the accessibility tree as text, never only as a `title` — hover is an affordance for a mouse and nothing else; axe runs over the table with an active and an ended grant |
| **Test** | 14 tests in `src/pages/grantedprojects.test.tsx`. `e2e/grantedprojects.spec.ts` drives Flow 3 against the real service: it assigns a delegated role, checks the Management API holds it, removes it, and checks the API again |

---

## Colour

- `color-danger`: the holder's Remove control (`danger-text`) and the confirmation's
  confirm button. Nothing else on the screen uses it.
- Status is carried by badge text ("Active", "Ended"), with colour as reinforcement
  only. An ended grant is `muted`, not `danger`: the access ending is a fact, not a
  destructive action this administrator is about to take.
- The delegated badge's accent border is reinforcement for a mark and a sentence, never
  the sole carrier.

## The rule this screen exists to keep

`docs/UI-UX/08` says a role the grant does not delegate is **not rendered** — not
disabled, not greyed. The stronger form is what is implemented: the screen never learns
that the partner's other roles exist. `granted_role_keys` is its only role source, and a
test asserts the console makes no request for the granting project's roles at all.

A disabled checkbox would be a worse failure than it looks. It tells a partner
administrator that a role exists and that someone could enable it, which produces a
support conversation between two organizations about a decision only one of them gets to
make.

## Two things the server had to provide

1. **The names.** A grant row is visible to both parties, but the project it names and
   the organization that made it are the granting side's rows, which the receiving
   tenant's RLS hides. Without migration 039 this screen would show two UUIDs. The
   function is `SECURITY DEFINER` and bounded to grants whose `granted_org_id` is the
   caller's own organization; an integration test asks it for a grant between two other
   organizations and asserts it answers nothing.
2. **The blast radius from this side.** `holder_count` on `ReceivedGrant` counts this
   organization's own users holding a role through the grant, so the list can say
   whether an ended grant still has anything to clear up.

## What this screen still cannot do

The people assigned here cannot sign in to the granting organization's applications —
cross-organization sign-in is ADR-025 and has no task card yet. The roles are real: they
are what the granting organization's `/v1/authz/check` returns for those users (P4-04),
and nothing else consumes them yet. The screen does not claim otherwise, and the panel's
sentence — "gives that person access to `<partner>`'s project — not to anything here" —
is the closest it comes to the subject.

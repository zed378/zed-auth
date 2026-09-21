// Package grantsql holds the SQL that turns a `user_grants` row into the roles
// it actually confers.
//
// It is a package of its own, with no dependencies, because BOTH readers need
// it — the token claim in `internal/grant` and `/v1/authz/check` in
// `internal/authz` — and `internal/authz` already imports `internal/role`,
// while `internal/grant`'s own tests wire an authz handler into a server. One
// of those directions would be a cycle. A shared leaf package is also the
// honest shape: this statement is the definition of "effective roles", and
// neither reader owns it.
package grantsql

// EffectiveRoleKeys is the one statement that turns a `user_grants` row into
// the roles it actually confers (P4-04).
//
// A direct row confers its `role_keys`. A DELEGATED row — one carrying a
// `project_grant_id`, written by the receiving organization under `P4-02` —
// confers `role_keys ∩ granted_role_keys`, and nothing at all once the grant is
// revoked. Both readers of this table use it: the token claim here, and
// `/v1/authz/check` in `internal/authz`.
//
// # Why the intersection is computed in SQL
//
// The subset was already validated when the delegated row was written, so
// re-deriving it here looks redundant. It is not: `docs/PLAN/08` Part C, and
// `CLAUDE.md`'s third non-negotiable, require the subset to hold **on every
// request**, and a write-time check cannot speak for a grant that changed
// afterwards. Computing it against the grant row in the same statement means
// nothing — no cache, no in-process copy — sits between the grant and the
// answer (threat review T4-2).
//
// It takes $1 = user id and $2 = project id.
const EffectiveRoleKeys = `
	SELECT CASE
	         WHEN ug.project_grant_id IS NULL THEN ug.role_keys
	         WHEN pg.status <> 'active' THEN '{}'::text[]
	         ELSE (SELECT coalesce(array_agg(k ORDER BY k), '{}')
	                 FROM unnest(ug.role_keys) AS k
	                WHERE k = ANY (pg.granted_role_keys))
	       END
	  FROM user_grants ug
	  LEFT JOIN project_grants pg ON pg.id = ug.project_grant_id
	 WHERE ug.user_id = $1 AND ug.project_id = $2`

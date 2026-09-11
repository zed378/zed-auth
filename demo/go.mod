// The demo consumer applications (P1-26).
//
// A module of its own, not a package inside `backend/`. Two reasons, and the
// second is the one that matters:
//
//   1. These applications are meant to be copied. A consumer team should be
//      able to lift `webapp/` or `spa/` wholesale, and a file that imports
//      `backend/internal/...` cannot be lifted at all.
//   2. **They must not be able to reach the service's own code.** A demo that
//      verified tokens by calling the same helper the issuer signs with would
//      prove nothing: the round trip a consumer actually makes is over HTTP,
//      against published bytes, with no shared types. Keeping the module
//      boundary makes that structural rather than a matter of discipline.
//
// Standard library only, for the same reason — a reference implementation that
// needs a JWT dependency teaches "pick a library", which is where the
// permissive defaults come from.
module github.com/zed378/zed-auth/demo

go 1.26.5

toolchain go1.26.6

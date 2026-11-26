# PF-14/16/17 — Review of the parallel console work

Date: 2026-11-26. Status: fixes applied, uncommitted; parent tasks unchanged.

The earlier records report component, build and workbench checks. They did not
run the full end-to-end suite or validate the workflow file.

Found and fixed:

- One click on `Review` created the Project Grant, skipping the confirmation
  step. React reused the button node and made it the submit button during the
  click. The two buttons now have distinct keys. jsdom does not reproduce this,
  so the earlier "nothing is sent by Review" test passed; it now also asserts a
  new node, and the end-to-end spec asserts no grant exists after `Review`.
- `.github/workflows/ci.yml` did not parse: two new steps were indented under
  the previous one, so GitHub would have run no job at all.
- Three end-to-end tests failed: the organization switcher had moved out of the
  navigation landmark, and Project Grant `Review` no longer disables itself.
  The shell header is now a `header` landmark and both specs follow the UI.
- Reopening a role or SAML editor kept the values from its first opening. Both
  APIs replace the whole set, so a save could restore a revoked permission or an
  old certificate. A draft is now kept only while the stored record is unchanged.
- Global search could open a result belonging to earlier input on Enter.
- A create form reopened after success still read "Changes saved."

Validation after the fixes: lint and typecheck, 634 console tests in 56 files,
14 workbench checks and 34 end-to-end tests on a fresh local stack all pass.
`scripts/check.sh` was not run. Not deployed to staging.

Left open:

- A save from an editor that stays open while another administrator changes the
  record still replaces that change. The API offers no concurrency check, and
  the earlier console behaved the same way.
- A 403 from the project directory hides authorized role data. This is what the
  PF-16 roles chain specifies; it affects a `PROJECT_OWNER` who administers a
  different organization.
- PF-13, PF-14 and PF-15 code exists, but their statuses, records and chain
  documents do not.

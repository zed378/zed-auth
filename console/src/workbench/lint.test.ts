import { Linter } from "eslint";
// Local ESLint plugins are JavaScript and deliberately share the real rules.
// @ts-expect-error local plugin has no declaration file
import local from "../../eslint-local-rules.js";

const linter = new Linter();
function messages(source: string) {
  return linter.verify(source, [{ plugins: { local }, rules: { "local/no-page-primitive": "error" } }]);
}

it("rejects page-local library primitives in each declaration form", () => {
  for (const source of ["function Button() {}", "const Table = () => null;", "class Modal {}"])
    expect(messages(source)).toHaveLength(1);
});

it("permits page composition and importing library primitives", () => {
  expect(messages('import { Button } from "../components"; function UsersPage() { return Button; }')).toHaveLength(0);
});

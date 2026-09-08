import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach } from "vitest";

// Without this, a component from one test is still mounted during the next.
// The symptom is a `getByRole` finding two matches and a failure that looks
// like it belongs to whichever test happens to run second.
afterEach(() => {
  cleanup();
});

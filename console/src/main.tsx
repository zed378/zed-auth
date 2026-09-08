import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { App } from "./App";
import "./styles/tokens.css";

const container = document.getElementById("root");
if (!container) {
  // A blank page with a console error is the worst version of this. If the
  // mount point is missing the build is broken, and saying so beats silence.
  throw new Error("console: #root is missing from index.html");
}

createRoot(container).render(
  <StrictMode>
    <App />
  </StrictMode>,
);

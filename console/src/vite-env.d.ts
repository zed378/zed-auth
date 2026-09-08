/// <reference types="vite/client" />

interface ImportMetaEnv {
  /**
   * Base URL of the Management API.
   *
   * Build-time rather than runtime: the console is a static bundle, and a
   * runtime-configurable API origin in a static SPA is a way to redirect every
   * bearer token it holds. Empty means same-origin.
   */
  readonly VITE_API_BASE_URL?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}

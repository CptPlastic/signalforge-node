/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_SIGNALFORGE_SOURCE_REPO_URL?: string
  readonly VITE_SIGNALFORGE_SOURCE_URL?: string
  readonly VITE_SIGNALFORGE_LICENSE_URL?: string
  readonly VITE_SIGNALFORGE_FAIR_SOURCE_URL?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}
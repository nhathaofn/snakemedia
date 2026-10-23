declare global {
  interface Window {
    go?: unknown;
    runtime?: unknown;
  }
}

/**
 * True when running inside the Wails webview (or the `wails dev` browser bridge),
 * i.e. when generated Go bindings and runtime functions can be called.
 */
export function isWailsRuntime(): boolean {
  return typeof window !== 'undefined' && window.go !== undefined && window.runtime !== undefined;
}

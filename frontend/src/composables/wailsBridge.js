import * as AppBindings from "../../wailsjs/go/main/App.js";

/**
 * Invokes a Wails-bound Go App method safely (returns undefined outside Wails runtime).
 */
export function callApp(name, ...args) {
  if (typeof window === "undefined" || !window.go?.main?.App?.[name]) {
    return Promise.resolve(undefined);
  }
  const fn = AppBindings[name];
  if (typeof fn !== "function") {
    return Promise.resolve(undefined);
  }
  return fn(...args);
}

export function hasAppMethod(name) {
  return (
    typeof window !== "undefined" &&
    Boolean(window.go?.main?.App?.[name]) &&
    typeof AppBindings[name] === "function"
  );
}

export function onEvent(event, cb) {
  if (typeof window === "undefined") return;
  const rt = window.runtime;
  if (rt?.EventsOn) rt.EventsOn(event, cb);
}

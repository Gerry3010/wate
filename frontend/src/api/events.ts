/**
 * Event delivery, per window.
 *
 * Go can emit to one window (the event then carries that window's name as `sender`) or to the
 * whole app (no sender). Both arrive at every window's listener, so the filtering happens
 * here. Events with no sender are let through deliberately: that is what lets the backend be
 * narrowed one event at a time without the frontend having to change in step.
 */

import { Events, Window } from "@wailsio/runtime";

interface WailsEvent<T> {
  name: string;
  data: T;
  sender?: string;
}

/** This window's Wails name. Empty until initWindowId() has run. */
export let windowId = "";

/**
 * Resolve this window's name. It is an IPC round trip, so it has to finish before the first
 * listener is installed — otherwise early targeted events are tested against an empty id and
 * thrown away.
 */
export async function initWindowId(): Promise<string> {
  try {
    windowId = (await Window.Name()) ?? "";
  } catch (err) {
    console.warn("window name:", err);
    windowId = "";
  }
  return windowId;
}

/** Is an event meant for us? A broadcast (no sender) is meant for everyone. */
export function addressedToMe(sender: string | undefined, me: string): boolean {
  return !sender || sender === me;
}

/** Subscribe to an event addressed to this window, or broadcast to all of them. */
export function onMine<T>(name: string, fn: (data: T) => void): () => void {
  return Events.On(name, (ev: WailsEvent<T>) => {
    if (!addressedToMe(ev.sender, windowId)) return;
    fn(ev.data);
  });
}

/** Subscribe to an event from any window, told which one sent it. */
export function onAny<T>(name: string, fn: (data: T, sender: string) => void): () => void {
  return Events.On(name, (ev: WailsEvent<T>) => fn(ev.data, ev.sender ?? ""));
}

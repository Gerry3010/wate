import { describe, expect, it } from "vitest";
import { statusView } from "./status";

const none = { read: false, write: false, manage: false };

describe("statusView", () => {
  it("says nothing about an ordinary pane but still names it", () => {
    const v = statusView({ openedByAgent: false, command: "zsh", access: none });
    expect(v.origin).toBe("");
    expect(v.status).toBe("");
    expect(v.label).toBe("zsh");
    expect(v.chips).toEqual([]);
    expect(v.chipTitle).toBe("");
  });

  it("falls back to a word when nothing is running", () => {
    expect(statusView({ openedByAgent: false, command: "", access: none }).label).toBe("shell");
  });

  it("marks a pane an agent opened", () => {
    expect(statusView({ openedByAgent: true, command: "npm", access: none }).origin).toBe("agent");
  });

  it("carries the session's status through for the dot", () => {
    expect(statusView({ openedByAgent: false, agentStatus: "waiting", command: "claude", access: none }).status).toBe("waiting");
  });

  it("shows a letter for every right the user handed out, in a fixed order", () => {
    const v = statusView({ openedByAgent: false, command: "zsh", access: { read: true, write: false, manage: true } });
    expect(v.chips).toEqual(["R", "M"]);
    expect(v.chipTitle).toContain("read this pane");
    expect(v.chipTitle).toContain("resize and close");
    expect(v.chipTitle).not.toContain("type into");
  });

  it("spells all three out when everything is open", () => {
    const v = statusView({ openedByAgent: true, command: "tail", access: { read: true, write: true, manage: true } });
    expect(v.chips).toEqual(["R", "W", "M"]);
  });

  it("survives an access record that is not there yet", () => {
    const v = statusView({ openedByAgent: false, command: "zsh", access: undefined as unknown as typeof none });
    expect(v.chips).toEqual([]);
  });
});

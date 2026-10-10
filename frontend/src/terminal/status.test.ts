import { describe, expect, it } from "vitest";
import { afterToggle, statusView, writeMeans } from "./status";

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

  it("says the agent may run things once Read and Write are both on", () => {
    // The two ticks together hand the pane over, Enter and all, so the hover text has to
    // promise more than typing. Write on its own must not.
    const both = statusView({ openedByAgent: false, command: "zsh", access: { read: true, write: true, manage: false } });
    expect(both.chipTitle).toContain("run commands");
    const typing = statusView({ openedByAgent: false, command: "zsh", access: { read: false, write: true, manage: false } });
    expect(typing.chipTitle).toContain("type into");
    expect(typing.chipTitle).not.toContain("run commands");
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

describe("afterToggle", () => {
  it("fills the other two in when Manage goes on", () => {
    expect(afterToggle(none, "manage")).toEqual({ read: true, write: true, manage: true });
  });

  it("leaves them when Manage goes off", () => {
    // Taking away the right to close a pane is no reason to stop reading it. "Revoke all"
    // is there for when it is.
    expect(afterToggle({ read: true, write: true, manage: true }, "manage")).toEqual({
      read: true,
      write: true,
      manage: false,
    });
  });

  it("treats Read and Write as plain switches", () => {
    expect(afterToggle(none, "read")).toEqual({ read: true, write: false, manage: false });
    expect(afterToggle({ read: true, write: false, manage: false }, "read")).toEqual(none);
    // Write never drags Read in with it: reading is the one that costs privacy.
    expect(afterToggle(none, "write")).toEqual({ read: false, write: true, manage: false });
  });
});

describe("writeMeans", () => {
  it("promises only the prompt until Read is on as well", () => {
    expect(writeMeans(none)).toBe("type, no Enter");
    expect(writeMeans({ read: false, write: true, manage: false })).toBe("type, no Enter");
  });

  it("promises the whole pane once Read is on", () => {
    expect(writeMeans({ read: true, write: false, manage: false })).toBe("type and run");
  });
});

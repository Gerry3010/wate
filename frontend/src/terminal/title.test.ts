import { describe, expect, it } from "vitest";
import { paneTitle, promptPath } from "./title";

describe("promptPath", () => {
  it("writes home as ~", () => {
    expect(promptPath("/home/gerry")).toBe("~");
    expect(promptPath("/Users/gerry/src")).toBe("~/src");
  });

  it("keeps short paths as they are", () => {
    expect(promptPath("/usr/local/bin")).toBe("/usr/local/bin");
    expect(promptPath("/home/gerry/Sync-Projekte")).toBe("~/Sync-Projekte");
  });

  it("shortens the parents from the left, keeping the last directory", () => {
    expect(promptPath("/home/gerry/Sync-Projekte/GO-Projekte/wate")).toBe("~/Sy/GO-Projekte/wate");
    expect(promptPath("/home/gerry/Sync-Projekte/GO-Projekte/wate", 14)).toBe("~/Sy/GO/wate");
    expect(promptPath("/home/gerry/.config/some-long-directory/app", 24)).toBe("~/.co/so/app");
  });

  it("falls back to an ellipsis when even that does not fit", () => {
    expect(promptPath("/home/gerry/a/b/c/d/e/f/g/h/i/j/k/project", 12)).toBe("~/…/project");
  });

  it("is empty for an unknown directory", () => {
    expect(promptPath("")).toBe("");
  });
});

describe("paneTitle", () => {
  const cmd = (name: string, host = "") => ({ pane: "p1", name, host, cwd: "/home/gerry" });

  it("names the ssh host while a session is up", () => {
    expect(paneTitle(cmd("ssh", "io-main"), "/home/gerry/src")).toBe("ssh io-main");
  });

  it("shows the path for any other command", () => {
    expect(paneTitle(cmd("vim"), "/home/gerry/src")).toBe("~/src");
    expect(paneTitle(undefined, "/home/gerry/src")).toBe("~/src");
  });

  it("uses the backend's directory when the shell reported none", () => {
    expect(paneTitle(cmd(""), "")).toBe("~");
  });

  it("falls back when nothing is known", () => {
    expect(paneTitle(undefined, "", "wate")).toBe("wate");
  });
});

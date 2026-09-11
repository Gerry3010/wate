import { describe, expect, it } from "vitest";
import { parseOsc52 } from "./osc52";

const b64 = (s: string) => btoa(String.fromCharCode(...new TextEncoder().encode(s)));

describe("parseOsc52", () => {
  it("decodes the clipboard selection", () => {
    expect(parseOsc52(`c;${b64("hello")}`)).toBe("hello");
  });

  it("decodes UTF-8, not just latin-1", () => {
    expect(parseOsc52(`c;${b64("Grüße 👋")}`)).toBe("Grüße 👋");
  });

  it("takes the clipboard selections and the default one", () => {
    expect(parseOsc52(`;${b64("default")}`)).toBe("default");
    expect(parseOsc52(`s0;${b64("buffer")}`)).toBe("buffer");
    expect(parseOsc52(`cs;${b64("both")}`)).toBe("both");
  });

  it("leaves the primary selection and the cut buffers alone", () => {
    expect(parseOsc52(`p;${b64("primary")}`)).toBeNull();
    expect(parseOsc52(`3;${b64("cut buffer")}`)).toBeNull();
  });

  it("never answers a read request", () => {
    expect(parseOsc52("c;?")).toBeNull();
    expect(parseOsc52("c;")).toBeNull();
  });

  it("refuses malformed payloads", () => {
    expect(parseOsc52("nonsense")).toBeNull();
    expect(parseOsc52("c;not base64!!")).toBeNull();
  });

  it("refuses oversized payloads", () => {
    expect(parseOsc52(`c;${"A".repeat(1024 * 1024 + 4)}`)).toBeNull();
  });
});

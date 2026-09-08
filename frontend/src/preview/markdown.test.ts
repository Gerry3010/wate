// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { renderMarkdown } from "./markdown";

describe("renderMarkdown", () => {
  it("renders GFM", () => {
    const html = renderMarkdown("# Title\n\n- [x] done\n\n| a | b |\n|---|---|\n| 1 | 2 |\n\n~~gone~~ `code`");
    expect(html).toContain("<h1>Title</h1>");
    expect(html).toContain("<table>");
    expect(html).toContain("<del>gone</del>");
    expect(html).toContain("<code>code</code>");
    expect(html).toContain('type="checkbox"');
  });
  it("strips scripts and event handlers", () => {
    const html = renderMarkdown('<script>alert(1)</script><img src=x onerror="alert(1)"> <a href="javascript:alert(1)">x</a>');
    expect(html).not.toContain("<script");
    expect(html).not.toContain("onerror");
    expect(html).not.toContain("javascript:");
  });
  it("keeps fenced code language classes", () => {
    expect(renderMarkdown("```go\nfunc main() {}\n```")).toContain('class="language-go"');
  });
});

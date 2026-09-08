import DOMPurify from "dompurify";
import hljs from "highlight.js";
import { Marked } from "marked";

const marked = new Marked({ gfm: true, breaks: false });

/** Markdown → sanitized HTML string. Code blocks are highlighted with highlight.js. */
export function renderMarkdown(src: string): string {
  const html = marked.parse(src, { async: false }) as string;
  return DOMPurify.sanitize(html, {
    USE_PROFILES: { html: true },
    ADD_ATTR: ["target"],
    FORBID_TAGS: ["style", "script", "iframe", "object", "embed", "form"],
  });
}

/** Highlight every <pre><code class="language-x"> under root. */
export function highlightCode(root: HTMLElement) {
  root.querySelectorAll<HTMLElement>("pre code").forEach((el) => {
    if (el.dataset.highlighted) return;
    const lang = /language-([\w+-]+)/.exec(el.className)?.[1];
    if (lang && hljs.getLanguage(lang)) {
      el.innerHTML = hljs.highlight(el.textContent ?? "", { language: lang }).value;
    } else if (!lang) {
      el.innerHTML = hljs.highlightAuto(el.textContent ?? "").value;
    }
    el.dataset.highlighted = "yes";
  });
}

import { HighlightStyle, syntaxHighlighting } from "@codemirror/language";
import { EditorView } from "@codemirror/view";
import { tags as t } from "@lezer/highlight";

/** CodeMirror theme driven entirely by the CSS variables the active yate theme sets. */
export const yateEditorTheme = EditorView.theme(
  {
    "&": { color: "var(--fg)", backgroundColor: "transparent", height: "100%" },
    ".cm-content": { caretColor: "var(--fg)", fontFamily: "var(--editor-font)", fontSize: "var(--editor-font-size)", lineHeight: "var(--editor-line-height)", padding: "8px 0" },
    ".cm-scroller": { fontFamily: "var(--editor-font)", fontSize: "var(--editor-font-size)", lineHeight: "var(--editor-line-height)" },
    "&.cm-focused": { outline: "none" },
    ".cm-cursor, .cm-dropCursor": { borderLeftColor: "var(--fg)" },
    "&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground, .cm-selectionBackground, ::selection": { backgroundColor: "var(--selection) !important" },
    ".cm-activeLine": { backgroundColor: "rgba(255,255,255,0.04)" },
    ".cm-gutters": { backgroundColor: "transparent", color: "var(--tab-inactive-fg)", border: "none", fontFamily: "var(--editor-font)", fontSize: "var(--editor-font-size)" },
    ".cm-activeLineGutter": { backgroundColor: "rgba(255,255,255,0.04)", color: "var(--fg)" },
    ".cm-lineNumbers .cm-gutterElement": { padding: "0 12px 0 16px" },
    ".cm-matchingBracket": { backgroundColor: "color-mix(in srgb, var(--accent) 30%, transparent)", outline: "none" },
    ".cm-panels": { backgroundColor: "var(--tabbar-bg)", color: "var(--fg)" },
    ".cm-panels.cm-panels-bottom": { borderTop: "1px solid var(--border)" },
    ".cm-searchMatch": { backgroundColor: "color-mix(in srgb, var(--yellow) 35%, transparent)" },
    ".cm-searchMatch.cm-searchMatch-selected": { backgroundColor: "color-mix(in srgb, var(--accent) 50%, transparent)" },
    ".cm-textfield": { backgroundColor: "rgba(0,0,0,0.25)", border: "1px solid var(--border)", color: "var(--fg)" },
    ".cm-button": { backgroundImage: "none", backgroundColor: "rgba(255,255,255,0.08)", border: "1px solid var(--border)", color: "var(--fg)" },
    ".cm-tooltip": { backgroundColor: "var(--tabbar-bg)", border: "1px solid var(--border)", color: "var(--fg)" },
  },
  { dark: true },
);

const highlight = HighlightStyle.define([
  { tag: [t.keyword, t.controlKeyword, t.moduleKeyword, t.operatorKeyword], color: "var(--magenta)" },
  { tag: [t.definitionKeyword, t.modifier], color: "var(--magenta)" },
  { tag: [t.string, t.special(t.string), t.character], color: "var(--green)" },
  { tag: [t.number, t.bool, t.null, t.atom, t.literal], color: "var(--yellow)" },
  { tag: [t.comment, t.lineComment, t.blockComment, t.docComment], color: "var(--tab-inactive-fg)", fontStyle: "italic" },
  { tag: [t.function(t.variableName), t.function(t.propertyName), t.labelName], color: "var(--blue)" },
  { tag: [t.typeName, t.className, t.namespace, t.macroName], color: "var(--cyan)" },
  { tag: [t.propertyName, t.attributeName, t.definition(t.variableName)], color: "var(--fg)" },
  { tag: [t.variableName, t.name], color: "var(--fg)" },
  { tag: [t.operator, t.punctuation, t.separator, t.bracket], color: "color-mix(in srgb, var(--fg) 75%, transparent)" },
  { tag: [t.regexp, t.escape], color: "var(--red)" },
  { tag: [t.tagName, t.angleBracket], color: "var(--red)" },
  { tag: [t.heading], color: "var(--accent)", fontWeight: "bold" },
  { tag: [t.heading1], fontSize: "1.25em" },
  { tag: [t.heading2], fontSize: "1.15em" },
  { tag: t.emphasis, fontStyle: "italic" },
  { tag: t.strong, fontWeight: "bold" },
  { tag: t.strikethrough, textDecoration: "line-through" },
  { tag: [t.link, t.url], color: "var(--blue)", textDecoration: "underline" },
  { tag: t.monospace, color: "var(--green)" },
  { tag: t.quote, color: "var(--tab-inactive-fg)" },
  { tag: t.list, color: "var(--accent)" },
  { tag: t.meta, color: "var(--tab-inactive-fg)" },
  { tag: t.invalid, color: "var(--red)", textDecoration: "underline wavy" },
]);

export const yateHighlighting = syntaxHighlighting(highlight);

import { sitePath } from "./site-path.mjs";

export const PRIMARY_NAV = Object.freeze([
  { label: "Quick start", href: sitePath("start"), key: "start" },
  { label: "Pipeline", href: sitePath("pipeline"), key: "pipeline" },
  { label: "CLI", href: sitePath("cli"), key: "cli" },
  { label: "Config", href: sitePath("config"), key: "config" },
  { label: "Design", href: sitePath("design"), key: "design" },
]);

export const DOCS_NAV = Object.freeze([
  {
    section: "start",
    items: [
      { label: "Quick start", href: sitePath("start"), key: "start" },
      { label: "With consigliere", href: sitePath("start/consigliere"), key: "consigliere" },
    ],
  },
  {
    section: "docs",
    items: [
      { label: "Pipeline and gates", href: sitePath("pipeline"), key: "pipeline" },
      { label: "Daemon", href: sitePath("daemon"), key: "daemon" },
      { label: "Evidence", href: sitePath("evidence"), key: "evidence" },
      { label: "Cursor Cloud", href: sitePath("cursor"), key: "cursor" },
      { label: "Configuration", href: sitePath("config"), key: "config" },
    ],
  },
  {
    section: "reference",
    items: [
      { label: "CLI", href: sitePath("cli"), key: "cli" },
      { label: "Changelog", href: sitePath("changelog"), key: "changelog" },
      { label: "Design system", href: sitePath("design"), key: "design" },
    ],
  },
]);

export const DOCS_ORDER = Object.freeze(DOCS_NAV.flatMap((group) => group.items));

const DOCS_BY_KEY = new Map(
  DOCS_ORDER.map((item) => {
    const group = DOCS_NAV.find((candidate) => candidate.items.some((entry) => entry.key === item.key));
    return [item.key, { ...item, section: group?.section ?? "" }];
  }),
);

export function docsPage(key) {
  return DOCS_BY_KEY.get(key);
}

export function pagerFor(key) {
  const index = DOCS_ORDER.findIndex((item) => item.key === key);
  return {
    prev: index > 0 ? DOCS_ORDER[index - 1] : undefined,
    next: index >= 0 && index < DOCS_ORDER.length - 1 ? DOCS_ORDER[index + 1] : undefined,
  };
}

export function primaryNavKey(pathname) {
  const normalized = pathname.endsWith("/") ? pathname : `${pathname}/`;
  const match = PRIMARY_NAV.find((item) => item.href === normalized);
  return match?.key;
}

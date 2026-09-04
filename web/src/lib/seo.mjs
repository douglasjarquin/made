import astroConfig from "../../astro.config.mjs";

export const SITE_ORIGIN = astroConfig.site;
export const SITE_BASE = `${astroConfig.base.replace(/\/+$/, "")}/`;
export const PRODUCT_NAME = "made";
export const PRODUCT_DESCRIPTION =
  "Automated review, tests, docs, lint, push, and CI. A Go rewrite of no-mistakes' validation-gate pipeline, built to pair with consigliere and herdr.";
export const REPOSITORY_URL = "https://github.com/douglasjarquin/made";
export const PRODUCT_OPERATING_SYSTEM = "Linux, macOS";
export const CONSIGLIERE_URL = "https://github.com/douglasjarquin/consigliere";
export const HERDR_URL = "https://github.com/douglasjarquin/herdr";
export const NO_MISTAKES_URL = "https://github.com/kunchenguid/no-mistakes";

export const INDEXABLE_PATHS = Object.freeze([
  SITE_BASE,
  `${SITE_BASE}start/`,
  `${SITE_BASE}start/consigliere/`,
  `${SITE_BASE}pipeline/`,
  `${SITE_BASE}daemon/`,
  `${SITE_BASE}evidence/`,
  `${SITE_BASE}cursor/`,
  `${SITE_BASE}config/`,
  `${SITE_BASE}cli/`,
  `${SITE_BASE}changelog/`,
  `${SITE_BASE}design/`,
]);

export function withTrailingSlash(pathname) {
  const withoutIndex = pathname.replace(/\/index\.html$/, "/");
  return withoutIndex.endsWith("/") ? withoutIndex : `${withoutIndex}/`;
}

export function canonicalUrl(pathname) {
  return new URL(withTrailingSlash(pathname), SITE_ORIGIN).href;
}

export function absoluteUrl(pathname) {
  return new URL(pathname, SITE_ORIGIN).href;
}

export function sitemapLocation() {
  return absoluteUrl(`${SITE_BASE}sitemap.xml`);
}

export function robotsTxt() {
  return `User-agent: *\nAllow: ${SITE_BASE}\n\nSitemap: ${sitemapLocation()}\n`;
}

export function sitemapXml() {
  const entries = INDEXABLE_PATHS.map(
    (pathname) => `  <url>\n    <loc>${canonicalUrl(pathname)}</loc>\n  </url>`,
  ).join("\n");
  return `<?xml version="1.0" encoding="UTF-8"?>\n<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">\n${entries}\n</urlset>\n`;
}

export function llmsTxt() {
  const liveUrl = canonicalUrl(SITE_BASE);
  const fence = "```";
  return `# made

> ${PRODUCT_DESCRIPTION}

made is a Go CLI and daemon that runs a validation-gate pipeline: review, tests, docs, lint, push, and CI. It is an independent synthesis of no-mistakes, built to pair with consigliere and herdr.

Live site: ${liveUrl}
Repository: ${REPOSITORY_URL}

Every command has a --json form with a fixed schema. Run ids are exact.

${fence}
made capabilities --json
made doctor --json
made verify --json $(git rev-parse HEAD)
${fence}

## Pages

- [Home](${liveUrl})
- [Quick start](${canonicalUrl(`${SITE_BASE}start/`)})
- [With consigliere](${canonicalUrl(`${SITE_BASE}start/consigliere/`)})
- [Pipeline and gates](${canonicalUrl(`${SITE_BASE}pipeline/`)})
- [Daemon](${canonicalUrl(`${SITE_BASE}daemon/`)})
- [Evidence](${canonicalUrl(`${SITE_BASE}evidence/`)})
- [Cursor Cloud](${canonicalUrl(`${SITE_BASE}cursor/`)})
- [Configuration](${canonicalUrl(`${SITE_BASE}config/`)})
- [CLI](${canonicalUrl(`${SITE_BASE}cli/`)})
- [Changelog](${canonicalUrl(`${SITE_BASE}changelog/`)})
- [Design system](${canonicalUrl(`${SITE_BASE}design/`)})
`;
}

export function productJsonLd() {
  const url = canonicalUrl(SITE_BASE);
  return {
    "@context": "https://schema.org",
    "@graph": [
      {
        "@type": "WebSite",
        name: PRODUCT_NAME,
        url,
        description: PRODUCT_DESCRIPTION,
      },
      {
        "@type": "SoftwareApplication",
        name: PRODUCT_NAME,
        url,
        description: PRODUCT_DESCRIPTION,
        applicationCategory: "DeveloperApplication",
        operatingSystem: PRODUCT_OPERATING_SYSTEM,
        downloadUrl: REPOSITORY_URL,
        codeRepository: REPOSITORY_URL,
      },
    ],
  };
}

# STATIC ASTRO DOCS SITE

The repository-level guidance in `../AGENTS.md` still applies. This file records only web-specific seams.

## DEPLOYMENT AND ROUTING

- Keep `astro.config.mjs` on static output with the `/made` GitHub Pages base and `https://douglasjarquin.github.io`.
- Build every internal page and asset URL with `sitePath()` so local preview, Playwright, and GitHub Pages retain the same trailing-slash base contract.
- Use relative route URLs against Playwright's configured base URL, and assert `/made/.../` when the prefix itself is part of the behavior under test.
- Treat `web/dist/` as the disposable static package consumed by preview and the Pages artifact upload.

## SOURCE OWNERSHIP

- `src/layouts/SiteLayout.astro` owns the document shell, metadata, theme bootstrap, `SiteHeader`, `SiteFooter`, and the import of `global.css`.
- `src/layouts/DocsLayout.astro` owns the docs grid, sidebar, breadcrumb, and pager. Home is the only route that skips it.
- Keep shared tokens and chrome in `src/styles/global.css`. Route files own page composition.
- `src/lib/nav.mjs` is the source of truth for primary nav, docs sidebar, and pager order.
- `src/lib/seo.mjs` generates robots.txt, sitemap.xml, llms.txt, and JSON-LD. Committed files under `public/` must match those functions.

## AUBE AND MISE BOUNDARIES

- From the repository root, use the `mise run web:*` tasks so Node 24 and Aube 2.2.4 come from the pinned toolchain.
- From `web/`, use `aube run <script>` or `aube -C web ...` from the root.
- Keep `web/.npmrc` on `node-linker=hoisted` so Astro prerender can resolve native bindings.
- `mise run web:dev:local` serves the site at `https://made.test` through portless. Set `ASTRO_DEV_BACKGROUND=1` so Astro 7 does not daemonize out from under portless.

## TEST SURFACES

- `tests/unit/*.test.mjs` are Node tests for config and SEO without a browser.
- `tests/e2e/*.spec.mjs` are Playwright tests against a production build served under `/made/` in desktop Chromium.
- Playwright builds and starts its own Astro preview on `PLAYWRIGHT_PORT` (default `4321`) with `--ignore-lock`.

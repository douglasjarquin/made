import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import {
  INDEXABLE_PATHS,
  PRODUCT_DESCRIPTION,
  PRODUCT_NAME,
  PRODUCT_OPERATING_SYSTEM,
  REPOSITORY_URL,
  SITE_BASE,
  SITE_ORIGIN,
  canonicalUrl,
  llmsTxt,
  productJsonLd,
  robotsTxt,
  sitemapLocation,
  sitemapXml,
  withTrailingSlash,
} from "../../src/lib/seo.mjs";

const publicDirectory = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../public");

const indexableUrls = [
  "https://douglasjarquin.github.io/made/",
  "https://douglasjarquin.github.io/made/start/",
  "https://douglasjarquin.github.io/made/start/consigliere/",
  "https://douglasjarquin.github.io/made/pipeline/",
  "https://douglasjarquin.github.io/made/daemon/",
  "https://douglasjarquin.github.io/made/evidence/",
  "https://douglasjarquin.github.io/made/cursor/",
  "https://douglasjarquin.github.io/made/config/",
  "https://douglasjarquin.github.io/made/cli/",
  "https://douglasjarquin.github.io/made/changelog/",
  "https://douglasjarquin.github.io/made/design/",
];

test("canonical URLs are HTTPS trailing-slash addresses without index.html", () => {
  assert.equal(SITE_ORIGIN, "https://douglasjarquin.github.io");
  assert.equal(SITE_BASE, "/made/");
  assert.equal(withTrailingSlash("/made/index.html"), "/made/");
  assert.equal(withTrailingSlash("/made/start"), "/made/start/");
  assert.equal(canonicalUrl("/made/index.html"), "https://douglasjarquin.github.io/made/");
  assert.equal(
    canonicalUrl("/made/start/index.html"),
    "https://douglasjarquin.github.io/made/start/",
  );
});

test("robots.txt allows the project path and points at the project sitemap", () => {
  const committed = readFileSync(path.join(publicDirectory, "robots.txt"), "utf8");
  assert.equal(committed, robotsTxt());
  assert.match(committed, /^User-agent: \*\nAllow: \/made\/\n\nSitemap: /);
  assert.equal(sitemapLocation(), "https://douglasjarquin.github.io/made/sitemap.xml");
  assert.doesNotMatch(committed, /Allow: \/\n/);
});

test("sitemap.xml lists every indexable trailing-slash URL", () => {
  const committed = readFileSync(path.join(publicDirectory, "sitemap.xml"), "utf8");
  assert.equal(committed, sitemapXml());
  assert.deepEqual(
    INDEXABLE_PATHS.map((pathname) => canonicalUrl(pathname)),
    indexableUrls,
  );
});

test("llms.txt names the CLI, live site, and repository", () => {
  const committed = readFileSync(path.join(publicDirectory, "llms.txt"), "utf8");
  assert.equal(committed, llmsTxt());
  assert.match(committed, /^# made\n/);
  assert.match(committed, /Go CLI and daemon/);
  assert.match(committed, /Live site: https:\/\/douglasjarquin\.github\.io\/made\//);
  assert.match(committed, /Repository: https:\/\/github\.com\/douglasjarquin\/made/);
});

test("JSON-LD describes the visible site and software without ratings or offers", () => {
  const jsonLd = productJsonLd();
  const serialized = JSON.stringify(jsonLd);
  assert.equal(jsonLd["@context"], "https://schema.org");
  assert.deepEqual(
    jsonLd["@graph"].map((node) => node["@type"]),
    ["WebSite", "SoftwareApplication"],
  );
  const [website, software] = jsonLd["@graph"];
  for (const node of jsonLd["@graph"]) {
    assert.equal(node.name, PRODUCT_NAME);
    assert.equal(node.description, PRODUCT_DESCRIPTION);
    assert.equal(node.url, "https://douglasjarquin.github.io/made/");
    assert.equal("aggregateRating" in node, false);
    assert.equal("offers" in node, false);
  }
  assert.equal("operatingSystem" in website, false);
  assert.equal("downloadUrl" in website, false);
  assert.equal("codeRepository" in website, false);
  assert.equal(software.operatingSystem, PRODUCT_OPERATING_SYSTEM);
  assert.equal(software.downloadUrl, REPOSITORY_URL);
  assert.equal(software.codeRepository, REPOSITORY_URL);
  assert.doesNotMatch(serialized, /aggregateRating|offers|"ratingValue"/);
});

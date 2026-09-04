import { expect, test } from "@playwright/test";

const origin = "https://douglasjarquin.github.io";
const routes = [
  {
    path: "./",
    title: "Turn code into green PRs | made",
    description:
      "Automated review, tests, docs, lint, push, and CI. A Go rewrite of no-mistakes' validation-gate pipeline, built to pair with consigliere and herdr.",
    canonical: `${origin}/made/`,
    heading: "Turn code into green PRs.",
  },
  {
    path: "start/",
    title: "Quick start | made",
    description:
      "Install the binary, add a config, verify one commit. Wire it to consigliere once that feels boring.",
    canonical: `${origin}/made/start/`,
    heading: "Quick start",
  },
  {
    path: "start/consigliere/",
    title: "With consigliere | made",
    description:
      "Consigliere decides what to build and who builds it. made decides whether it is ready to become a PR. Neither does the other's job.",
    canonical: `${origin}/made/start/consigliere/`,
    heading: "With consigliere",
  },
  {
    path: "pipeline/",
    title: "Pipeline and gates | made",
    description:
      "A gate is the ordered set of checks a commit must pass before made pushes, opens a PR, and watches CI. A run is one commit walking one gate.",
    canonical: `${origin}/made/pipeline/`,
    heading: "Pipeline and gates",
  },
  {
    path: "daemon/",
    title: "Daemon | made",
    description: "One process per machine, one Unix socket, and a write-ahead log that survives a crash.",
    canonical: `${origin}/made/daemon/`,
    heading: "Daemon",
  },
  {
    path: "evidence/",
    title: "Evidence | made",
    description:
      "When a run fails, made says which check, which workflow run, how many reruns it spent, and where to look. Bounded, by design.",
    canonical: `${origin}/made/evidence/`,
    heading: "Evidence",
  },
  {
    path: "cursor/",
    title: "Cursor Cloud | made",
    description:
      "Project a reviewer and a skill from trusted config, so a Cloud agent can verify commits with made and no daemon.",
    canonical: `${origin}/made/cursor/`,
    heading: "Cursor Cloud",
  },
  {
    path: "config/",
    title: "Configuration | made",
    description: "Two equally valid homes for one file. Pick either; never both.",
    canonical: `${origin}/made/config/`,
    heading: "Configuration",
  },
  {
    path: "cli/",
    title: "CLI | made",
    description:
      "Every command has a --json form with a fixed schema. Run ids are exact; nothing is fuzzy-matched.",
    canonical: `${origin}/made/cli/`,
    heading: "CLI",
  },
  {
    path: "changelog/",
    title: "Changelog | made",
    description: "Milestones the README records, from first-class config paths through the initial rewrite.",
    canonical: `${origin}/made/changelog/`,
    heading: "Changelog",
  },
  {
    path: "design/",
    title: "Design system | made",
    description:
      "Cleaner and greener. One serif, one mono, one green, a lot of air. Light is home; dark is a switch.",
    canonical: `${origin}/made/design/`,
    heading: "Design system",
  },
];

test("JSON-LD is in the static HTML, not injected after render", async ({ request }) => {
  const html = await (await request.get("/made/")).text();
  const jsonLd = html.match(
    /<script type="application\/ld\+json"[^>]*>([\s\S]*?)<\/script>/,
  );

  expect(jsonLd?.[1]).toBeTruthy();
  const parsed = JSON.parse(jsonLd[1]);
  expect(parsed["@graph"].map((node) => node["@type"])).toEqual([
    "WebSite",
    "SoftwareApplication",
  ]);
});

test("robots.txt allows the project path and names the sitemap", async ({ request }) => {
  const response = await request.get("/made/robots.txt");
  const body = await response.text();

  expect(response.ok()).toBe(true);
  expect(body).toBe(
    "User-agent: *\nAllow: /made/\n\nSitemap: https://douglasjarquin.github.io/made/sitemap.xml\n",
  );
});

test("llms.txt summarizes the CLI, live site, and repository", async ({ request }) => {
  const response = await request.get("/made/llms.txt");
  const body = await response.text();

  expect(response.ok()).toBe(true);
  expect(body).toContain("made is a Go CLI and daemon");
  expect(body).toContain("Live site: https://douglasjarquin.github.io/made/");
  expect(body).toContain("Repository: https://github.com/douglasjarquin/made");
});

test("the project 404 page is branded, linked, and noindexed", async ({ page, request }) => {
  const missing = await request.get("/made/this-path-is-not-a-route/");
  expect(missing.status()).toBe(404);

  await page.goto("404.html");

  await expect(page).toHaveTitle("Page not found | made");
  await expect(page.locator('meta[name="robots"]')).toHaveAttribute("content", "noindex");
  await expect(page.locator('script[type="application/ld+json"]')).toHaveCount(0);
  await expect(page.getByTestId("project-404")).toContainText("No run, page, or gate by that name.");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Not found.");
  await expect(page.getByRole("link", { name: "Install, configure, verify →" })).toHaveAttribute(
    "href",
    "/made/start/",
  );
});

test("sitemap.xml lists every indexable trailing-slash URL", async ({ request }) => {
  const response = await request.get("/made/sitemap.xml");
  const body = await response.text();
  const locations = [...body.matchAll(/<loc>([^<]+)<\/loc>/g)].map((match) => match[1]);

  expect(response.ok()).toBe(true);
  expect(locations).toEqual(routes.map((route) => route.canonical));
  expect(locations.every((location) => location.endsWith("/"))).toBe(true);
});

test("every indexable route has unique metadata, a self-canonical, and JSON-LD", async ({ page }) => {
  const titles = [];
  const descriptions = [];

  for (const route of routes) {
    await page.goto(route.path);

    await expect(page).toHaveTitle(route.title);
    await expect(page.locator('meta[name="description"]')).toHaveAttribute(
      "content",
      route.description,
    );
    await expect(page.locator('link[rel="canonical"]')).toHaveAttribute("href", route.canonical);
    await expect(page.getByRole("heading", { level: 1 })).toHaveText(route.heading);

    const jsonLd = JSON.parse(
      await page.locator('script[type="application/ld+json"]').innerText(),
    );
    expect(jsonLd["@graph"].map((node) => node["@type"])).toEqual([
      "WebSite",
      "SoftwareApplication",
    ]);
    expect(JSON.stringify(jsonLd)).not.toMatch(/aggregateRating|"offers"|ratingValue/);

    await expect(page.locator('meta[property="og:title"]')).toHaveAttribute("content", route.title);
    await expect(page.locator('meta[property="og:url"]')).toHaveAttribute("content", route.canonical);
    await expect(page.locator('meta[property="og:image"]')).toHaveCount(0);
    await expect(page.locator('meta[name="twitter:card"]')).toHaveAttribute("content", "summary");
    await expect(page.locator('link[rel="icon"]')).toHaveCount(0);

    titles.push(route.title);
    descriptions.push(route.description);
  }

  expect(new Set(titles).size).toBe(routes.length);
  expect(new Set(descriptions).size).toBe(routes.length);
});

test("index.html duplicates canonicalise to the trailing-slash home URL", async ({ page }) => {
  await page.goto("index.html");

  await expect(page.locator('link[rel="canonical"]')).toHaveAttribute(
    "href",
    "https://douglasjarquin.github.io/made/",
  );
});

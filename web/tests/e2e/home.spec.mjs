import { expect, test } from "@playwright/test";

test("the home hero names the product and the two primary actions", async ({ page }) => {
  await page.goto("./");
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Turn code into green PRs.");
  await expect(page.getByRole("link", { name: "Get started" })).toHaveAttribute("href", "/made/start/");
  await expect(page.getByRole("link", { name: "How the gate works" })).toHaveAttribute(
    "href",
    "/made/pipeline/",
  );
  await expect(page.locator(".chip--active")).toHaveText("succeeded");
});

test("the home remains useful without client JavaScript", async ({ browser }) => {
  const context = await browser.newContext({
    baseURL: `http://127.0.0.1:${process.env.PLAYWRIGHT_PORT ?? "4321"}/made/`,
    javaScriptEnabled: false,
  });
  const page = await context.newPage();

  try {
    await page.goto("./");
    await expect(page.getByRole("heading", { level: 1 })).toHaveText("Turn code into green PRs.");
    await expect(page.getByRole("link", { name: "Get started" })).toBeVisible();
  } finally {
    await context.close();
  }
});

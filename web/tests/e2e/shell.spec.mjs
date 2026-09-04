import { expect, test } from "@playwright/test";

const routes = [
  { label: "Quick start", pathname: "/made/start/" },
  { label: "Pipeline", pathname: "/made/pipeline/" },
  { label: "CLI", pathname: "/made/cli/" },
  { label: "Config", pathname: "/made/config/" },
  { label: "Design", pathname: "/made/design/" },
];

const viewports = [
  { name: "desktop", width: 1440, height: 900 },
  { name: "mobile", width: 375, height: 844 },
];

for (const viewport of viewports) {
  test(`shared shell works at ${viewport.name} size`, async ({ page }) => {
    await page.setViewportSize(viewport);
    await page.goto("./");

    await page.keyboard.press("Tab");
    const focusedLink = page.locator(":focus-visible");
    await expect(focusedLink).toHaveAttribute("href", "#main-content");
    await expect(focusedLink).toHaveText("Skip to main content");
    await expect
      .poll(() =>
        page.evaluate(
          () => document.documentElement.scrollWidth <= document.documentElement.clientWidth,
        ),
      )
      .toBe(true);

    if (viewport.name === "mobile") {
      await page.getByRole("button", { name: "Menu" }).click();
    }

    for (const route of routes) {
      await page.getByRole("navigation", { name: "Primary" }).getByRole("link", {
        name: route.label,
        exact: true,
      }).click();

      await expect(page).toHaveURL(new RegExp(`${route.pathname.replaceAll("/", "\\/")}$`));
      const activeLinks = page.locator('[data-nav-link][aria-current="page"]');
      await expect(activeLinks).toHaveCount(1);
      await expect(activeLinks).toHaveText(route.label);
      await expect
        .poll(() =>
          page.evaluate(
            () => document.documentElement.scrollWidth <= document.documentElement.clientWidth,
          ),
        )
        .toBe(true);

      if (viewport.name === "mobile" && route !== routes.at(-1)) {
        await page.getByRole("button", { name: "Menu" }).click();
      }
    }
  });
}

test("theme toggle persists light and dark on the document", async ({ page }) => {
  await page.goto("./");
  const toggle = page.getByRole("button", { name: "Dark" });
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
  await toggle.click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await expect(page.getByRole("button", { name: "Light" })).toHaveCount(1);
  await expect.poll(() => page.evaluate(() => localStorage.getItem("made-theme"))).toBe("dark");
  await page.getByRole("button", { name: "Light" }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
});

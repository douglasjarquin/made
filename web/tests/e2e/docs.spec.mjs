import { expect, test } from "@playwright/test";

test("docs pages share the sidebar, breadcrumb, and pager", async ({ page }) => {
  await page.goto("start/");

  const sidebar = page.getByRole("navigation", { name: "Docs" }).first();
  await expect(sidebar.getByRole("link", { name: "Quick start" })).toHaveAttribute(
    "aria-current",
    "page",
  );
  await expect(page.getByRole("navigation", { name: "Breadcrumb" })).toContainText("Quick start");
  await expect(page.getByRole("navigation", { name: "Docs pager" }).getByRole("link", { name: /With consigliere/ })).toHaveAttribute(
    "href",
    "/made/start/consigliere/",
  );

  await sidebar.getByRole("link", { name: "Daemon" }).click();
  await expect(page).toHaveURL(/\/made\/daemon\/$/);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText("Daemon");
  await expect(page.getByRole("navigation", { name: "Docs pager" }).getByRole("link", { name: /previous/ })).toContainText(
    "Pipeline and gates",
  );
  await expect(page.getByRole("navigation", { name: "Docs pager" }).getByRole("link", { name: /next/ })).toContainText(
    "Evidence",
  );
});

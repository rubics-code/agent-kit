import { expect, test } from "@playwright/test";

test("connect an agent on another origin, then talk to it", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("link", { name: "Connect an agent" }).click();
  const dialog = page.getByRole("dialog", { name: "Connect an agent" });
  await expect(dialog.getByText(/asks for none/)).toBeVisible();
  await expect(dialog.getByLabel(/key/i)).toHaveCount(0);

  await dialog.getByLabel("Address").fill("127.0.0.1:8097");
  await dialog.getByRole("button", { name: "Check" }).click();
  await expect(dialog.getByLabel("Agent card").getByText("helper")).toBeVisible();
  await expect(dialog.getByText(/It sends Kit cards/)).toBeVisible();
  await expect(dialog.getByText(/Test call: .* OK · “Hello\./)).toBeVisible();
  await dialog.getByRole("button", { name: "Connect helper" }).click();

  const agents = page.getByRole("navigation", { name: "Agents" });
  await expect(agents.getByText("helper")).toBeVisible();
  await page.getByLabel("Message helper").fill("say hello");
  await page.getByLabel("Message helper").press("Enter");
  await expect(page.getByText("Hello. I am the dev agent, and I run on the fake model.")).toBeVisible();

  const stored = await page.evaluate(() => localStorage.getItem("console.agents.v1") ?? "");
  expect(stored).toContain("127.0.0.1:8097");
  expect(stored.toLowerCase()).not.toMatch(/key|token|secret|password/);
});

test("a bad address shows an error", async ({ page }) => {
  await page.goto("/#/connect");
  const dialog = page.getByRole("dialog", { name: "Connect an agent" });
  await dialog.getByLabel("Address").fill("127.0.0.1:1");
  await dialog.getByRole("button", { name: "Check" }).click();
  await expect(dialog.getByRole("alert")).toBeVisible();
  await expect(dialog.getByRole("button", { name: /^Connect / })).toBeDisabled();
});

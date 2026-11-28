import { expect, test } from "@playwright/test";

test("an agent puts a card on the dashboard from the ask bar", async ({ page }) => {
  await page.goto("/#/dashboard");
  await expect(page.getByRole("heading", { name: "Dashboard" })).toBeVisible();
  await page.getByLabel("Ask for a card").fill("Make a card called Shopping with the items: milk, eggs, bread.");
  await page.getByRole("button", { name: "Ask" }).click();
  await expect(page.getByText("devagent: The card is on the dashboard.")).toBeVisible();

  const card = page.getByRole("region", { name: "Shopping" });
  await expect(card.getByText("eggs")).toBeVisible();
  await expect(card.getByText("Updated just now")).toBeVisible();
  await expect(page.getByText(/Last change just now: devagent (added|updated) “Shopping”/)).toBeVisible();
  await expect(page.getByRole("link", { name: /Dashboard \d+ cards? · updated just now/ })).toBeVisible();

  await card.getByRole("button", { name: "Remove Shopping" }).click();
  await expect(card).toHaveCount(0);
});

test("a person adds a card from a thread", async ({ page }) => {
  await page.goto("/");
  await page.getByLabel("Message devagent").fill("Write a note called Plants with the text: water on Friday.");
  await page.getByLabel("Message devagent").press("Enter");
  await page.getByRole("region", { name: "Approval: write_note" }).getByRole("button", { name: "Approve" }).click();
  await expect(page.getByText('The note "Plants" is written.')).toBeVisible();

  await page.getByLabel("Message devagent").fill("What notes do I have?");
  await page.getByLabel("Message devagent").press("Enter");
  const notes = page.getByRole("region", { name: "Notes" }).last();
  await expect(notes.getByRole("cell", { name: "water on Friday" })).toBeVisible();
  await notes.getByRole("button", { name: "Add to dashboard" }).click();
  await expect(notes.getByText("On the dashboard")).toBeVisible();

  await page.getByRole("link", { name: /^Dashboard/ }).click();
  const card = page.getByRole("region", { name: "Notes" }).first();
  await expect(card.getByRole("cell", { name: "Plants" })).toBeVisible();
  await expect(page.getByText(/a person added “Notes”/)).toBeVisible();
  await card.getByRole("link", { name: /From the thread/ }).click();
  await expect(page.getByText('The note "Plants" is written.')).toBeVisible();
});

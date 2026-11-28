import { expect, test, type Page } from "@playwright/test";

async function say(page: Page, text: string) {
  const box = page.getByLabel("Message devagent");
  await box.fill(text);
  await box.press("Enter");
}

test.beforeEach(async ({ page }) => {
  await page.goto("/");
  await expect(page.getByRole("navigation", { name: "Agents" }).getByText("devagent")).toBeVisible();
});

test("a reply streams in", async ({ page }) => {
  await say(page, "say hello");
  await expect(page.getByText("Hello. I am the dev agent, and I run on the fake model.")).toBeVisible();
  await expect(page.getByRole("navigation", { name: "Threads with devagent" }).getByText("say hello")).toBeVisible();
});

test("a write waits for approval, and Approve writes it", async ({ page }) => {
  await say(page, "Write a note called Groceries with the text: milk, eggs.");
  const card = page.getByRole("region", { name: "Approval: write_note" });
  await expect(card.getByText('Write the note "Groceries" (10 characters): "milk, eggs"')).toBeVisible();
  await expect(card.getByText("The model did not write it.")).toBeVisible();
  await card.getByRole("button", { name: "Approve" }).click();
  await expect(page.getByText('The note "Groceries" is written.')).toBeVisible();
  await expect(card.getByText(/^Approved · /)).toBeVisible();
  await expect(page.getByText(/write_note ✓/)).toBeVisible();

  await say(page, "What notes do I have?");
  await expect(page.getByText(/Your notes: .*Groceries/)).toBeVisible();
});

test("Reject does not write", async ({ page }) => {
  await say(page, "Write a note called Secret with the text: do not keep.");
  const card = page.getByRole("region", { name: "Approval: write_note" });
  await card.getByRole("button", { name: "Reject" }).click();
  await expect(page.getByText("You rejected the write, so I did not write the note.")).toBeVisible();
  await expect(card.getByText(/^Rejected · /)).toBeVisible();
  await expect(page.getByText("write_note · rejected")).toBeVisible();
});

test("a thread stays after a reload", async ({ page }) => {
  await say(page, "say hello again");
  await expect(page.getByText(/I run on the fake model/)).toBeVisible();
  await page.reload();
  await expect(page.getByText("say hello again").first()).toBeVisible();
  await expect(page.getByText(/I run on the fake model/)).toBeVisible();
});

test("on a narrow screen the sidebar opens from the menu", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 800 });
  const side = page.getByRole("navigation", { name: "Agents" });
  await expect(side).not.toBeInViewport();
  await page.getByRole("button", { name: "Open the sidebar" }).click();
  await expect(side).toBeInViewport();
});

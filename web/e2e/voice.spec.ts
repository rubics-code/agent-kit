import { expect, test } from "@playwright/test";

// The microphone is a tone from an oscillator, so the test runs the same on
// every machine. The recorder, the WAV encoder and the upload are real. The
// dev agent with -fake checks that the upload is a WAV file with audio, and
// gives fixed text.
test.beforeEach(async ({ page }) => {
  await page.addInitScript(() => {
    navigator.mediaDevices.getUserMedia = async () => {
      const ctx = new AudioContext();
      const osc = ctx.createOscillator();
      const dest = ctx.createMediaStreamDestination();
      osc.connect(dest);
      osc.start();
      return dest.stream;
    };
  });
});
test("hold to talk sends the words as a voice message", async ({ page }) => {
  await page.goto("/");
  const talk = page.getByRole("button", { name: "Hold to talk to devagent" });
  const upload = page.waitForRequest((r) => r.url().endsWith("/kit/transcribe") && r.method() === "POST");
  await talk.hover();
  await page.mouse.down();
  await expect(page.getByRole("status", { name: "Recording" })).toBeVisible();
  await page.waitForTimeout(1200);
  await page.mouse.up();

  const req = await upload;
  expect(req.headers()["content-type"]).toBe("audio/wav");
  expect(req.postDataBuffer()?.subarray(0, 4).toString()).toBe("RIFF");
  await expect(page.getByText(/You · voice ·/)).toBeVisible();
  await expect(page.getByText("Hello. I am the dev agent, and I run on the fake model.")).toBeVisible();
});

test("a short press does not send", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "Hold to talk to devagent" }).click();
  await expect(page.getByText("Hold the button while you talk.")).toBeVisible();
});

test("the space bar in an empty box records", async ({ page }) => {
  await page.goto("/");
  const box = page.getByLabel("Message devagent");
  await box.focus();
  await page.keyboard.down(" ");
  await expect(page.getByRole("status", { name: "Recording" })).toBeVisible();
  await page.waitForTimeout(800);
  await page.keyboard.press("Escape");
  await expect(page.getByRole("status", { name: "Recording" })).toHaveCount(0);
  await page.keyboard.up(" ");
});

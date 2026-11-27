import { describe, expect, it } from "vitest";
import { ServiceError } from "../src/index.ts";
import { openRouterTranscriber } from "../src/index.ts";

describe("openRouterTranscriber", () => {
  it("sends the WAV as input_audio and returns the text", async () => {
    let sent: { model?: string; messages?: { content: { type: string; input_audio?: { data: string; format: string } }[] }[] } = {};
    const f = (async (_url: string | URL | Request, init?: RequestInit) => {
      sent = JSON.parse(String(init?.body));
      return Response.json({ choices: [{ message: { content: " list my keys \n" } }] });
    }) as typeof fetch;

    const text = await openRouterTranscriber("k", { model: "m", fetch: f })(Buffer.from("RIFFxxxxWAVE"));

    expect(text).toBe("list my keys");
    expect(sent.model).toBe("m");
    const audio = sent.messages![0]!.content.find((c) => c.type === "input_audio")!.input_audio!;
    expect(audio.format).toBe("wav");
    expect(Buffer.from(audio.data, "base64").toString()).toBe("RIFFxxxxWAVE");
  });

  it("gives a ServiceError when OpenRouter fails", async () => {
    const f = (async () => Response.json({ error: { message: "no credits" } }, { status: 402 })) as typeof fetch;
    const err = await openRouterTranscriber("k", { fetch: f })(Buffer.alloc(1)).catch((e: unknown) => e);
    expect(err).toBeInstanceOf(ServiceError);
    expect((err as Error).message).toContain("no credits");
  });

  it("gives no text when the model marks the audio as no speech", async () => {
    for (const reply of ["<noise>", "[silence]", "(no speech)"]) {
      const f = (async () => Response.json({ choices: [{ message: { content: reply } }] })) as typeof fetch;
      expect(await openRouterTranscriber("k", { fetch: f })(Buffer.alloc(1))).toBe("");
    }
  });
});

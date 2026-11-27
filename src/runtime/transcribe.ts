import { OPENROUTER_BASE_URL } from "./agent.ts";
import { ServiceError } from "../errors.ts";

// A cheap audio model on OpenRouter: about $0.0001 for a short message.
export const DEFAULT_TRANSCRIBE_MODEL = "google/gemini-3.5-flash-lite";

export type Transcriber = (wav: Buffer) => Promise<string>;

// Speech to text with an audio model on OpenRouter, on the agent's own key.
export function openRouterTranscriber(
  apiKey: string,
  opts: { model?: string; title?: string; fetch?: typeof fetch } = {},
): Transcriber {
  const f = opts.fetch ?? fetch;
  return async (wav) => {
    const res = await f(`${OPENROUTER_BASE_URL}/chat/completions`, {
      method: "POST",
      headers: { Authorization: `Bearer ${apiKey}`, "Content-Type": "application/json", "X-Title": opts.title ?? "agent-kit" },
      body: JSON.stringify({
        model: opts.model ?? DEFAULT_TRANSCRIBE_MODEL,
        temperature: 0,
        messages: [
          {
            role: "user",
            content: [
              {
                type: "text",
                text: "Transcribe this audio exactly, in the language spoken. Reply with the transcript only. If there is no speech, reply with nothing.",
              },
              { type: "input_audio", input_audio: { data: wav.toString("base64"), format: "wav" } },
            ],
          },
        ],
      }),
    });
    const json = (await res.json().catch(() => ({}))) as {
      choices?: { message?: { content?: unknown } }[];
      error?: { message?: string };
    };
    if (!res.ok) throw new ServiceError(`speech to text failed (${res.status}): ${json.error?.message ?? res.statusText}`);
    const text = json.choices?.[0]?.message?.content;
    const said = typeof text === "string" ? text.trim() : "";
    // A model marks a recording with no speech as "<noise>", "[silence]" and so on.
    return /^[<[(][^<>[\]()]*[>\])]\.?$/.test(said) ? "" : said;
  };
}

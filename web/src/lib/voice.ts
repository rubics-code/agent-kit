import { kitURL } from "./dashboard";
import type { Agent } from "./storage";
import { toSpeechWav } from "./wav";

// Recorder records the microphone while a person holds the button.
export class Recorder {
  private rec?: MediaRecorder;
  private chunks: Blob[] = [];
  private stream?: MediaStream;

  async start(): Promise<void> {
    this.stream = await navigator.mediaDevices.getUserMedia({ audio: { channelCount: 1, echoCancellation: true, noiseSuppression: true } });
    this.chunks = [];
    this.rec = new MediaRecorder(this.stream);
    this.rec.ondataavailable = (e) => e.data.size > 0 && this.chunks.push(e.data);
    this.rec.start(250);
  }

  // stop gives the recording, or undefined when cancel is true.
  async stop(cancel = false): Promise<Blob | undefined> {
    const rec = this.rec;
    if (!rec) return undefined;
    const done = new Promise<void>((r) => (rec.onstop = () => r()));
    if (rec.state !== "inactive") rec.stop();
    await done;
    this.stream?.getTracks().forEach((t) => t.stop());
    this.rec = undefined;
    return cancel ? undefined : new Blob(this.chunks, { type: rec.mimeType });
  }
}

// transcribe sends the recording to the agent, which turns it into text with
// its own key.
export async function transcribe(agent: Agent, recording: Blob, fetchFn: typeof fetch = (...a) => fetch(...a)): Promise<string> {
  const wav = await toSpeechWav(recording);
  const res = await fetchFn(kitURL(agent, "/kit/transcribe", location.origin), {
    method: "POST",
    headers: { "Content-Type": "audio/wav" },
    body: wav as BodyInit,
  });
  if (res.status === 404) throw new Error(`${agent.name} does not take voice.`);
  if (!res.ok) throw new Error((await res.text()).trim() || `speech to text failed (${res.status})`);
  return ((await res.json()) as { text?: string }).text?.trim() ?? "";
}

// encodeWav makes a 16-bit PCM mono WAV file from samples in [-1, 1].
export function encodeWav(samples: Float32Array, rate: number): Uint8Array {
  const out = new Uint8Array(44 + samples.length * 2);
  const v = new DataView(out.buffer);
  const text = (o: number, s: string) => [...s].forEach((c, i) => v.setUint8(o + i, c.charCodeAt(0)));
  text(0, "RIFF");
  v.setUint32(4, 36 + samples.length * 2, true);
  text(8, "WAVEfmt ");
  v.setUint32(16, 16, true);
  v.setUint16(20, 1, true);
  v.setUint16(22, 1, true);
  v.setUint32(24, rate, true);
  v.setUint32(28, rate * 2, true);
  v.setUint16(32, 2, true);
  v.setUint16(34, 16, true);
  text(36, "data");
  v.setUint32(40, samples.length * 2, true);
  for (let i = 0; i < samples.length; i++) {
    const s = Math.max(-1, Math.min(1, samples[i]!));
    v.setInt16(44 + i * 2, s < 0 ? s * 0x8000 : s * 0x7fff, true);
  }
  return out;
}

export const SPEECH_RATE = 16000;

// toSpeechWav decodes a recording, and makes it mono at 16 kHz.
export async function toSpeechWav(recording: Blob): Promise<Uint8Array> {
  const ctx = new AudioContext();
  try {
    const decoded = await ctx.decodeAudioData(await recording.arrayBuffer());
    const length = Math.max(1, Math.ceil(decoded.duration * SPEECH_RATE));
    const off = new OfflineAudioContext(1, length, SPEECH_RATE);
    const src = off.createBufferSource();
    src.buffer = decoded;
    src.connect(off.destination);
    src.start();
    const mono = await off.startRendering();
    return encodeWav(mono.getChannelData(0), SPEECH_RATE);
  } finally {
    void ctx.close();
  }
}

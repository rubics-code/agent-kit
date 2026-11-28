import { describe, expect, it } from "vitest";
import { encodeWav } from "./wav";

describe("encodeWav", () => {
  it("writes a PCM header and clamps the samples", () => {
    const b = encodeWav(new Float32Array([0, 1, -1, 2]), 16000);
    const v = new DataView(b.buffer);
    expect(new TextDecoder().decode(b.slice(0, 4))).toBe("RIFF");
    expect(new TextDecoder().decode(b.slice(8, 16))).toBe("WAVEfmt ");
    expect(v.getUint16(20, true)).toBe(1);
    expect(v.getUint32(24, true)).toBe(16000);
    expect(v.getUint32(40, true)).toBe(8);
    expect([0, 1, 2, 3].map((i) => v.getInt16(44 + i * 2, true))).toEqual([0, 32767, -32768, 32767]);
  });
});

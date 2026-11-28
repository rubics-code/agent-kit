// Colours and labels for agents. Each agent gets a stable colour from its id.
const PALETTE = ["#F0616D", "#4CC38A", "#9DB4D6", "#B49AF5", "#F2A65A", "#5AB0F2"];

export function agentColor(id: string): string {
  let h = 0;
  for (const c of id) h = (h * 31 + c.charCodeAt(0)) >>> 0;
  return PALETTE[h % PALETTE.length]!;
}

export function initial(name: string): string {
  return (name.trim()[0] ?? "?").toUpperCase();
}

export function ago(ms: number, now: number): string {
  const s = Math.max(0, Math.round((now - ms) / 1000));
  if (s < 45) return "just now";
  const m = Math.round(s / 60);
  if (m < 60) return `${m} min ago`;
  const h = Math.round(m / 60);
  if (h < 24) return `${h} h ago`;
  return new Date(ms).toLocaleDateString(undefined, { day: "numeric", month: "short" });
}

export function clock(ms: number): string {
  return new Date(ms).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
}

export function toolLabel(name: string, state: string, ms?: number): string {
  const mark = { done: "✓", failed: "✕", rejected: "· rejected", waiting: "· waits for approval" }[state] ?? "…";
  const time = state === "done" && ms !== undefined ? ` ${(ms / 1000).toFixed(1)}s` : "";
  return `${name} ${mark}${time}`;
}

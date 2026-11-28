const s = { fill: "none", stroke: "currentColor", strokeWidth: 2, "aria-hidden": true } as const;

export const IconLogo = () => (
  <svg width="16" height="16" viewBox="0 0 24 24" {...s} strokeWidth={2.4}><path d="M4 10v4M8 6v12M12 3v18M16 7v10M20 10v4" /></svg>
);
export const IconBoard = () => (
  <svg width="16" height="16" viewBox="0 0 24 24" {...s}><rect x="3" y="3" width="7" height="9" rx="1.5" /><rect x="14" y="3" width="7" height="5" rx="1.5" /><rect x="14" y="12" width="7" height="9" rx="1.5" /><rect x="3" y="16" width="7" height="5" rx="1.5" /></svg>
);
export const IconPlus = () => (
  <svg width="14" height="14" viewBox="0 0 24 24" {...s}><path d="M12 5v14M5 12h14" /></svg>
);
export const IconShield = () => (
  <svg width="18" height="18" viewBox="0 0 24 24" {...s} stroke="#F0828C"><path d="M12 3l8 4v5c0 5-3.5 8-8 9-4.5-1-8-4-8-9V7z" /></svg>
);
export const IconMenu = () => (
  <svg width="18" height="18" viewBox="0 0 24 24" {...s}><path d="M4 6h16M4 12h16M4 18h16" /></svg>
);
export const IconMic = () => (
  <svg width="18" height="18" viewBox="0 0 24 24" {...s} strokeWidth={2.2}><rect x="9" y="3" width="6" height="11" rx="3" /><path d="M5 11a7 7 0 0014 0M12 18v3" /></svg>
);

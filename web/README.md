# Agent Console

The web client of the Go Kit. React, Vite and TypeScript. It renders A2UI with `@a2ui/react` and the Kit's own catalog in `src/a2ui/kit.tsx`. To run it, see "Run the console on a laptop" in the root README.

## Layout

- `src/lib/`: the logic, with no React. The A2A client and the SSE reader, the thread model and its reducer, the browser store, the dashboard client, connect, and voice. Each file has a vitest test.
- `src/components/`: the screens. Thread, Dashboard and Connect an agent, in the style of the Agent Console canvas.
- `src/a2ui/kit.tsx`: the Kit catalog (`Card`, `Stack`, `Text`, `Table`, `StatusList`, `Fields`, `Button`, `Approval`). The Go side writes these components in `kit/a2ui`. `src/a2ui/kit.test.tsx` renders a golden file from the Go tests, so the two cannot drift apart.
- `e2e/`: Playwright tests against the dev agent with the fake model.
- `embed.go`: the Go package that serves `dist/` from the agent binary.

## Bundle size

Measured with `npm run build` (Vite 8):

| Part | Size | Gzip |
| --- | --- | --- |
| App, React and the A2A client (loads first) | 259 kB | 81 kB |
| A2UI renderer and catalogs (loads with the first card) | 514 kB | 136 kB |
| CSS | 15 kB | 4 kB |
| Fonts (Latin only, Inter 400 to 700 and JetBrains Mono 400, woff2) | 118 kB | |

Before the split, the one script was 772 kB (216 kB gzip). Most of the A2UI part is Lit, the A2UI core and zod. A later step can drop the basic catalog from the bundle if no connected agent needs it.

## Open audit findings

`npm audit` reports 4 high findings. They are all one advisory: zod before 4.6.6 can use too much memory when it checks a very large constrained list (GHSA-mrhx-c7xc-m3w5). `@a2ui/react` and `@a2ui/web_core` 0.16.1 need zod 3 (`^3.25.76`), and there is no fixed zod 3. `npm audit fix --force` would go back to `@a2ui/react` 0.8.0, which does not render A2UI v1.0.

The risk: zod checks the A2UI messages that an agent sends. An agent that sends a very large message can make the browser tab slow or stop. Only agents that a person connects can send messages. Fix it when `@a2ui/*` moves to zod 4.

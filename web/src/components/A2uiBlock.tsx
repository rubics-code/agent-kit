import { lazy, Suspense } from "react";
import type { ActionPayload } from "@a2ui/web_core";

// The A2UI renderer is most of the bundle, so it loads when the first card shows.
const View = lazy(() => import("./A2uiSurfaceView"));

export function A2uiBlock(props: { messages: unknown[]; onAction?: (a: ActionPayload) => void }) {
  return (
    <Suspense fallback={<div className="meta">Loading the card…</div>}>
      <View {...props} />
    </Suspense>
  );
}

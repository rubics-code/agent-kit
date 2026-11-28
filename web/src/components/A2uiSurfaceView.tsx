import { useEffect, useRef, useState } from "react";
import { A2uiSurface } from "@a2ui/react";
import { MessageProcessor, type ActionPayload, type SurfaceModel } from "@a2ui/web_core";
import { catalogs } from "../lib/catalogs";

// A2uiBlock renders one A2UI surface from its messages.
export default function A2uiSurfaceView({ messages, onAction }: { messages: unknown[]; onAction?: (a: ActionPayload) => void }) {
  const act = useRef(onAction);
  act.current = onAction;
  const [surface, setSurface] = useState<SurfaceModel<never> | undefined>();
  const [error, setError] = useState<string>();

  useEffect(() => {
    const p = new MessageProcessor(catalogs as never, (a: ActionPayload) => act.current?.(a));
    try {
      p.processMessages(messages as never);
      setSurface([...p.model.surfacesMap.values()][0] as SurfaceModel<never> | undefined);
      setError(undefined);
    } catch (e) {
      setError(String(e));
    }
    return () => p.model.dispose?.();
  }, [messages]);

  if (error) return <div className="error-line">The card could not be drawn: {error}</div>;
  if (!surface) return null;
  return (
    <div className="a2ui-host">
      <A2uiSurface surface={surface} />
    </div>
  );
}

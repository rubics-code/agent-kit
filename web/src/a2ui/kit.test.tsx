import { act } from "react";
import { createRoot } from "react-dom/client";
import { describe, expect, it, vi } from "vitest";
import { A2uiSurface } from "@a2ui/react";
import { MessageProcessor, type ActionPayload } from "@a2ui/web_core";
import { kitCatalog, KIT_CATALOG } from "./kit";
import golden from "../../../kit/a2ui/testdata/kit_approval.json";

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

async function render(messages: unknown[], onAction = vi.fn()) {
  const p = new MessageProcessor([kitCatalog] as never, onAction as (a: ActionPayload) => void);
  p.processMessages(messages as never);
  const surface = [...p.model.surfacesMap.values()][0]!;
  const host = document.createElement("div");
  document.body.append(host);
  await act(async () => createRoot(host).render(<A2uiSurface surface={surface as never} />));
  await act(async () => new Promise((r) => setTimeout(r, 0)));
  return { host, onAction };
}

describe("Kit catalog", () => {
  it("renders the approval card that the Go Kit writes", async () => {
    const { host, onAction } = await render(golden);
    expect(host.textContent).toContain('Write the note "Groceries" (10 characters): "milk, eggs"');
    expect(host.textContent).toContain("The model did not write it.");
    expect(host.querySelector("dl")?.textContent).toContain("milk, eggs");
    const approve = [...host.querySelectorAll("button")].find((b) => b.textContent === "Approve")!;
    await act(async () => approve.click());
    expect(onAction).toHaveBeenCalledWith(expect.objectContaining({ name: "kit.approve", context: expect.objectContaining({ confirmationId: "call-1" }) }));
  });

  it("renders a card with a table and a status list", async () => {
    const id = "s1";
    const { host } = await render([
      { version: "1.0", createSurface: { surfaceId: id, catalogId: KIT_CATALOG } },
      {
        version: "1.0",
        updateComponents: {
          surfaceId: id,
          components: [
            { id: "root", component: "Card", title: "Example", meta: "Updated just now", child: "body" },
            { id: "body", component: "Stack", children: ["t", "s"] },
            { id: "t", component: "Table", columns: ["Type", "Name"], rows: [["A", "www"]], mono: [1] },
            { id: "s", component: "StatusList", items: [{ label: "API", state: "Healthy", tone: "ok" }] },
          ],
        },
      },
    ]);
    expect(host.querySelector("th")?.textContent).toBe("Type");
    expect(host.querySelector("td.mono")?.textContent).toBe("www");
    expect(host.textContent).toContain("Healthy");
  });
});

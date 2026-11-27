import { describe, expect, it } from "vitest";
import { capabilityNote, defCatalog, reviewGroup, type EnableEnv } from "../src/index.ts";
import { GROUPS } from "./fixture.ts";

const env: EnableEnv = { has: () => true, hasStore: true };

// The tool names and approval flags of main before the kit. A change here
// changes what the model can call.
const t = (name: string, approval = false) => ({ name, approval });
const EXPECTED: Record<string, { name: string; approval: boolean }[]> = {
  mxroute: [
    t("mxroute_list_domains"),
    t("mxroute_list_mailboxes"),
    t("mxroute_list_forwarders"),
    t("mxroute_get_dns_records"),
    t("mxroute_get_verification_record"),
    t("mxroute_add_domain", true),
    t("mxroute_create_mailbox", true),
    t("mxroute_reset_mailbox_password", true),
    t("mxroute_create_forwarder", true),
  ],
  cloudflare: [t("cloudflare_list_dns_records"), t("cloudflare_create_dns_record", true)],
  registrar: [
    t("cloudflare_search_domains"),
    t("cloudflare_check_domains"),
    t("cloudflare_register_domain", true),
  ],
  openrouter: [
    t("openrouter_get_credits"),
    t("openrouter_list_keys"),
    t("openrouter_create_key", true),
    t("openrouter_update_key", true),
  ],
  infisical: [
    t("infisical_list_folders"),
    t("infisical_list_secret_names"),
    t("infisical_create_random_secret", true),
  ],
};

describe("tool groups", () => {
  it("keep the tool names and the approval flags", () => {
    expect(GROUPS.map((d) => d.id).sort()).toEqual(Object.keys(EXPECTED).sort());
    for (const def of GROUPS) expect(defCatalog(def), def.id).toEqual(EXPECTED[def.id]);
  });

  it("derive a catalog from their own factories", () => {
    for (const def of GROUPS) {
      const catalog = defCatalog(def);
      expect(catalog.length, def.id).toBeGreaterThan(0);
      expect(new Set(catalog.map((t) => t.name)).size, def.id).toBe(catalog.length);
    }
  });

  it("mark each tool that asks for approval structurally", () => {
    for (const def of GROUPS) {
      for (const t of defCatalog(def)) {
        if (t.approval) expect(t.name, def.id).toMatch(/_(add|create|register|reset|update)_/);
      }
    }
  });

  it("name the missing keys of a group that is off", () => {
    const statuses = GROUPS.map((def) =>
      reviewGroup(def, { has: (k) => k === "MXROUTE_SERVER" || k === "OPENROUTER_MANAGEMENT_KEY", hasStore: false }),
    );
    const by = Object.fromEntries(statuses.map((g) => [g.id, g]));

    expect(by.openrouter).toMatchObject({ enabled: true, missing: [] });
    expect(by.mxroute).toMatchObject({ enabled: false, missing: ["MXROUTE_USERNAME", "MXROUTE_API_KEY"] });
    expect(by.cloudflare).toMatchObject({ enabled: false, missing: ["CLOUDFLARE_API_TOKEN"] });
    expect(by.infisical).toMatchObject({ enabled: false });
  });

  it("tell the operator why a group is off", () => {
    const statuses = GROUPS.map((def) =>
      reviewGroup(def, { has: (k) => k === "OPENROUTER_MANAGEMENT_KEY", hasStore: false }),
    );
    const note = capabilityNote(statuses, "Greg");
    expect(note).toContain("- OpenRouter keys: on.");
    expect(note).toContain("- Cloudflare DNS: OFF. Missing: CLOUDFLARE_API_TOKEN.");
  });
});

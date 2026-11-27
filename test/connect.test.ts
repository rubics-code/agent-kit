import { describe, expect, it } from "vitest";
import { tool } from "langchain";
import { z } from "zod";
import {
  HttpCloudflareDns,
  HttpMxroute,
  approvedTool,
  assembleAgent,
  cloudflareGroup,
  connectServices,
  holdsAll,
  infisicalGroup,
  mxrouteGroup,
  type ConnectContext,
  type SecretStore,
  type ToolGroupDef,
} from "../src/index.ts";
import { GROUPS } from "./fixture.ts";
import { rubyPolicy } from "./fixture.ts";

const ctx = (keys: Record<string, string>, store?: SecretStore): ConnectContext => ({
  holds: (k) => !!keys[k],
  get: (k) => keys[k]!,
  store,
});

describe("connect", () => {
  it("makes a service only when the session holds every key", () => {
    expect(mxrouteGroup.connect(ctx({ MXROUTE_SERVER: "s", MXROUTE_USERNAME: "u" }))).toBeUndefined();
    expect(
      mxrouteGroup.connect(ctx({ MXROUTE_SERVER: "s", MXROUTE_USERNAME: "u", MXROUTE_API_KEY: "k" })),
    ).toBeInstanceOf(HttpMxroute);
    expect(cloudflareGroup.connect(ctx({ CLOUDFLARE_API_TOKEN: "t" }))).toBeInstanceOf(HttpCloudflareDns);
  });

  it("gives the Infisical group the agent's store", () => {
    const store = {} as SecretStore;
    expect(infisicalGroup.connect(ctx({}, store))).toBe(store);
    expect(infisicalGroup.connect(ctx({}))).toBeUndefined();
  });

  it("connects each group in a list by id", () => {
    const services = connectServices(GROUPS, ctx({ CLOUDFLARE_API_TOKEN: "t" }));
    expect(Object.keys(services).sort()).toEqual(GROUPS.map((g) => g.id).sort());
    expect(services.cloudflare).toBeDefined();
    expect(services.registrar).toBeDefined();
    expect(services.mxroute).toBeUndefined();
  });
});

// A group that an agent in another repo writes. It uses only the Kit's
// public interface.
interface Weather {
  forecast(city: string): Promise<string>;
  alert(city: string): Promise<void>;
}

const weatherGroup: ToolGroupDef<Weather> = {
  id: "weather",
  name: "Weather",
  host: "api.weather.test",
  keys: ["WEATHER_KEY"],
  setup: "Set WEATHER_KEY.",
  guidance: "Use the weather tools for forecasts.",
  connect(c) {
    return holdsAll(c, this.keys)
      ? { forecast: async (city) => `sun in ${city}`, alert: async () => {} }
      : undefined;
  },
  tools(svc, shared) {
    return [
      tool(({ city }) => svc.forecast(city), {
        name: "weather_forecast",
        description: "Get a forecast.",
        schema: z.object({ city: z.string() }),
      }),
      approvedTool({
        name: "weather_send_alert",
        description: "Send an alert.",
        operator: shared.policy.operator,
        schema: z.object({ city: z.string() }),
        prepare: async ({ city }) => ({ summary: `send an alert for ${city}` }),
        run: ({ city }) => svc.alert(city),
      }),
    ];
  },
};

describe("a custom group", () => {
  const build = (keys: Record<string, string>) => {
    const groups = [weatherGroup, cloudflareGroup];
    return assembleAgent({
      groups,
      system: "You are Scout.",
      policy: rubyPolicy,
      services: connectServices(groups, ctx(keys)),
      secrets: { deliver: async () => {} },
      enable: { has: (k) => !!keys[k] },
    });
  };

  it("mounts its tools and joins the briefing when its key is there", () => {
    const { tools, briefing, statuses } = build({ WEATHER_KEY: "w" });
    expect(tools.map((t) => t.name)).toEqual(["weather_forecast", "weather_send_alert"]);
    expect(briefing).toContain("Use the weather tools for forecasts.");
    expect(statuses.find((s) => s.id === "weather")?.catalog).toEqual([
      { name: "weather_forecast", approval: false },
      { name: "weather_send_alert", approval: true },
    ]);
    expect(statuses.find((s) => s.id === "cloudflare")).toMatchObject({ enabled: false, missing: ["CLOUDFLARE_API_TOKEN"] });
  });

  it("stays off without its key", () => {
    const { tools, statuses } = build({});
    expect(tools).toEqual([]);
    expect(statuses.find((s) => s.id === "weather")).toMatchObject({ enabled: false, missing: ["WEATHER_KEY"] });
  });
});

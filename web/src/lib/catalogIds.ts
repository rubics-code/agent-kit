import { BASIC_CATALOG } from "./a2a";

// The ids of the A2UI catalogs that this client renders, in order of
// preference. They go to the agent on each message. The catalogs themselves
// load on demand, in lib/catalogs.ts.
export const KIT_CATALOG = "https://github.com/rubics-code/agent-kit/catalogs/kit/v1";
export const catalogIds = [KIT_CATALOG, BASIC_CATALOG];

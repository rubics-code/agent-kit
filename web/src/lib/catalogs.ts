import { basicCatalog } from "@a2ui/web_core/catalogs/basic/v1";
import { BASIC_CATALOG } from "./a2a";
import { KIT_CATALOG, kitCatalog } from "../a2ui/kit";

// The A2UI catalogs that this client renders, in order of preference. The
// ids go to the agent on each message.
export const catalogs = [kitCatalog, basicCatalog];
export const catalogIds = [KIT_CATALOG, BASIC_CATALOG];

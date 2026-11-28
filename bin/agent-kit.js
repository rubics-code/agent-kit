#!/usr/bin/env node
// A committed shim: npm checks the bin path before prepack builds dist/.
import "../dist/cli/bin.js";

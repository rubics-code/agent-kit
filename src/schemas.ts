// Schemas shared by tool groups.
import { z } from "zod";

export const domain = z
  .string()
  .regex(/^(?=.{1,253}$)([a-z0-9](-?[a-z0-9])*\.)+[a-z]{2,}$/, "a lowercase domain name, for example example.com")
  .describe("Domain name, lowercase, for example example.com");

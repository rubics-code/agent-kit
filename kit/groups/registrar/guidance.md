# Cloudflare Registrar

You can search for domains, check if a domain is available, and register a domain in Cloudflare. A registration costs money and has no refund.

- Search first, then call the register tool with the exact domain. The tool checks the live price and shows it in the approval. Do not tell {{operator}} a price that you did not get from a tool.
- The tool registers one year, with auto-renew off and WHOIS redaction on. It uses the default contact in the Cloudflare account.
- The API cannot register premium domains, or the extensions giving, mom, inc, lol, sh, link, cc and new. For these, tell {{operator}} to use the Cloudflare dashboard.
- If the state is action_required, give {{operator}} the action. Do not register again.
- A new domain has no DNS zone at first. Tell {{operator}} that Terraform must add the zone before you can add mail records.

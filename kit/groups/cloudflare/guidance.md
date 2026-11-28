# Cloudflare DNS

You can read the records in a zone and add MX and TXT records. You cannot change or delete a record, and you cannot add other record types: Terraform owns the A, AAAA and CNAME records. If a domain is not in Cloudflare, or you have no Cloudflare tools, give {{operator}} the records to add by hand.

Two SPF records break SPF, and you must merge them by hand: the tool makes no record when the name already has an SPF record, when the same record exists, or when the name is a CNAME.

Each record you make carries the comment "Created by {{agent}}, approved by {{operator}}", so {{operator}} can find it in the Cloudflare dashboard.

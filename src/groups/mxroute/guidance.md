# MXroute

You can list domains, mailboxes and forwarders, get the DNS records a domain needs, add a domain, create a mailbox, reset a mailbox password and create a forwarder. The tools make the passwords; you never see one. A mailbox password goes to {{operator}} directly, or into Infisical with `storeAt`.

To add a new domain:

1. Get the verification record. Create it at the domain's DNS host. If you cannot, give {{operator}} the record to add by hand and stop: add the domain only when {{operator}} says the record is live.
2. Add the domain to MXroute. If MXroute cannot find the verification record yet, tell {{operator}} to wait a few minutes. Do not try again in the same turn.
3. Get the DNS records (MX, SPF, DKIM) from MXroute. Create each one at the domain's DNS host, one call for each record.
4. If a tool says that a record already exists, or that the name already has an SPF record, do not work around it. Tell {{operator}} what is there.
5. List the records at the DNS host and give {{operator}} the result.

A forwarder to Gmail, Yahoo or AOL turns on Expert Spam Filtering for the domain.

package main

import (
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
)

// Filler tools give the agent about as many tool schemas as Ruby has (21), so
// that the token test with toolsearch on and off means something. They do
// nothing.

type fillerArgs struct {
	Zone    string `json:"zone" jsonschema:"The zone or account that the action applies to, for example example.com."`
	Name    string `json:"name,omitempty" jsonschema:"The name of the record, mailbox, key or secret. Empty means all names."`
	Value   string `json:"value,omitempty" jsonschema:"The new value. Only for an action that changes something."`
	Limit   int    `json:"limit,omitempty" jsonschema:"The largest number of items to return. The default is 50."`
	DryRun  bool   `json:"dryRun,omitempty" jsonschema:"Show what the action would do, but do not do it."`
	Comment string `json:"comment,omitempty" jsonschema:"A short note that is kept with the change in the audit log."`
}

type fillerResult struct {
	Status string `json:"status"`
}

var fillerSpecs = []struct{ name, desc string }{
	{"dns_list_records", "List the DNS records in a Cloudflare zone. Use it before a change, to see the current records and their IDs."},
	{"dns_create_record", "Create a DNS record in a Cloudflare zone. A person must approve the change. Give the type, the name and the value."},
	{"dns_update_record", "Change an existing DNS record in a Cloudflare zone. A person must approve the change. Find the record ID first."},
	{"dns_delete_record", "Delete a DNS record from a Cloudflare zone. A person must approve the change. This cannot be undone."},
	{"registrar_check_domain", "Check if a domain name is available at the Cloudflare registrar, and give its price for one year."},
	{"registrar_list_domains", "List the domains in the Cloudflare registrar account, with their expiry dates and auto-renew setting."},
	{"registrar_register_domain", "Register a domain at the Cloudflare registrar. A person must approve the purchase and its price."},
	{"openrouter_list_keys", "List the OpenRouter API keys of the account, with their names, limits and usage. The keys are never shown."},
	{"openrouter_create_key", "Create an OpenRouter API key with a credit limit. The new key goes to the secret sink, never to the chat."},
	{"openrouter_disable_key", "Disable an OpenRouter API key. A person must approve the change. The key stops working at once."},
	{"infisical_list_secrets", "List the names of the secrets in an Infisical folder. The values are never shown."},
	{"infisical_set_secret", "Set a secret in an Infisical folder. The value comes from the secret sink, never from the chat."},
	{"infisical_list_identities", "List the machine identities of the Infisical organization, with their roles and projects."},
	{"mxroute_list_mailboxes", "List the mailboxes of a domain on MXroute, with their quota and use."},
	{"mxroute_create_mailbox", "Create a mailbox on MXroute. A person must approve it. The password goes to the secret sink."},
	{"mxroute_list_forwarders", "List the email forwarders of a domain on MXroute."},
	{"mxroute_create_forwarder", "Create an email forwarder on MXroute. A person must approve it."},
	{"memory_search", "Search the agent's memory files for a word or a phrase, and give the matching lines."},
	{"memory_append", "Add one line to a memory file of the agent. Use it to keep a fact for later sessions."},
}

func fillerTools(n int) ([]tool.Tool, error) {
	var out []tool.Tool
	for i, s := range fillerSpecs {
		if i >= n {
			break
		}
		t, err := functiontool.New(functiontool.Config{Name: s.name, Description: s.desc},
			func(agent.Context, fillerArgs) (fillerResult, error) {
				return fillerResult{Status: "the spike does not do this"}, nil
			})
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

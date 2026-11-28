// Package secrets is where a new secret goes. A new secret never goes into the
// conversation, to the model, or to a client.
package secrets

import "context"

// Sink takes a new secret.
type Sink interface {
	Deliver(ctx context.Context, label, value string) error
	// Where says where the secret goes, for the tool's reply.
	Where() string
}

// Place is a folder and a name in a secret store.
type Place struct {
	Path string // a folder, for example /agents/example
	Name string // a secret name, for example EXAMPLE_API_KEY
}

// Store is what a tool may do with a secret store. No call returns a value.
type Store interface {
	ListFolders(ctx context.Context, path string) ([]string, error)
	ListSecretNames(ctx context.Context, path string) ([]string, error)
	// Check says whether the folder and the secret exist now.
	Check(ctx context.Context, p Place) (folderExists, exists bool, err error)
	// Put makes missing folders, then creates the secret or replaces its value.
	Put(ctx context.Context, p Place, value string) error
}

package naming

// OP defines the corresponding operations for a name resolution change.
type OP uint8

const (
	// Add indicates a new address is added.
	Add = iota
	// Delete indicates an exisiting address is deleted.
	Delete
)

type ServiceConfig interface{}

// Update defines a name resolution change.
type Update struct {
	// Op indicates the operation of the update.
	Op     OP
	Addr   string
	Config ServiceConfig
}

// Resolver does one-shot name resolution and creates a Watcher to
// watch the future updates.
type Resolver interface {
	// Resolve returns the name resolution results.
	Resolve(target string) ([]*Update, error)
	// NewWatcher creates a Watcher to watch the changes on target.
	NewWatcher(target string) Watcher
}

// Watcher watches the updates for a particular target.
type Watcher interface {
	// Next blocks until an update or error happens. It may return one or more
	// updates.
	Next() ([]*Update, error)
	// Stop stops the Watcher.
	Stop()
}

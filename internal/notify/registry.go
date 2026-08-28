package notify

import "fmt"

// Builder constructs a Notifier from a params map.
// Each notifier type registers its builder via Register.
type Builder func(params map[string]string) (Notifier, error)

var registry = map[string]Builder{}

// Register adds a notifier builder for the given type name.
// It is typically called from an init() function.
func Register(name string, b Builder) {
	registry[name] = b
}

// Build constructs a Notifier for the given type and params.
func Build(name string, params map[string]string) (Notifier, error) {
	b, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unsupported notification target type %q", name)
	}
	return b(params)
}

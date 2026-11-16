// Package translator routes text to the active translation provider.
package translator

import (
	"context"
	"errors"
)

// ErrNoProvider is returned when no provider is active.
var ErrNoProvider = errors.New("no translator selected")

// Provider translates a single line of game text.
type Provider interface {
	Name() string
	Translate(ctx context.Context, text string) (string, error)
}

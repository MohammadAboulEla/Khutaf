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

type glossaryKey struct{}
type dialogueCtxKey struct{}

// WithGlossaryPrompt attaches matched glossary terms to ctx for LLM providers.
func WithGlossaryPrompt(ctx context.Context, prompt string) context.Context {
	if prompt == "" {
		return ctx
	}
	return context.WithValue(ctx, glossaryKey{}, prompt)
}

// GlossaryPromptFromContext extracts any attached glossary prompt from ctx.
func GlossaryPromptFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(glossaryKey{}).(string)
	return v
}

// WithDialogueContext attaches recent story dialogue turns (and active speaker) to ctx for LLM providers.
func WithDialogueContext(ctx context.Context, prompt string) context.Context {
	if prompt == "" {
		return ctx
	}
	return context.WithValue(ctx, dialogueCtxKey{}, prompt)
}

// DialogueContextFromContext extracts any attached rolling dialogue context from ctx.
func DialogueContextFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(dialogueCtxKey{}).(string)
	return v
}

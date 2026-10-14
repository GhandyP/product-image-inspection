// Package llm is the one place that reads the model configuration and builds a
// Gemini client.
//
// Two packages call a model — the evidence reader and the image describer — and
// they must not drift apart on which model is used, how the API key is
// discovered, or what a missing key looks like. Keeping that in one package is
// also what lets the rest of the repository be tested with a fake model and no
// network.
package llm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/model/gemini"
	"google.golang.org/genai"
)

// DefaultModel is used when ADK_MODEL is not set.
const DefaultModel = "gemini-flash-latest"

// ErrMissingAPIKey reports that GOOGLE_API_KEY was not configured.
var ErrMissingAPIKey = errors.New("missing GOOGLE_API_KEY")

// Name reports the model a caller will use.
func Name() string {
	if name := strings.TrimSpace(os.Getenv("ADK_MODEL")); name != "" {
		return name
	}
	return DefaultModel
}

// New builds the Gemini model, or reports that no key was configured.
//
// The key is read from the environment at call time, so a command that never
// calls a model needs no key and no network.
func New(ctx context.Context) (model.LLM, error) {
	key := strings.TrimSpace(os.Getenv("GOOGLE_API_KEY"))
	if key == "" {
		return nil, ErrMissingAPIKey
	}

	m, err := gemini.NewModel(ctx, Name(), &genai.ClientConfig{APIKey: key})
	if err != nil {
		return nil, fmt.Errorf("create Gemini model: %w", err)
	}
	return m, nil
}

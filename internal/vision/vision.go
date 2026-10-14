// Package vision turns a supplied image into a factual description the reader
// can quote.
//
// It is a port with two implementations, and that is the point: the tests run
// against fixtures with no model and no network, and the command line replaces
// them with Gemini only when a key is configured. A description is evidence, not
// a verdict — the describer is told so and the reader quotes it, so an image
// cannot decide anything by itself either.
package vision

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/genai"

	"github.com/GhandyP/product-image-inspection/internal/llm"
)

// Instruction is the stable instruction of the describer.
//
// It asks for what is visible and says where, and it forbids the verdict this
// package must never produce. It carries no brace, because ADK resolves
// anything shaped like a session-state placeholder before the model is called.
const Instruction = `You describe one product photograph so that an inspection can quote your description.

Report only what is visible: the parts of the product, their condition, and any mark, scratch, gap, stain, or
crease, with where in the frame it is. When the image does not show something clearly enough to describe it,
say that it is not visible, or that the frame is too dark, or that the part is out of frame.

You never decide whether the product passes or fails, and you never judge it against a criterion. Another
program compares your description with the criteria and a person makes the decision. A verdict in your answer
would be discarded, and a description of something you could not see would become evidence nobody can check.

Answer with plain prose, three sentences at most. No markdown, no lists, no headings.`

// Describer returns a factual description of one supplied image.
type Describer interface {
	// Describe returns what is visible in the image at ref. An empty
	// description is a failure, not an answer.
	Describe(ctx context.Context, ref string) (string, error)
}

// ADKDescriber describes images through a Google ADK runner.
type ADKDescriber struct {
	runner *runner.Runner
}

var _ Describer = (*ADKDescriber)(nil)

// errNothingDescribed reports an answer that describes nothing, which is not
// evidence.
var errNothingDescribed = errors.New("the model described nothing")

// NewADKDescriber builds a describer around any model.LLM, which is what lets
// the tests exercise the multimodal request without the network.
func NewADKDescriber(m model.LLM) (*ADKDescriber, error) {
	describerAgent, err := llmagent.New(llmagent.Config{
		Name:        "image_describer",
		Description: "describing what is visible in one product photograph",
		Model:       m,
		InstructionProvider: func(adkagent.ReadonlyContext) (string, error) {
			return Instruction, nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create image describer agent: %w", err)
	}

	r, err := runner.NewInMemory("product-image-inspection", describerAgent)
	if err != nil {
		return nil, fmt.Errorf("create image describer runner: %w", err)
	}
	return &ADKDescriber{runner: r}, nil
}

// NewGeminiDescriber builds a describer backed by the configured Gemini model.
func NewGeminiDescriber(ctx context.Context) (*ADKDescriber, error) {
	m, err := llm.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("create the Gemini describer: %w", err)
	}
	return NewADKDescriber(m)
}

// Describe sends one image as a multimodal part and returns the description.
func (d *ADKDescriber) Describe(ctx context.Context, ref string) (string, error) {
	data, mimeType, err := Load(ref)
	if err != nil {
		return "", err
	}

	message := genai.NewContentFromParts([]*genai.Part{
		genai.NewPartFromText("Describe the image supplied for this inspection."),
		genai.NewPartFromBytes(data, mimeType),
	}, genai.RoleUser)

	var description string
	for event, err := range d.runner.Run(ctx, "user", "describe", message, adkagent.RunConfig{}) {
		if err != nil {
			return "", fmt.Errorf("run the image describer: %w", err)
		}
		if event == nil || event.Content == nil {
			continue
		}
		description = contentText(event.Content)
	}

	if strings.TrimSpace(description) == "" {
		return "", fmt.Errorf("run the image describer for %q: %w", ref, errNothingDescribed)
	}
	return strings.TrimSpace(description), nil
}

// Load reads one image and reports the media type it has to be sent as.
//
// Only the formats a Gemini request accepts as an inline image part are read,
// so an unsupported file is refused before it costs a model call.
func Load(ref string) ([]byte, string, error) {
	mimeType, ok := mediaTypes[strings.ToLower(filepath.Ext(ref))]
	if !ok {
		return nil, "", fmt.Errorf("image %q: unsupported image type %q, want .png, .jpg, .jpeg or .webp",
			ref, filepath.Ext(ref))
	}

	data, err := os.ReadFile(ref)
	if err != nil {
		return nil, "", fmt.Errorf("read image %q: %w", ref, err)
	}
	if len(data) == 0 {
		return nil, "", fmt.Errorf("image %q is empty", ref)
	}
	return data, mimeType, nil
}

// mediaTypes maps the extensions this program accepts to their media types.
var mediaTypes = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
}

// Fake is a Describer that answers from a fixed map of descriptions.
//
// It exists so the whole pipeline can run offline, over the fixtures in
// testdata, without a key and without the network.
type Fake struct {
	// Descriptions maps an image reference to its description.
	Descriptions map[string]string
	// Err, when set, is returned instead of a description.
	Err error
}

var _ Describer = Fake{}

// Describe returns the fixture description of one image.
func (f Fake) Describe(_ context.Context, ref string) (string, error) {
	if f.Err != nil {
		return "", f.Err
	}

	description, ok := f.Descriptions[ref]
	if !ok {
		return "", fmt.Errorf("there is no fixture description for %q", ref)
	}
	if strings.TrimSpace(description) == "" {
		return "", fmt.Errorf("the fixture description of %q is empty", ref)
	}
	return description, nil
}

// LoadFixture reads a description file, shaped as an object that maps an image
// reference to the description of that image, and returns a Fake over it.
//
// A fixture is checked here rather than at the first Describe call, so a broken
// fixture is reported once, by name, instead of as a missing image later.
func LoadFixture(path string) (Fake, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Fake{}, fmt.Errorf("read descriptions %q: %w", path, err)
	}

	descriptions := map[string]string{}
	if err := json.Unmarshal(data, &descriptions); err != nil {
		return Fake{}, fmt.Errorf("decode descriptions %q: %w", path, err)
	}
	if len(descriptions) == 0 {
		return Fake{}, fmt.Errorf("descriptions %q map no image to a description", path)
	}

	for _, ref := range sortedRefs(descriptions) {
		if strings.TrimSpace(descriptions[ref]) == "" {
			return Fake{}, fmt.Errorf("descriptions %q: the description of %q is empty", path, ref)
		}
	}
	return Fake{Descriptions: descriptions}, nil
}

// sortedRefs returns the keys of a description map in a stable order.
func sortedRefs(descriptions map[string]string) []string {
	return slices.Sorted(maps.Keys(descriptions))
}

// contentText concatenates the text parts of one model turn.
func contentText(content *genai.Content) string {
	var b strings.Builder
	for _, part := range content.Parts {
		if part != nil {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

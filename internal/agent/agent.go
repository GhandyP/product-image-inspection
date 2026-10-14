// Package agent reads the evidence of one inspection into structured
// observations, with Google ADK.
//
// This is the only package in the repository that calls a model, and the model
// has exactly one job: say what the supplied evidence shows, criterion by
// criterion. It never decides whether a product passes or fails — that is
// internal/evaluate — and nothing it answers is believed on its own:
//
//   - every observation has to quote text that was actually supplied, and the
//     quote is re-checked here;
//   - every observation has to name a criterion that was declared;
//   - an image observation has to name the image it came from and where in that
//     image the evidence is;
//   - one violation refuses the whole answer. A partly usable answer would make
//     a status look backed by evidence that the model invented.
//
// What survives those rules is still only an observation. The evaluator applies
// its own rules on top, so a run of this program can be wrong about the world
// but cannot quietly be wrong about what its evidence says.
package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/genai"

	"github.com/GhandyP/product-image-inspection/internal/inspection"
	"github.com/GhandyP/product-image-inspection/internal/llm"
)

// Instruction is the stable instruction of the reader agent.
//
// It states the same rules the guard enforces after the answer arrives, because
// a model that is told the rules produces usable answers more often than one
// that is corrected afterwards. It carries no brace: ADK resolves anything
// shaped like a session-state placeholder in an instruction before the model is
// called, and fails the run when it cannot resolve it.
const Instruction = `You read the evidence supplied for one product inspection and report what it shows,
one observation per criterion.

You never decide whether a product passes or fails, and you never decide what happens to the inventory.
Another program derives the status from your observations, and a person makes the decision. Your job is to
report evidence, not a verdict.

Report one observation for every criterion you can judge from the supplied text. Leave out the criteria you
cannot judge: an empty answer is a valid answer, and it is better than a guess.

Rules for every observation:
- criterion_id must be one of the declared criterion identifiers, spelled exactly as it was declared.
- source is "note" when the evidence comes from the inspector's notes, and "image" when it comes from the
  description of one image.
- evidence must be text copied from that source exactly as it appears there. Never paraphrase, complete,
  translate, or clean up the text you quote, and never report an observation whose evidence you cannot copy
  from the source character for character.
- finding is "complies" when the evidence shows the criterion holds, "violates" when it shows the criterion is
  broken, and "indeterminate" when you cannot tell. Use "indeterminate" instead of guessing: it is the honest
  answer, and it sends that criterion to a person.
- confidence is a number between 0 and 1 that says how sure you are. Report it honestly; a low value is useful
  and an inflated one is not.
- When source is "image", set image_ref to the exact reference of the image whose description you quoted, and
  set location to where in the image the evidence is, in words a person can follow, such as "lower-left corner
  of the front label". An observation from an image without a location is refused.
- When source is "note", leave image_ref out.

Answer with the JSON object only. No prose, no explanation, no markdown.`

// Reader turns the supplied evidence into observations about the declared
// criteria.
type Reader interface {
	// Read returns one observation for each thing the evidence shows. It
	// returns nothing usable alongside an error: an answer that broke one rule
	// is refused whole.
	Read(ctx context.Context, criteria inspection.Criteria, evidence inspection.Evidence) ([]inspection.Observation, error)
}

// ADKReader reads evidence through a Google ADK runner.
type ADKReader struct {
	runner         *runner.Runner
	sessionCounter atomic.Uint64
}

var _ Reader = (*ADKReader)(nil)

// ErrMissingAPIKey reports that GOOGLE_API_KEY was not configured. It is the
// error the shared model client returns, named here because a caller of this
// package is the one that has to act on it.
var ErrMissingAPIKey = llm.ErrMissingAPIKey

var errEmptyResponse = errors.New("the model returned no observations")

// NewADKReader builds a reader around any model.LLM, which is what lets the
// tests exercise this package through a fake model instead of the network.
func NewADKReader(m model.LLM) (*ADKReader, error) {
	readerAgent, err := llmagent.New(llmagent.Config{
		Name:        "evidence_reader",
		Description: "reading inspection evidence into observations about declared criteria",
		Model:       m,
		// InstructionProvider is the documented way to keep an instruction out
		// of ADK's session-state substitution, which fails the run before the
		// model is called when the text contains something shaped like a
		// placeholder.
		InstructionProvider: func(adkagent.ReadonlyContext) (string, error) { return Instruction, nil },
		OutputSchema:        OutputSchema(),
	})
	if err != nil {
		return nil, fmt.Errorf("create evidence reader agent: %w", err)
	}

	r, err := runner.NewInMemory("product-image-inspection", readerAgent)
	if err != nil {
		return nil, fmt.Errorf("create evidence reader runner: %w", err)
	}
	return &ADKReader{runner: r}, nil
}

// NewGeminiReader builds a reader backed by the configured Gemini model.
func NewGeminiReader(ctx context.Context) (*ADKReader, error) {
	m, err := llm.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("create the Gemini reader: %w", err)
	}
	return NewADKReader(m)
}

// Prompt wraps the evidence in the reading task. It is deterministic: the same
// criteria and the same evidence produce the same request, images in a stable
// order, so two runs are comparable.
func Prompt(criteria inspection.Criteria, evidence inspection.Evidence) string {
	var b strings.Builder

	fmt.Fprintf(&b, "Read the supplied evidence for product %s and report what it shows about each criterion.\n",
		criteria.ProductID)

	b.WriteString("\nCriteria:\n")
	for _, criterion := range criteria.Criteria {
		fmt.Fprintf(&b, "- %s: %s\n", criterion.ID, criterion.Statement)
	}

	b.WriteString("\nNotes:\n")
	if strings.TrimSpace(evidence.Notes) == "" {
		b.WriteString("no notes were supplied.\n")
	} else {
		b.WriteString(evidence.Notes)
		b.WriteString("\n")
	}

	b.WriteString("\nImages:\n")
	if len(evidence.Images) == 0 {
		b.WriteString("no images were supplied.\n")
	} else {
		for _, ref := range inspection.ImageRefs(evidence) {
			fmt.Fprintf(&b, "image %q:\n%s\n", ref, evidence.Images[ref])
		}
	}

	return b.String()
}

// Read runs one ADK turn, decodes the observations, and refuses the answer
// unless every observation quotes the evidence it claims and names a criterion
// that exists.
func (r *ADKReader) Read(
	ctx context.Context,
	criteria inspection.Criteria,
	evidence inspection.Evidence,
) ([]inspection.Observation, error) {
	// The input is refused before a model is called, so a malformed request
	// costs nothing and cannot produce half an answer.
	if err := criteria.Validate(); err != nil {
		return nil, err
	}
	if err := evidence.Validate(); err != nil {
		return nil, err
	}

	message := genai.NewContentFromText(Prompt(criteria, evidence), genai.RoleUser)
	sessionID := fmt.Sprintf("read-%d", r.sessionCounter.Add(1))

	var responseText string
	for event, err := range r.runner.Run(ctx, "user", sessionID, message, adkagent.RunConfig{}) {
		if err != nil {
			return nil, fmt.Errorf("run the evidence reader: %w", err)
		}
		if event == nil || event.Content == nil {
			continue
		}
		responseText = contentText(event.Content)
	}

	if strings.TrimSpace(responseText) == "" {
		return nil, fmt.Errorf("run the evidence reader: %w", errEmptyResponse)
	}

	var answer observationAnswer
	if err := json.Unmarshal([]byte(responseText), &answer); err != nil {
		return nil, fmt.Errorf("decode the observations: %w", err)
	}

	observations := answer.Observations
	if err := inspection.ValidateObservations(criteria, evidence, observations); err != nil {
		return nil, fmt.Errorf("the model reported observations this inspection cannot use: %w", err)
	}
	if err := inspection.VerifyQuotes(evidence, observations); err != nil {
		return nil, fmt.Errorf("the model reported evidence that was not supplied: %w", err)
	}

	return observations, nil
}

// observationAnswer mirrors the output schema of the reader agent. It is
// deliberately a different type from any result type: the model answers
// observations, never a status.
type observationAnswer struct {
	Observations []inspection.Observation `json:"observations"`
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

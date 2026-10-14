package agent

import (
	"context"
	"errors"
	"iter"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"

	"github.com/GhandyP/product-image-inspection/internal/inspection"
)

// The evidence every answer in these tests has to quote.
const (
	notes            = "The seam along the lid is open on the left side.\nThe label is legible."
	labelDescription = "The label image shows text that is legible and square."
)

const validResponse = `{"observations": [
  {"criterion_id": "seam", "source": "note", "evidence": "The seam along the lid is open on the left side.",
   "finding": "violates", "confidence": 0.9},
  {"criterion_id": "label", "source": "image", "image_ref": "label.png", "location": "centre of the label",
   "evidence": "text that is legible and square", "finding": "complies", "confidence": 0.8}
]}`

func testCriteria() inspection.Criteria {
	return inspection.Criteria{
		ProductID: "SKU-4471",
		Criteria: []inspection.Criterion{
			{ID: "seam", Statement: "The seam along the lid is closed."},
			{ID: "label", Statement: "The label is legible and square."},
		},
	}
}

func testEvidence() inspection.Evidence {
	return inspection.Evidence{
		Notes:  notes,
		Images: map[string]string{"label.png": labelDescription},
	}
}

// fakeModel stands in for Gemini, so the package is exercised without a key and
// without the network.
type fakeModel struct {
	response string
	err      error
	requests []*model.LLMRequest
}

func (m *fakeModel) Name() string { return "fake-model" }

func (m *fakeModel) GenerateContent(_ context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	m.requests = append(m.requests, req)

	return func(yield func(*model.LLMResponse, error) bool) {
		if m.err != nil {
			yield(nil, m.err)
			return
		}
		yield(&model.LLMResponse{
			Content:      genai.NewContentFromText(m.response, genai.RoleModel),
			TurnComplete: true,
		}, nil)
	}
}

func newTestReader(t *testing.T, m model.LLM) *ADKReader {
	t.Helper()

	reader, err := NewADKReader(m)
	if err != nil {
		t.Fatalf("NewADKReader() error = %v, want nil", err)
	}
	return reader
}

func requestText(req *model.LLMRequest) string {
	var b strings.Builder
	for _, content := range req.Contents {
		b.WriteString(contentText(content))
	}
	return b.String()
}

func TestReadReturnsObservationsThatQuoteTheSuppliedEvidence(t *testing.T) {
	observations, err := newTestReader(t, &fakeModel{response: validResponse}).Read(
		context.Background(), testCriteria(), testEvidence())
	if err != nil {
		t.Fatalf("Read() error = %v, want nil", err)
	}

	want := []inspection.Observation{
		{
			CriterionID: "seam", Source: inspection.SourceNote,
			Evidence: "The seam along the lid is open on the left side.",
			Finding:  inspection.FindingViolates, Confidence: 0.9,
		},
		{
			CriterionID: "label", Source: inspection.SourceImage, ImageRef: "label.png",
			Location: "centre of the label", Evidence: "text that is legible and square",
			Finding: inspection.FindingComplies, Confidence: 0.8,
		},
	}

	if !reflect.DeepEqual(observations, want) {
		t.Errorf("Read() =\n%+v\nwant\n%+v", observations, want)
	}
}

func TestReadRefusesAnAnswerItCannotTrust(t *testing.T) {
	cases := []struct {
		name     string
		response string
		modelErr error
		want     string
	}{
		{
			name: "a quote the notes do not contain",
			response: `{"observations": [{"criterion_id": "seam", "source": "note",
			  "evidence": "the seam is welded shut", "finding": "violates", "confidence": 0.9}]}`,
			want: `criterion "seam": observation 1 quotes text the notes do not contain`,
		},
		{
			name: "an image observation quoting the notes instead of the image",
			response: `{"observations": [{"criterion_id": "label", "source": "image", "image_ref": "label.png",
			  "location": "centre", "evidence": "The label is legible.", "finding": "complies", "confidence": 0.9}]}`,
			want: `quotes text the description of "label.png" does not contain`,
		},
		{
			name: "an observation about a criterion that was never declared",
			response: `{"observations": [{"criterion_id": "colour", "source": "note",
			  "evidence": "The label is legible.", "finding": "complies", "confidence": 0.9}]}`,
			want: `unknown criterion "colour"`,
		},
		{
			name: "an image observation that does not say where to look",
			response: `{"observations": [{"criterion_id": "label", "source": "image", "image_ref": "label.png",
			  "evidence": "text that is legible and square", "finding": "complies", "confidence": 0.9}]}`,
			want: "location is required when the observation comes from the image",
		},
		{
			name: "an image observation about an image nobody supplied",
			response: `{"observations": [{"criterion_id": "label", "source": "image", "image_ref": "back.png",
			  "location": "centre", "evidence": "text that is legible and square", "finding": "complies", "confidence": 0.9}]}`,
			want: `image "back.png" is not one of the supplied images`,
		},
		{
			name: "a confidence outside the range",
			response: `{"observations": [{"criterion_id": "seam", "source": "note",
			  "evidence": "The seam along the lid is open on the left side.", "finding": "violates", "confidence": 1.4}]}`,
			want: "confidence must be in [0, 1], got 1.4",
		},
		{
			name:     "an answer that is not JSON",
			response: `Of course! Here are the observations you asked for.`,
			want:     "decode the observations",
		},
		{
			name:     "an empty answer",
			response: "   ",
			want:     "returned no observations",
		},
		{
			name:     "a model that failed",
			modelErr: errors.New("the model is unreachable"),
			want:     "the model is unreachable",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			observations, err := newTestReader(t, &fakeModel{response: tc.response, err: tc.modelErr}).Read(
				context.Background(), testCriteria(), testEvidence())

			if err == nil {
				t.Fatalf("Read() = %+v, want an error naming %q", observations, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Read() = %q, want it to contain %q", err, tc.want)
			}
			if observations != nil {
				t.Errorf("Read() returned %+v alongside an error, want nothing: a refused answer is not partly usable", observations)
			}
		})
	}
}

func TestReadRefusesInputItCannotUse(t *testing.T) {
	m := &fakeModel{response: validResponse}

	cases := []struct {
		name     string
		criteria inspection.Criteria
		evidence inspection.Evidence
		want     string
	}{
		{
			name:     "criteria without a product",
			criteria: inspection.Criteria{Criteria: testCriteria().Criteria},
			evidence: testEvidence(),
			want:     "product_id is required",
		},
		{
			name:     "nothing supplied to read",
			criteria: testCriteria(),
			evidence: inspection.Evidence{},
			want:     "notes or at least one image is required",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			observations, err := newTestReader(t, m).Read(context.Background(), tc.criteria, tc.evidence)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Read() = %v, want it to contain %q", err, tc.want)
			}
			if observations != nil {
				t.Errorf("Read() returned %+v alongside an error, want nothing", observations)
			}
		})
	}

	if len(m.requests) != 0 {
		t.Errorf("the model received %d requests for input this package should have refused first", len(m.requests))
	}
}

func TestTheInstructionReachesTheModelVerbatimAndCarriesNoBrace(t *testing.T) {
	m := &fakeModel{response: validResponse}

	if _, err := newTestReader(t, m).Read(context.Background(), testCriteria(), testEvidence()); err != nil {
		t.Fatalf("Read() error = %v, want nil", err)
	}
	if len(m.requests) != 1 {
		t.Fatalf("model received %d requests, want exactly 1", len(m.requests))
	}
	if m.requests[0].Config == nil || m.requests[0].Config.SystemInstruction == nil {
		t.Fatal("the model received no system instruction")
	}

	got := contentText(m.requests[0].Config.SystemInstruction)
	if !strings.HasPrefix(got, Instruction) {
		t.Errorf("the instruction the model received does not start with the instruction this package publishes:\n%q", got)
	}

	// ADK appends the agent's identity to whatever instruction it is given.
	// That suffix is the framework's; nothing else may be added, and nothing
	// may be substituted into ours.
	suffix := strings.TrimPrefix(got, Instruction)
	if !strings.Contains(suffix, `"evidence_reader"`) {
		t.Errorf("the instruction the model received carries unexpected text: %q", suffix)
	}

	// ADK resolves anything shaped like a session-state placeholder in a plain
	// Instruction before the model is called, and fails the run when it cannot.
	// A brace in the instruction is therefore a defect, not a style choice.
	if strings.Contains(Instruction, "{") {
		t.Error("the instruction carries a brace: ADK would read it as a session-state placeholder")
	}

	for _, want := range []string{"never decide", "quote", "indeterminate", "image_ref", "location"} {
		if !strings.Contains(Instruction, want) {
			t.Errorf("the instruction does not mention %q, which the guard enforces", want)
		}
	}
}

func TestPromptCarriesTheCriteriaTheNotesAndTheDescribedImages(t *testing.T) {
	prompt := Prompt(testCriteria(), testEvidence())

	for _, want := range []string{
		"SKU-4471",
		"seam: The seam along the lid is closed.",
		"label: The label is legible and square.",
		"The seam along the lid is open on the left side.",
		`image "label.png"`,
		labelDescription,
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("Prompt() =\n%s\nwant it to contain %q", prompt, want)
		}
	}
}

func TestPromptNamesTheSourcesItHas(t *testing.T) {
	onlyNotes := Prompt(testCriteria(), inspection.Evidence{Notes: notes})
	if strings.Contains(onlyNotes, `image "`) {
		t.Errorf("Prompt() offers an image when none was supplied:\n%s", onlyNotes)
	}
	if !strings.Contains(onlyNotes, "no images were supplied") {
		t.Errorf("Prompt() = \n%s\nwant it to say that no image was supplied", onlyNotes)
	}

	onlyImages := Prompt(testCriteria(), inspection.Evidence{
		Images: map[string]string{"label.png": labelDescription},
	})
	if !strings.Contains(onlyImages, "no notes were supplied") {
		t.Errorf("Prompt() =\n%s\nwant it to say that no notes were supplied", onlyImages)
	}
}

func TestOutputSchemaDescribesTheObservationContract(t *testing.T) {
	schema := OutputSchema()

	if schema.Type != genai.TypeObject {
		t.Fatalf("schema type = %v, want an object", schema.Type)
	}

	observations, ok := schema.Properties["observations"]
	if !ok || observations.Type != genai.TypeArray || observations.Items == nil {
		t.Fatalf("schema has no observations array: %+v", schema.Properties)
	}

	item := observations.Items
	for _, key := range []string{"criterion_id", "source", "evidence", "finding", "confidence", "location", "image_ref"} {
		if _, ok := item.Properties[key]; !ok {
			t.Errorf("the item schema has no %q, which the guard validates", key)
		}
	}
	if want := []string{"criterion_id", "source", "evidence", "finding", "confidence"}; !reflect.DeepEqual(item.Required, want) {
		t.Errorf("required = %v, want %v", item.Required, want)
	}

	if got := item.Properties["source"].Enum; !reflect.DeepEqual(got, []string{"note", "image"}) {
		t.Errorf("source enum = %v, want note and image", got)
	}
	if got := item.Properties["finding"].Enum; !reflect.DeepEqual(got, []string{"complies", "violates", "indeterminate"}) {
		t.Errorf("finding enum = %v, want complies, violates, indeterminate", got)
	}
	if item.Properties["confidence"].Type != genai.TypeNumber {
		t.Errorf("confidence type = %v, want a number", item.Properties["confidence"].Type)
	}
	if item.Properties["location"].Type != genai.TypeString || item.Properties["image_ref"].Type != genai.TypeString {
		t.Error("location and image_ref have to be offered as strings the guard can check")
	}
}

func TestFakeReaderAnswersWithoutAModel(t *testing.T) {
	want := []inspection.Observation{{
		CriterionID: "seam", Source: inspection.SourceNote, Evidence: "The seam along the lid is open on the left side.",
		Finding: inspection.FindingViolates, Confidence: 0.9,
	}}

	observations, err := Fake{Observations: want}.Read(context.Background(), testCriteria(), testEvidence())
	if err != nil {
		t.Fatalf("Read() error = %v, want nil", err)
	}
	if !reflect.DeepEqual(observations, want) {
		t.Errorf("Read() = %+v, want %+v", observations, want)
	}

	failure := errors.New("no reader configured")
	if _, err := (Fake{Err: failure}).Read(context.Background(), testCriteria(), testEvidence()); !errors.Is(err, failure) {
		t.Errorf("Read() error = %v, want %v", err, failure)
	}
}

func TestModelNameHonoursTheEnvironment(t *testing.T) {
	t.Setenv("ADK_MODEL", "")
	if got := ModelName(); got != DefaultModel {
		t.Errorf("ModelName() = %q, want %q", got, DefaultModel)
	}

	t.Setenv("ADK_MODEL", "  gemini-2.5-pro  ")
	if got := ModelName(); got != "gemini-2.5-pro" {
		t.Errorf("ModelName() = %q, want the override without its padding", got)
	}
}

func TestNewGeminiReaderNeedsAKey(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "")

	if _, err := NewGeminiReader(context.Background()); !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("NewGeminiReader() error = %v, want %v", err, ErrMissingAPIKey)
	}
}

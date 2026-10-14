package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GhandyP/product-image-inspection/internal/agent"
	"github.com/GhandyP/product-image-inspection/internal/evaluate"
	"github.com/GhandyP/product-image-inspection/internal/inspection"
	"github.com/GhandyP/product-image-inspection/internal/llm"
	"github.com/GhandyP/product-image-inspection/internal/vision"
)

const (
	criteriaJSON = `{
	  "product_id": "SKU-4471",
	  "criteria": [
	    {"id": "seam", "statement": "The seam along the lid is closed."},
	    {"id": "label", "statement": "The label is legible and square."}
	  ]
	}`

	notes = "The seam along the lid is open on the left side.\nThe label is legible."

	resolvedObservations = `{"observations": [
	  {"criterion_id": "seam", "source": "note",
	   "evidence": "The seam along the lid is open on the left side.",
	   "finding": "violates", "confidence": 0.9},
	  {"criterion_id": "label", "source": "note",
	   "evidence": "The label is legible.", "finding": "complies", "confidence": 0.9}
	]}`

	// seamOnly leaves the label unobserved, which is what a run that needs a
	// person looks like.
	seamOnlyObservations = `{"observations": [
	  {"criterion_id": "seam", "source": "note",
	   "evidence": "The seam along the lid is open on the left side.",
	   "finding": "violates", "confidence": 0.9}
	]}`
)

// write puts a file in the test's own directory and returns its path.
func write(t *testing.T, name, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("prepare %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// noModel fails the test when a run reaches for a model it should not need.
func noModel(t *testing.T) Deps {
	t.Helper()

	return Deps{
		NewReader: func(context.Context) (agent.Reader, error) {
			t.Error("the run asked for a model reader when it was given observations")
			return nil, errors.New("no model was expected")
		},
		NewDescriber: func(context.Context) (vision.Describer, error) {
			t.Error("the run asked for a model describer when it was given descriptions")
			return nil, errors.New("no model was expected")
		},
	}
}

type outcome struct {
	code   int
	stdout string
	stderr string
}

// run drives the command line and collects everything a caller could observe.
func run(t *testing.T, deps Deps, args ...string) outcome {
	t.Helper()

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), args, deps, &stdout, &stderr)
	return outcome{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

func TestRunEvaluatesSuppliedObservationsWithoutAModel(t *testing.T) {
	criteria := write(t, "criteria.json", criteriaJSON)
	notesPath := write(t, "notes.txt", notes)
	observations := write(t, "observations.json", resolvedObservations)

	got := run(t, noModel(t), "-criteria", criteria, "-notes", notesPath, "-observations", observations)

	if got.code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got.code, ExitOK, got.stdout, got.stderr)
	}
	for _, want := range []string{
		"SKU-4471",
		"seam", "fail",
		"label", "pass",
		"1 pass, 1 fail, 0 review in 2 criteria",
		"qualified human inspector",
	} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("stdout does not contain %q:\n%s", want, got.stdout)
		}
	}
	if got.stderr != "" {
		t.Errorf("stderr = %q, want nothing on a successful run", got.stderr)
	}
}

func TestRunCarriesTheEvidenceIntoThePreview(t *testing.T) {
	criteria := write(t, "criteria.json", criteriaJSON)
	notesPath := write(t, "notes.txt", notes)
	observations := write(t, "observations.json", resolvedObservations)

	got := run(t, noModel(t), "-criteria", criteria, "-notes", notesPath, "-observations", observations)

	if !strings.Contains(got.stdout, "The seam along the lid is open on the left side.") {
		t.Errorf("stdout does not carry the evidence a reader can check:\n%s", got.stdout)
	}
	if !strings.Contains(got.stdout, "The label is legible.") {
		t.Errorf("stdout does not carry the evidence of the resolved criterion:\n%s", got.stdout)
	}
}

func TestRunExitsReviewWhenTheEvidenceDidNotResolveEveryCriterion(t *testing.T) {
	criteria := write(t, "criteria.json", criteriaJSON)
	notesPath := write(t, "notes.txt", notes)
	observations := write(t, "observations.json", seamOnlyObservations)

	got := run(t, noModel(t), "-criteria", criteria, "-notes", notesPath, "-observations", observations)

	if got.code != ExitReview {
		t.Fatalf("exit code = %d, want %d (a criterion without evidence is not an error)\nstdout:\n%s\nstderr:\n%s",
			got.code, ExitReview, got.stdout, got.stderr)
	}
	if !strings.Contains(got.stdout, "no_observation") {
		t.Errorf("stdout does not say why the criterion needs a person:\n%s", got.stdout)
	}
}

func TestRunPrintsTheResultAsJSONWhenAsked(t *testing.T) {
	criteria := write(t, "criteria.json", criteriaJSON)
	notesPath := write(t, "notes.txt", notes)
	observations := write(t, "observations.json", resolvedObservations)

	got := run(t, noModel(t), "-criteria", criteria, "-notes", notesPath, "-observations", observations, "-json")
	if got.code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr:\n%s", got.code, ExitOK, got.stderr)
	}

	var result inspection.Result
	if err := json.Unmarshal([]byte(got.stdout), &result); err != nil {
		t.Fatalf("stdout is not the result as JSON: %v\n%s", err, got.stdout)
	}
	if result.ProductID != "SKU-4471" || len(result.Checks) != 2 {
		t.Fatalf("result = %+v, want the two criteria of SKU-4471", result)
	}
	if result.Checks[0].Status != inspection.StatusFail || result.Checks[1].Status != inspection.StatusPass {
		t.Errorf("checks = %+v, want seam fail and label pass", result.Checks)
	}
	if result.Summary.Pass != 1 || result.Summary.Fail != 1 || result.Summary.Review != 0 {
		t.Errorf("summary = %+v, want 1 pass, 1 fail, 0 review", result.Summary)
	}
	if !result.Approval.Required {
		t.Error("approval.required = false, want true in every result")
	}
}

func TestRunUsesTheThresholdItWasGiven(t *testing.T) {
	criteria := write(t, "criteria.json", criteriaJSON)
	notesPath := write(t, "notes.txt", notes)
	observations := write(t, "observations.json", `{"observations": [
	  {"criterion_id": "seam", "source": "note",
	   "evidence": "The seam along the lid is open on the left side.",
	   "finding": "violates", "confidence": 0.8},
	  {"criterion_id": "label", "source": "note",
	   "evidence": "The label is legible.", "finding": "complies", "confidence": 0.8}
	]}`)

	got := run(t, noModel(t), "-criteria", criteria, "-notes", notesPath, "-observations", observations, "-threshold", "0.9")
	if got.code != ExitReview {
		t.Fatalf("exit code = %d, want %d: 0.8 is below the 0.9 the caller asked for\nstdout:\n%s",
			got.code, ExitReview, got.stdout)
	}
	if !strings.Contains(got.stdout, "low_confidence") {
		t.Errorf("stdout does not carry the reason the criterion was not resolved:\n%s", got.stdout)
	}

	got = run(t, noModel(t), "-criteria", criteria, "-notes", notesPath, "-observations", observations)
	if got.code != ExitOK {
		t.Fatalf("exit code = %d, want %d under the default threshold\nstdout:\n%s", got.code, ExitOK, got.stdout)
	}
}

func TestRunRefusesWhatItCannotUse(t *testing.T) {
	good := write(t, "criteria.json", criteriaJSON)
	notesPath := write(t, "notes.txt", notes)
	goodObservations := write(t, "observations.json", resolvedObservations)

	cases := []struct {
		name     string
		args     []string
		wantCode int
		want     string
	}{
		{
			name:     "no criteria",
			args:     []string{"-notes", notesPath, "-observations", goodObservations},
			wantCode: ExitUsage,
			want:     "give -criteria",
		},
		{
			name:     "no evidence at all",
			args:     []string{"-criteria", good, "-observations", goodObservations},
			wantCode: ExitUsage,
			want:     "give -notes or at least one -image",
		},
		{
			name:     "an unexpected argument",
			args:     []string{"-criteria", good, "-notes", notesPath, "-observations", goodObservations, "extra"},
			wantCode: ExitUsage,
			want:     "unexpected argument",
		},
		{
			name:     "criteria that break the contract",
			args:     []string{"-criteria", write(t, "twice.json", `{"product_id": "SKU-4471", "criteria": [{"id": "seam", "statement": "x"}, {"id": "seam", "statement": "y"}]}`), "-notes", notesPath, "-observations", goodObservations},
			wantCode: ExitRefused,
			want:     `duplicate criterion "seam"`,
		},
		{
			name:     "criteria that are not JSON",
			args:     []string{"-criteria", write(t, "broken.json", "{oh no"), "-notes", notesPath, "-observations", goodObservations},
			wantCode: ExitRefused,
			want:     "decode the criteria",
		},
		{
			name:     "notes that are not there",
			args:     []string{"-criteria", good, "-notes", filepath.Join(t.TempDir(), "absent.txt"), "-observations", goodObservations},
			wantCode: ExitRefused,
			want:     "read the notes",
		},
		{
			name:     "observations about an undeclared criterion",
			args:     []string{"-criteria", good, "-notes", notesPath, "-observations", write(t, "unknown.json", `{"observations": [{"criterion_id": "colour", "source": "note", "evidence": "The label is legible.", "finding": "complies", "confidence": 0.9}]}`)},
			wantCode: ExitRefused,
			want:     `unknown criterion "colour"`,
		},
		{
			name:     "observations with an invented quote",
			args:     []string{"-criteria", good, "-notes", notesPath, "-observations", write(t, "invented.json", `{"observations": [{"criterion_id": "seam", "source": "note", "evidence": "the seam is welded shut", "finding": "violates", "confidence": 0.9}]}`)},
			wantCode: ExitRefused,
			want:     "quotes text the notes do not contain",
		},
		{
			name:     "observations that are not JSON",
			args:     []string{"-criteria", good, "-notes", notesPath, "-observations", write(t, "broken-observations.json", "of course!")},
			wantCode: ExitRefused,
			want:     "decode the observations",
		},
		{
			name:     "an image nobody described",
			args:     []string{"-criteria", good, "-image", "testdata/images/fail.png", "-descriptions", write(t, "descriptions.json", `{"other.png": "a panel"}`)},
			wantCode: ExitRefused,
			want:     `no fixture description for "testdata/images/fail.png"`,
		},
		{
			name:     "descriptions that are not there",
			args:     []string{"-criteria", good, "-image", "testdata/images/fail.png", "-descriptions", filepath.Join(t.TempDir(), "absent.json")},
			wantCode: ExitRefused,
			want:     "read descriptions",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := run(t, noModel(t), tc.args...)

			if got.code != tc.wantCode {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s",
					got.code, tc.wantCode, got.stdout, got.stderr)
			}
			if !strings.Contains(got.stderr, tc.want) {
				t.Fatalf("stderr does not contain %q:\n%s", tc.want, got.stderr)
			}
			if got.stdout != "" {
				t.Errorf("stdout = %q, want nothing when the request was refused", got.stdout)
			}
		})
	}
}

func TestRunReportsAModelItCannotUse(t *testing.T) {
	criteria := write(t, "criteria.json", criteriaJSON)
	notesPath := write(t, "notes.txt", notes)

	for _, tc := range []struct {
		name string
		deps Deps
		args []string
	}{
		{
			name: "no reader configured",
			deps: Deps{NewReader: func(context.Context) (agent.Reader, error) { return nil, llm.ErrMissingAPIKey }},
			args: []string{"-criteria", criteria, "-notes", notesPath},
		},
		{
			name: "no describer configured",
			deps: Deps{NewDescriber: func(context.Context) (vision.Describer, error) { return nil, llm.ErrMissingAPIKey }},
			args: []string{"-criteria", criteria, "-image", "testdata/images/fail.png"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := run(t, tc.deps, tc.args...)

			if got.code != ExitModel {
				t.Fatalf("exit code = %d, want %d\nstderr:\n%s", got.code, ExitModel, got.stderr)
			}
			if !strings.Contains(got.stderr, "GOOGLE_API_KEY") {
				t.Fatalf("stderr does not name the missing configuration:\n%s", got.stderr)
			}
		})
	}
}

func TestRunReadsEvidenceThroughTheReaderItIsGiven(t *testing.T) {
	criteria := write(t, "criteria.json", criteriaJSON)
	notesPath := write(t, "notes.txt", notes)

	deps := Deps{NewReader: func(context.Context) (agent.Reader, error) {
		return agent.Fake{Observations: []inspection.Observation{
			{
				CriterionID: "seam", Source: inspection.SourceNote,
				Evidence: "The seam along the lid is open on the left side.",
				Finding:  inspection.FindingViolates, Confidence: 0.9,
			},
			{
				CriterionID: "label", Source: inspection.SourceNote, Evidence: "The label is legible.",
				Finding: inspection.FindingComplies, Confidence: 0.9,
			},
		}}, nil
	}}

	got := run(t, deps, "-criteria", criteria, "-notes", notesPath)
	if got.code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr:\n%s", got.code, ExitOK, got.stderr)
	}
	if !strings.Contains(got.stdout, "1 pass, 1 fail, 0 review in 2 criteria") {
		t.Errorf("stdout does not carry the summary of the run:\n%s", got.stdout)
	}
}

func TestRunDescribesImagesAndEvaluatesTheirEvidence(t *testing.T) {
	criteria := write(t, "criteria.json", criteriaJSON)
	image := write(t, "images/fail.png", "not a real png, but the fake never reads it")
	description := "A pale panel with a dark diagonal scratch across its lower half."
	descriptions := write(t, "descriptions.json", `{"`+image+`": "`+description+`"}`)

	observations := write(t, "observations.json", `{"observations": [
	  {"criterion_id": "seam", "source": "image", "image_ref": "`+image+`",
	   "location": "across the lower half", "evidence": "dark diagonal scratch",
	   "finding": "violates", "confidence": 0.9},
	  {"criterion_id": "label", "source": "image", "image_ref": "`+image+`",
	   "location": "centre", "evidence": "A pale panel", "finding": "complies", "confidence": 0.9}
	]}`)

	got := run(t, noModel(t), "-criteria", criteria, "-image", image, "-descriptions", descriptions, "-observations", observations)

	if got.code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got.code, ExitOK, got.stdout, got.stderr)
	}
	if !strings.Contains(got.stdout, "dark diagonal scratch") {
		t.Errorf("stdout does not carry the evidence quoted from the image:\n%s", got.stdout)
	}
	if !strings.Contains(got.stdout, "across the lower half") {
		t.Errorf("stdout does not say where in the image the evidence is:\n%s", got.stdout)
	}
}

func TestRunDescribesAnImageWithTheDescriberItIsGiven(t *testing.T) {
	criteria := write(t, "criteria.json", criteriaJSON)
	image := write(t, "images/fail.png", "bytes")
	description := "A pale panel with a dark diagonal scratch across its lower half."

	var asked []string
	deps := Deps{
		NewDescriber: func(context.Context) (vision.Describer, error) {
			return vision.Fake{Descriptions: map[string]string{image: description}}, nil
		},
		NewReader: func(context.Context) (agent.Reader, error) {
			return agent.Fake{Observations: []inspection.Observation{{
				CriterionID: "seam", Source: inspection.SourceImage, ImageRef: image,
				Location: "across the lower half", Evidence: "dark diagonal scratch",
				Finding:  inspection.FindingViolates, Confidence: 0.9,
			}}}, nil
		},
	}
	_ = asked

	got := run(t, deps, "-criteria", criteria, "-image", image)
	if got.code != ExitReview {
		t.Fatalf("exit code = %d, want %d: the label had no observation\nstdout:\n%s\nstderr:\n%s",
			got.code, ExitReview, got.stdout, got.stderr)
	}
	if !strings.Contains(got.stdout, "dark diagonal scratch") {
		t.Errorf("stdout does not carry the evidence quoted from the described image:\n%s", got.stdout)
	}
}

func TestRunReportsADefectInItsOwnGuarantee(t *testing.T) {
	criteria := write(t, "criteria.json", criteriaJSON)
	notesPath := write(t, "notes.txt", notes)
	observations := write(t, "observations.json", resolvedObservations)

	deps := noModel(t)
	deps.Verify = func(inspection.Criteria, inspection.Result, evaluate.Options) error {
		return errors.New("result has 1 checks for 2 criteria")
	}

	got := run(t, deps, "-criteria", criteria, "-notes", notesPath, "-observations", observations)

	if got.code != ExitDefect {
		t.Fatalf("exit code = %d, want %d: a result that breaks the guarantee is a defect\nstderr:\n%s",
			got.code, ExitDefect, got.stderr)
	}
	if !strings.Contains(got.stderr, "defect in this repository") {
		t.Errorf("stderr does not say whose defect it is:\n%s", got.stderr)
	}
	if got.stdout != "" {
		t.Errorf("stdout = %q, want nothing printed for a defective result", got.stdout)
	}
}

func TestRunAnswersHelpWithoutFailing(t *testing.T) {
	got := run(t, Deps{}, "-h")

	if got.code != ExitOK {
		t.Fatalf("exit code = %d, want %d for -h", got.code, ExitOK)
	}
	if !strings.Contains(got.stderr, "-criteria") {
		t.Errorf("the help does not describe the flags:\n%s", got.stderr)
	}
}

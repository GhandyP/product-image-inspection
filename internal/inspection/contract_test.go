package inspection

import (
	"strings"
	"testing"
)

// validCriteria is the baseline every case mutates, so a failure names the rule
// that broke rather than an unrelated missing field.
func validCriteria() Criteria {
	return Criteria{
		ProductID: "SKU-4471",
		Criteria: []Criterion{
			{ID: "seam", Statement: "The seam along the lid is closed."},
			{ID: "label", Statement: "The label is legible and aligned.", MinConfidence: 0.9},
		},
	}
}

// validEvidence is the baseline supply every observation case quotes.
func validEvidence() Evidence {
	return Evidence{
		Notes:  "the seam is closed and the label is legible",
		Images: map[string]string{"testdata/label.png": "the label is legible"},
	}
}

func TestCriteriaValidateAcceptsACompleteContract(t *testing.T) {
	if err := validCriteria().Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

func TestCriteriaValidateRefusesEveryMalformedField(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Criteria)
		want   string
	}{
		{
			name:   "product id required",
			mutate: func(c *Criteria) { c.ProductID = "   " },
			want:   "product_id is required",
		},
		{
			name:   "at least one criterion",
			mutate: func(c *Criteria) { c.Criteria = nil },
			want:   "at least one criterion is required",
		},
		{
			name:   "criterion id required",
			mutate: func(c *Criteria) { c.Criteria[1].ID = "" },
			want:   "criterion 2: id is required",
		},
		{
			name:   "criterion statement required",
			mutate: func(c *Criteria) { c.Criteria[0].Statement = "  " },
			want:   `criterion "seam": statement is required`,
		},
		{
			name:   "duplicate criterion",
			mutate: func(c *Criteria) { c.Criteria[1].ID = "seam" },
			want:   `duplicate criterion "seam"`,
		},
		{
			name:   "min confidence above range",
			mutate: func(c *Criteria) { c.Criteria[0].MinConfidence = 1.5 },
			want:   `criterion "seam": min_confidence must be in (0, 1], got 1.5`,
		},
		{
			name:   "min confidence below range",
			mutate: func(c *Criteria) { c.Criteria[0].MinConfidence = -0.1 },
			want:   `criterion "seam": min_confidence must be in (0, 1], got -0.1`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			criteria := validCriteria()
			tc.mutate(&criteria)

			err := criteria.Validate()
			if err == nil {
				t.Fatalf("Validate() = nil, want an error naming %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestCriteriaValidateReportsEveryProblemAtOnce(t *testing.T) {
	criteria := Criteria{
		Criteria: []Criterion{
			{ID: "seam"},
			{ID: "seam"},
			{},
		},
	}

	err := criteria.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want problems")
	}

	problems := strings.Split(err.Error(), "; ")
	for _, want := range []string{
		"product_id is required",
		`criterion "seam": statement is required`,
		`duplicate criterion "seam"`,
		"criterion 3: id is required",
		"criterion 3: statement is required",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Validate() = %q, missing %q (problems: %v)", err, want, problems)
		}
	}
}

func TestMinimumConfidenceLetsTheCriterionOverrideTheRunDefault(t *testing.T) {
	cases := []struct {
		name string
		min  float64
		run  float64
		want float64
	}{
		{name: "run default when the criterion says nothing", min: 0, run: 0.7, want: 0.7},
		{name: "criterion override wins", min: 0.9, run: 0.7, want: 0.9},
		{name: "criterion override can be lower than the default", min: 0.4, run: 0.7, want: 0.4},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			criterion := Criterion{ID: "seam", Statement: "x", MinConfidence: tc.min}
			if got := criterion.MinimumConfidence(tc.run); got != tc.want {
				t.Fatalf("MinimumConfidence(%v) = %v, want %v", tc.run, got, tc.want)
			}
		})
	}
}

func TestObservationsValidateAcceptsCompleteObservations(t *testing.T) {
	observations := []Observation{
		{CriterionID: "seam", Source: SourceNote, Evidence: "the seam is closed", Finding: FindingComplies, Confidence: 0.8},
		{
			CriterionID: "label", Source: SourceImage, ImageRef: "testdata/label.png", Location: "top-left",
			Evidence: "label is legible", Finding: FindingViolates, Confidence: 0.95,
		},
	}

	if err := ValidateObservations(validCriteria(), validEvidence(), observations); err != nil {
		t.Fatalf("ValidateObservations() = %v, want nil", err)
	}
}

func TestObservationsValidateRefusesEveryMalformedField(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Observation)
		want   string
	}{
		{
			name:   "criterion id required",
			mutate: func(o *Observation) { o.CriterionID = "" },
			want:   "observation 1: criterion_id is required",
		},
		{
			name:   "criterion must be declared",
			mutate: func(o *Observation) { o.CriterionID = "colour" },
			want:   `observation 1: unknown criterion "colour"`,
		},
		{
			name:   "evidence required",
			mutate: func(o *Observation) { o.Evidence = "   " },
			want:   `criterion "seam": observation 1: evidence is required`,
		},
		{
			name:   "image evidence needs a location",
			mutate: func(o *Observation) { o.Source = SourceImage; o.ImageRef = "testdata/label.png" },
			want:   `criterion "seam": observation 1: location is required when the observation comes from the image`,
		},
		{
			name:   "image evidence needs its image",
			mutate: func(o *Observation) { o.Source = SourceImage; o.Location = "top-left" },
			want:   `criterion "seam": observation 1: image_ref is required when the observation comes from the image`,
		},
		{
			name: "image evidence must name a supplied image",
			mutate: func(o *Observation) {
				o.Source = SourceImage
				o.Location = "top-left"
				o.ImageRef = "testdata/back.png"
			},
			want: `criterion "seam": observation 1: image "testdata/back.png" is not one of the supplied images`,
		},
		{
			name: "a note observation cannot name an image",
			mutate: func(o *Observation) {
				o.ImageRef = "testdata/label.png"
			},
			want: `criterion "seam": observation 1: image_ref is only for image observations`,
		},
		{
			name:   "source must be known",
			mutate: func(o *Observation) { o.Source = "notes" },
			want:   `criterion "seam": observation 1: source "notes" is not one of note, image`,
		},
		{
			name:   "finding must be known",
			mutate: func(o *Observation) { o.Finding = "maybe" },
			want:   `criterion "seam": observation 1: finding "maybe" is not one of complies, violates, indeterminate`,
		},
		{
			name:   "confidence above range",
			mutate: func(o *Observation) { o.Confidence = 1.01 },
			want:   `criterion "seam": observation 1: confidence must be in [0, 1], got 1.01`,
		},
		{
			name:   "confidence below range",
			mutate: func(o *Observation) { o.Confidence = -0.5 },
			want:   `criterion "seam": observation 1: confidence must be in [0, 1], got -0.5`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			observation := Observation{
				CriterionID: "seam",
				Source:      SourceNote,
				Evidence:    "the seam is closed",
				Finding:     FindingComplies,
				Confidence:  0.8,
			}
			tc.mutate(&observation)

			err := ValidateObservations(validCriteria(), validEvidence(), []Observation{observation})
			if err == nil {
				t.Fatalf("ValidateObservations() = nil, want an error naming %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateObservations() = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestObservationsValidateAcceptsAnEmptySet(t *testing.T) {
	// No observation is not a malformed payload: it is the shape that makes
	// every criterion resolve to review.
	if err := ValidateObservations(validCriteria(), validEvidence(), nil); err != nil {
		t.Fatalf("ValidateObservations() = %v, want nil", err)
	}
}

func TestInputValidateRefusesMalformedImageRefs(t *testing.T) {
	cases := []struct {
		name  string
		input Input
		want  string
	}{
		{
			name:  "empty ref",
			input: Input{ProductID: "SKU-4471", Images: []string{"testdata/label.png", "  "}},
			want:  "images[1]: image ref is required",
		},
		{
			name:  "repeated ref",
			input: Input{ProductID: "SKU-4471", Images: []string{"a.png", "a.png"}},
			want:  `duplicate image ref "a.png"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.input.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestEvidenceForSelectsTheSourceAnObservationClaims(t *testing.T) {
	evidence := Evidence{
		Notes:  "the seam is closed",
		Images: map[string]string{"label.png": "the label is legible"},
	}

	cases := []struct {
		name     string
		source   Source
		imageRef string
		want     string
		found    bool
	}{
		{name: "notes", source: SourceNote, want: "the seam is closed", found: true},
		{name: "a described image", source: SourceImage, imageRef: "label.png", want: "the label is legible", found: true},
		{name: "an unknown image", source: SourceImage, imageRef: "back.png"},
		{name: "an unknown source", source: "label"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, found := evidence.EvidenceFor(tc.source, tc.imageRef)
			if got != tc.want || found != tc.found {
				t.Fatalf("EvidenceFor(%q, %q) = (%q, %v), want (%q, %v)", tc.source, tc.imageRef, got, found, tc.want, tc.found)
			}
		})
	}
}

func TestInputValidateRequiresAProductAndSomeEvidence(t *testing.T) {
	cases := []struct {
		name  string
		input Input
		want  string
	}{
		{
			name:  "notes are enough",
			input: Input{ProductID: "SKU-4471", Notes: "the seam is closed"},
		},
		{
			name:  "an image without notes is enough",
			input: Input{ProductID: "SKU-4471", Images: []string{"testdata/pass.png"}},
		},
		{
			name:  "product id required",
			input: Input{Notes: "the seam is closed"},
			want:  "product_id is required",
		},
		{
			name:  "notes or an image required",
			input: Input{ProductID: "SKU-4471", Notes: "   "},
			want:  "notes or at least one image is required",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.input.Validate()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestEvidenceValidateNeedsSomethingToReadAndADescriptionPerImage(t *testing.T) {
	cases := []struct {
		name     string
		evidence Evidence
		want     string
	}{
		{
			name:     "notes are enough",
			evidence: Evidence{Notes: "the seam is closed"},
		},
		{
			name:     "a described image is enough",
			evidence: Evidence{Images: map[string]string{"label.png": "the label is legible"}},
		},
		{
			name:     "nothing supplied",
			evidence: Evidence{},
			want:     "notes or at least one image is required",
		},
		{
			name:     "blank notes and no image",
			evidence: Evidence{Notes: "   "},
			want:     "notes or at least one image is required",
		},
		{
			name:     "an image without a description",
			evidence: Evidence{Images: map[string]string{"label.png": "  "}},
			want:     `image "label.png" has no description`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.evidence.Validate()
			if tc.want == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestEvidenceFoundInNeedsARealQuote(t *testing.T) {
	const notes = "The seam along the lid is\nclosed, and the label is legible."

	cases := []struct {
		name     string
		evidence string
		want     bool
	}{
		{name: "verbatim", evidence: "the seam along the lid is", want: true},
		{name: "across a line break", evidence: "lid is closed", want: true},
		{name: "different case", evidence: "The Label Is Legible", want: true},
		{name: "surrounding spaces", evidence: "  label is legible.  ", want: true},
		{name: "invented text", evidence: "the seam is welded shut", want: false},
		{name: "empty", evidence: "", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			observation := Observation{Evidence: tc.evidence}
			if got := observation.EvidenceFoundIn(notes); got != tc.want {
				t.Fatalf("EvidenceFoundIn(%q) = %v, want %v", tc.evidence, got, tc.want)
			}
		})
	}
}

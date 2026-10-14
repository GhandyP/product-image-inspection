package evaluate

import (
	"strings"
	"testing"

	"github.com/GhandyP/product-image-inspection/internal/inspection"
)

// The text every observation in these tests has to quote. They are constants so
// a test can assert the evidence it expected, not a substring it hoped for.
const (
	seamNote  = "The seam along the lid is\nopen on the left side."
	labelNote = "The label is legible and square to the edge."
	markNote  = "The back panel shows a scratch across the barcode."
	labelShot = "The label is legible and square to the edge in the image."
)

func criteria() inspection.Criteria {
	return inspection.Criteria{
		ProductID: "SKU-4471",
		Criteria: []inspection.Criterion{
			{ID: "seam", Statement: "The seam along the lid is closed."},
			{ID: "label", Statement: "The label is legible and square."},
			{ID: "barcode", Statement: "The barcode is unmarked."},
		},
	}
}

func evidence() inspection.Evidence {
	return inspection.Evidence{
		Notes:  strings.Join([]string{seamNote, labelNote, markNote}, "\n"),
		Images: map[string]string{"label.png": labelShot},
	}
}

// threeResolved is the baseline for the cases that overlook one criterion: the
// other two resolve, so a failure names the rule under test.
func threeResolved() []inspection.Observation {
	return []inspection.Observation{
		{CriterionID: "seam", Source: inspection.SourceNote, Evidence: seamNote, Finding: inspection.FindingComplies, Confidence: 0.9},
		{CriterionID: "label", Source: inspection.SourceNote, Evidence: labelNote, Finding: inspection.FindingComplies, Confidence: 0.8},
		{CriterionID: "barcode", Source: inspection.SourceNote, Evidence: markNote, Finding: inspection.FindingViolates, Confidence: 0.85},
	}
}

func TestRunResolvesOneStatusPerCriterionInDeclaredOrder(t *testing.T) {
	result, err := Run(criteria(), evidence(), threeResolved(), Options{})
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	if result.ProductID != "SKU-4471" {
		t.Errorf("ProductID = %q, want %q", result.ProductID, "SKU-4471")
	}

	want := []struct {
		id         string
		status     inspection.Status
		reason     inspection.Reason
		evidence   string
		confidence float64
	}{
		{id: "seam", status: inspection.StatusPass, reason: inspection.ReasonResolved, evidence: seamNote, confidence: 0.9},
		{id: "label", status: inspection.StatusPass, reason: inspection.ReasonResolved, evidence: labelNote, confidence: 0.8},
		{id: "barcode", status: inspection.StatusFail, reason: inspection.ReasonResolved, evidence: markNote, confidence: 0.85},
	}

	if len(result.Checks) != len(want) {
		t.Fatalf("len(Checks) = %d, want %d", len(result.Checks), len(want))
	}
	for i, expected := range want {
		check := result.Checks[i]
		switch {
		case check.CriterionID != expected.id:
			t.Errorf("check %d criterion_id = %q, want %q", i+1, check.CriterionID, expected.id)
		case check.Status != expected.status:
			t.Errorf("check %s status = %q, want %q", expected.id, check.Status, expected.status)
		case check.Reason != expected.reason:
			t.Errorf("check %s reason = %q, want %q", expected.id, check.Reason, expected.reason)
		case check.Evidence != expected.evidence:
			t.Errorf("check %s evidence = %q, want %q", expected.id, check.Evidence, expected.evidence)
		case check.Confidence != expected.confidence:
			t.Errorf("check %s confidence = %v, want %v", expected.id, check.Confidence, expected.confidence)
		case check.Statement != criteria().Criteria[i].Statement:
			t.Errorf("check %s statement = %q, want the declared statement", expected.id, check.Statement)
		}
	}

	if result.Summary.Pass != 2 || result.Summary.Fail != 1 || result.Summary.Review != 0 {
		t.Errorf("Summary = %+v, want 2 pass, 1 fail, 0 review", result.Summary)
	}
	if result.Summary.NeedsHumanReview {
		t.Error("NeedsHumanReview = true, want false when every criterion resolved")
	}
	if !result.Approval.Required {
		t.Error("Approval.Required = false, want true: no run of this program authorizes an inventory decision")
	}
	if result.Approval.Role == "" {
		t.Error("Approval.Role is empty, want the person the decision belongs to")
	}
	if result.Approval.Reason == "" {
		t.Error("Approval.Reason is empty, want the reason the approval is required")
	}

	if err := Verify(criteria(), result, Options{}); err != nil {
		t.Fatalf("Verify() over a run of this evaluator = %v, want nil", err)
	}
}

func TestRunNeverGuessesWhatTheEvidenceDidNotShow(t *testing.T) {
	cases := []struct {
		name           string
		criterion      string
		observations   []inspection.Observation
		wantReason     inspection.Reason
		wantEvidence   []string
		wantConfidence float64
	}{
		{
			name:         "no observation at all",
			criterion:    "barcode",
			observations: threeResolved()[:2],
			wantReason:   inspection.ReasonNoObservation,
		},
		{
			name:      "confidence below the run default",
			criterion: "barcode",
			observations: append(threeResolved()[:2], inspection.Observation{
				CriterionID: "barcode", Source: inspection.SourceNote, Evidence: markNote,
				Finding: inspection.FindingViolates, Confidence: 0.4,
			}),
			wantReason:     inspection.ReasonLowConfidence,
			wantEvidence:   []string{markNote},
			wantConfidence: 0.4,
		},
		{
			name:      "two observations that disagree",
			criterion: "barcode",
			observations: append(threeResolved()[:2],
				inspection.Observation{
					CriterionID: "barcode", Source: inspection.SourceNote, Evidence: markNote,
					Finding: inspection.FindingViolates, Confidence: 0.9,
				},
				inspection.Observation{
					CriterionID: "barcode", Source: inspection.SourceNote, Evidence: "The back panel shows a scratch",
					Finding: inspection.FindingComplies, Confidence: 0.9,
				},
			),
			wantReason:     inspection.ReasonContradictory,
			wantEvidence:   []string{markNote, "The back panel shows a scratch"},
			wantConfidence: 0.9,
		},
		{
			name:      "a reader that could not tell",
			criterion: "barcode",
			observations: append(threeResolved()[:2], inspection.Observation{
				CriterionID: "barcode", Source: inspection.SourceNote, Evidence: markNote,
				Finding: inspection.FindingIndeterminate, Confidence: 0.9,
			}),
			wantReason:     inspection.ReasonIndeterminate,
			wantEvidence:   []string{markNote},
			wantConfidence: 0.9,
		},
		{
			name:      "confidence below what the criterion itself requires",
			criterion: "label",
			observations: []inspection.Observation{
				{CriterionID: "seam", Source: inspection.SourceNote, Evidence: seamNote, Finding: inspection.FindingComplies, Confidence: 0.9},
				{CriterionID: "label", Source: inspection.SourceNote, Evidence: labelNote, Finding: inspection.FindingComplies, Confidence: 0.9},
				{CriterionID: "barcode", Source: inspection.SourceNote, Evidence: markNote, Finding: inspection.FindingViolates, Confidence: 0.85},
			},
			wantReason:     inspection.ReasonLowConfidence,
			wantEvidence:   []string{labelNote},
			wantConfidence: 0.9,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			declared := criteria()
			if tc.criterion == "label" {
				// The run default is 0.5 here, so only the criterion's own
				// 0.95 can send the 0.9 observation to review.
				declared.Criteria[1].MinConfidence = 0.95
			}

			result, err := Run(declared, evidence(), tc.observations, Options{MinConfidence: 0.5})
			if err != nil {
				t.Fatalf("Run() = %v, want nil", err)
			}

			check := checkFor(t, result, tc.criterion)
			if check.Status != inspection.StatusReview {
				t.Fatalf("check %s status = %q, want %q", tc.criterion, check.Status, inspection.StatusReview)
			}
			if check.Reason != tc.wantReason {
				t.Fatalf("check %s reason = %q, want %q", tc.criterion, check.Reason, tc.wantReason)
			}
			for _, want := range tc.wantEvidence {
				if !strings.Contains(check.Evidence, want) {
					t.Errorf("check %s evidence = %q, want it to contain %q", tc.criterion, check.Evidence, want)
				}
			}
			if check.Confidence != tc.wantConfidence {
				t.Errorf("check %s confidence = %v, want %v", tc.criterion, check.Confidence, tc.wantConfidence)
			}
			if !result.Summary.NeedsHumanReview {
				t.Error("NeedsHumanReview = false, want true when a criterion went to review")
			}
			if err := Verify(declared, result, Options{MinConfidence: 0.5}); err != nil {
				t.Fatalf("Verify() over a run of this evaluator = %v, want nil", err)
			}
		})
	}
}

func TestRunLeavesAnUnobservedRunEntirelyForReview(t *testing.T) {
	result, err := Run(criteria(), evidence(), nil, Options{})
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	if len(result.Checks) != 3 {
		t.Fatalf("len(Checks) = %d, want 3", len(result.Checks))
	}
	for _, check := range result.Checks {
		if check.Status != inspection.StatusReview || check.Reason != inspection.ReasonNoObservation {
			t.Errorf("check %s = (%q, %q), want (review, no_observation)", check.CriterionID, check.Status, check.Reason)
		}
		if check.Evidence != "" {
			t.Errorf("check %s evidence = %q, want empty when nothing was observed", check.CriterionID, check.Evidence)
		}
	}
	if result.Summary.Review != 3 {
		t.Errorf("Summary.Review = %d, want 3", result.Summary.Review)
	}
	if !result.Summary.NeedsHumanReview {
		t.Error("NeedsHumanReview = false, want true")
	}
	if !strings.Contains(result.Approval.Reason, "3") {
		t.Errorf("Approval.Reason = %q, want it to name the 3 criteria waiting for a person", result.Approval.Reason)
	}
}

func TestRunFoldsLineBreaksAndCaseInsideAQuote(t *testing.T) {
	observations := []inspection.Observation{{
		CriterionID: "seam", Source: inspection.SourceNote,
		Evidence: "LID IS\n   open on the left", Finding: inspection.FindingViolates, Confidence: 0.8,
	}}

	result, err := Run(criteria(), evidence(), observations, Options{})
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	check := checkFor(t, result, "seam")
	if check.Status != inspection.StatusFail {
		t.Fatalf("check seam status = %q, want %q", check.Status, inspection.StatusFail)
	}
	check = checkFor(t, result, "label")
	if check.Status != inspection.StatusReview {
		t.Fatalf("check label status = %q, want review: it had no observation", check.Status)
	}
}

func TestRunReadsAnImageObservationFromItsOwnImage(t *testing.T) {
	input := inspection.Input{ProductID: "SKU-4471", Images: []string{"label.png"}}
	supplied := inspection.Evidence{Images: map[string]string{"label.png": labelShot}}
	observations := []inspection.Observation{{
		CriterionID: "label", Source: inspection.SourceImage, ImageRef: "label.png",
		Location: "top-left of the label", Evidence: "legible and square to the edge in the image",
		Finding: inspection.FindingComplies, Confidence: 0.9,
	}}

	if err := input.Validate(); err != nil {
		t.Fatalf("the input fixture is invalid: %v", err)
	}

	declared := inspection.Criteria{ProductID: "SKU-4471", Criteria: []inspection.Criterion{
		{ID: "label", Statement: "The label is legible and square."},
	}}

	result, err := Run(declared, supplied, observations, Options{})
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}

	check := checkFor(t, result, "label")
	if check.Status != inspection.StatusPass {
		t.Fatalf("check label status = %q, want %q", check.Status, inspection.StatusPass)
	}
	if check.Location != "top-left of the label" {
		t.Errorf("check label location = %q, want the location of the evidence", check.Location)
	}
}

func TestRunBreaksATieByInputOrderAndPrefersTheStrongestQuote(t *testing.T) {
	first := "The seam along the lid is"
	second := "open on the left side"

	observations := []inspection.Observation{
		{CriterionID: "seam", Source: inspection.SourceNote, Evidence: first, Finding: inspection.FindingComplies, Confidence: 0.8},
		{CriterionID: "seam", Source: inspection.SourceNote, Evidence: second, Finding: inspection.FindingComplies, Confidence: 0.8},
	}

	result, err := Run(criteria(), evidence(), observations, Options{})
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	if check := checkFor(t, result, "seam"); check.Evidence != first {
		t.Fatalf("check seam evidence = %q, want the first quote at the tied confidence", check.Evidence)
	}

	observations[1].Confidence = 0.95
	result, err = Run(criteria(), evidence(), observations, Options{})
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	check := checkFor(t, result, "seam")
	if check.Evidence != second || check.Confidence != 0.95 {
		t.Fatalf("check seam = (%q, %v), want the strongest quote (%q, 0.95)", check.Evidence, check.Confidence, second)
	}
}

func TestRunUsesTheRunDefaultOnlyWhenTheCriterionDeclaresNone(t *testing.T) {
	observation := inspection.Observation{
		CriterionID: "seam", Source: inspection.SourceNote, Evidence: seamNote,
		Finding: inspection.FindingComplies, Confidence: 0.6,
	}

	declared := inspection.Criteria{ProductID: "SKU-4471", Criteria: []inspection.Criterion{
		{ID: "seam", Statement: "The seam along the lid is closed."},
	}}

	resolved, err := Run(declared, evidence(), []inspection.Observation{observation}, Options{MinConfidence: 0.5})
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	if check := checkFor(t, resolved, "seam"); check.Status != inspection.StatusPass {
		t.Fatalf("check seam status = %q, want pass under the run default of 0.5", check.Status)
	}

	// The zero Options value is the default of the package, not "no threshold".
	unresolved, err := Run(declared, evidence(), []inspection.Observation{observation}, Options{})
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	check := checkFor(t, unresolved, "seam")
	if check.Status != inspection.StatusReview || check.Reason != inspection.ReasonLowConfidence {
		t.Fatalf("check seam = (%q, %q), want (review, low_confidence) under the 0.7 default", check.Status, check.Reason)
	}
}

func TestRunRefusesInputItCannotTrust(t *testing.T) {
	cases := []struct {
		name         string
		criteria     inspection.Criteria
		evidence     inspection.Evidence
		observations []inspection.Observation
		want         string
	}{
		{
			name:         "criteria without a product",
			criteria:     inspection.Criteria{Criteria: criteria().Criteria},
			evidence:     evidence(),
			observations: threeResolved(),
			want:         "product_id is required",
		},
		{
			name:         "an input with nothing supplied",
			criteria:     criteria(),
			evidence:     inspection.Evidence{},
			observations: nil,
			want:         "notes or at least one image is required",
		},
		{
			name:     "an observation about an undeclared criterion",
			criteria: criteria(),
			evidence: evidence(),
			observations: append(threeResolved(), inspection.Observation{
				CriterionID: "colour", Source: inspection.SourceNote, Evidence: seamNote,
				Finding: inspection.FindingComplies, Confidence: 0.9,
			}),
			want: `unknown criterion "colour"`,
		},
		{
			name:     "an invented quote",
			criteria: criteria(),
			evidence: evidence(),
			observations: []inspection.Observation{{
				CriterionID: "seam", Source: inspection.SourceNote,
				Evidence: "the seam is welded shut", Finding: inspection.FindingComplies, Confidence: 0.9,
			}},
			want: `criterion "seam": observation 1 quotes text the notes do not contain`,
		},
		{
			name:     "an image quote that is not in that image",
			criteria: criteria(),
			evidence: evidence(),
			observations: []inspection.Observation{{
				CriterionID: "label", Source: inspection.SourceImage, ImageRef: "label.png",
				Location: "top-left", Evidence: markNote, Finding: inspection.FindingViolates, Confidence: 0.9,
			}},
			want: `criterion "label": observation 1 quotes text the description of "label.png" does not contain`,
		},
		{
			name:     "an image observation without a location",
			criteria: criteria(),
			evidence: evidence(),
			observations: []inspection.Observation{{
				CriterionID: "label", Source: inspection.SourceImage, ImageRef: "label.png",
				Evidence: labelShot, Finding: inspection.FindingComplies, Confidence: 0.9,
			}},
			want: "location is required when the observation comes from the image",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := Run(tc.criteria, tc.evidence, tc.observations, Options{})
			if err == nil {
				t.Fatalf("Run() = nil, want an error naming %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Run() = %q, want it to contain %q", err, tc.want)
			}
			if result.ProductID != "" || result.Checks != nil {
				t.Errorf("Run() returned %+v alongside an error, want the zero result", result)
			}
		})
	}
}

func TestVerifyCatchesEveryRuleItClaimsToCheck(t *testing.T) {
	clean, err := Run(criteria(), evidence(), threeResolved(), Options{})
	if err != nil {
		t.Fatalf("Run() = %v, want nil", err)
	}
	if err := Verify(criteria(), clean, Options{}); err != nil {
		t.Fatalf("Verify() over a clean result = %v, want nil", err)
	}

	cases := []struct {
		name   string
		mutate func(*inspection.Result)
		want   string
	}{
		{
			name: "a criterion without a check",
			mutate: func(r *inspection.Result) {
				r.Checks = r.Checks[:2]
				r.Summary.Pass = 1
			},
			want: "has 2 checks for 3 criteria",
		},
		{
			name: "a check for a criterion that was never declared",
			mutate: func(r *inspection.Result) {
				r.Checks[0].CriterionID = "colour"
			},
			want: `check 1: criterion_id "colour" is not a declared criterion`,
		},
		{
			name: "the same criterion twice",
			mutate: func(r *inspection.Result) {
				r.Checks[1].CriterionID = "seam"
			},
			want: `check 2: criterion "seam" appears more than once`,
		},
		{
			name: "checks in a different order than the criteria",
			mutate: func(r *inspection.Result) {
				r.Checks[0], r.Checks[1] = r.Checks[1], r.Checks[0]
			},
			want: `check 1 is for criterion "label", want "seam"`,
		},
		{
			name: "a statement that drifted from the criterion",
			mutate: func(r *inspection.Result) {
				r.Checks[0].Statement = "The seam is fine, probably."
			},
			want: "check 1: statement does not match the declared criterion",
		},
		{
			name: "a status outside the three",
			mutate: func(r *inspection.Result) {
				r.Checks[0].Status = "approved"
			},
			want: `check 1: status "approved" is not one of pass, fail, review`,
		},
		{
			name: "a resolved check with a review reason",
			mutate: func(r *inspection.Result) {
				r.Checks[0].Reason = inspection.ReasonNoObservation
			},
			want: `check 1: a resolved check needs reason "resolved", got "no_observation"`,
		},
		{
			name: "a resolved check without evidence",
			mutate: func(r *inspection.Result) {
				r.Checks[0].Evidence = ""
			},
			want: "check 1: a resolved check needs the evidence it rests on",
		},
		{
			name: "a resolved check below the confidence it requires",
			mutate: func(r *inspection.Result) {
				r.Checks[0].Confidence = 0.2
			},
			want: "check 1: resolved at confidence 0.2, below the 0.7 this criterion requires",
		},
		{
			name: "a review without a reason this evaluator produces",
			mutate: func(r *inspection.Result) {
				r.Checks[2].Status = inspection.StatusReview
				r.Checks[2].Reason = inspection.ReasonResolved
				r.Summary.Fail = 0
				r.Summary.Review = 1
				r.Summary.NeedsHumanReview = true
			},
			want: `check 3: review reason "resolved" is not one of`,
		},
		{
			name: "a review that carries no evidence at all",
			mutate: func(r *inspection.Result) {
				r.Checks[2].Status = inspection.StatusReview
				r.Checks[2].Reason = inspection.ReasonLowConfidence
				r.Checks[2].Evidence = ""
				r.Summary.Fail = 0
				r.Summary.Review = 1
				r.Summary.NeedsHumanReview = true
			},
			want: `check 3: a review with reason "low_confidence" needs evidence`,
		},
		{
			name: "a no_observation review that carries evidence anyway",
			mutate: func(r *inspection.Result) {
				r.Checks[2].Status = inspection.StatusReview
				r.Checks[2].Reason = inspection.ReasonNoObservation
				r.Summary.Fail = 0
				r.Summary.Review = 1
				r.Summary.NeedsHumanReview = true
			},
			want: "check 3: no_observation cannot carry evidence",
		},
		{
			name: "a summary that does not count the checks",
			mutate: func(r *inspection.Result) {
				r.Summary.Pass = 3
			},
			want: "summary pass=3 fail=1 review=0 does not match the checks",
		},
		{
			name: "a run that hides the criteria waiting for a person",
			mutate: func(r *inspection.Result) {
				r.Checks[2].Status = inspection.StatusReview
				r.Checks[2].Reason = inspection.ReasonIndeterminate
				r.Summary.Fail = 0
				r.Summary.Review = 1
			},
			want: "summary needs_human_review is false while 1 checks are for review",
		},
		{
			name: "a result that claims an approval is optional",
			mutate: func(r *inspection.Result) {
				r.Approval.Required = false
			},
			want: "approval.required is false: no run of this program authorizes an inventory decision",
		},
		{
			name: "a result for another product",
			mutate: func(r *inspection.Result) {
				r.ProductID = "SKU-0000"
			},
			want: `result product_id "SKU-0000" does not match criteria product_id "SKU-4471"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tampered := clone(clean)
			tc.mutate(&tampered)

			err := Verify(criteria(), tampered, Options{})
			if err == nil {
				t.Fatalf("Verify() = nil, want an error naming %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Verify() = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

// checkFor returns the check of one criterion, failing the test when the run
// produced none.
func checkFor(t *testing.T, result inspection.Result, criterionID string) inspection.Check {
	t.Helper()

	for _, check := range result.Checks {
		if check.CriterionID == criterionID {
			return check
		}
	}
	t.Fatalf("no check for criterion %q in %+v", criterionID, result.Checks)
	return inspection.Check{}
}

// clone returns a result the caller can tamper with without touching the
// original, including the slice backing array.
func clone(result inspection.Result) inspection.Result {
	copied := result
	copied.Checks = append([]inspection.Check(nil), result.Checks...)
	return copied
}

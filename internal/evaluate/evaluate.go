// Package evaluate derives one status per declared criterion from the
// observations a reader extracted, and never guesses.
//
// This package is the decision path, and it is ordinary deterministic code.
// Nothing here calls a model, reads an image, or trusts an observation: an
// observation has to quote text that was actually supplied (the quote is
// re-checked here), and the status follows from a small set of rules:
//
//   - no usable observation leaves the criterion for review;
//   - an observation the reader could not decide on leaves it for review;
//   - two observations that disagree leave it for review, whatever their
//     confidence: agreement is not a majority vote;
//   - an observation below the confidence the criterion requires leaves it for
//     review;
//   - otherwise the criterion resolves to pass or fail on the strongest
//     agreeing quote.
//
// Run also verifies its own result with Verify before returning it, so a result
// that broke one of these rules is reported as a defect in this repository
// rather than handed to a caller that would believe it.
package evaluate

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/GhandyP/product-image-inspection/internal/inspection"
)

// DefaultMinConfidence is the confidence an observation needs before a
// criterion may resolve, for every criterion that does not declare its own.
const DefaultMinConfidence = 0.7

// Options configures one run.
type Options struct {
	// MinConfidence is the run default. Zero means DefaultMinConfidence; a
	// criterion that declares min_confidence always wins over it.
	MinConfidence float64
}

// minConfidence returns the threshold this run uses.
func (o Options) minConfidence() float64 {
	if o.MinConfidence > 0 {
		return o.MinConfidence
	}
	return DefaultMinConfidence
}

// validate refuses a threshold that could not resolve anything.
func (o Options) validate() error {
	if math.IsNaN(o.MinConfidence) || o.MinConfidence < 0 || o.MinConfidence > 1 {
		return fmt.Errorf("min_confidence must be in (0, 1], got %v", o.MinConfidence)
	}
	return nil
}

// Run derives the status of every declared criterion.
//
// It returns the zero result alongside an error, so a caller never has half an
// inspection to act on: either every criterion has a status, or nothing does.
func Run(
	criteria inspection.Criteria,
	evidence inspection.Evidence,
	observations []inspection.Observation,
	options Options,
) (inspection.Result, error) {
	if err := options.validate(); err != nil {
		return inspection.Result{}, err
	}
	if err := criteria.Validate(); err != nil {
		return inspection.Result{}, err
	}
	if err := evidence.Validate(); err != nil {
		return inspection.Result{}, err
	}
	if err := inspection.ValidateObservations(criteria, inputOf(evidence), observations); err != nil {
		return inspection.Result{}, err
	}
	if err := verifyQuotes(evidence, observations); err != nil {
		return inspection.Result{}, err
	}

	runDefault := options.minConfidence()
	checks := make([]inspection.Check, 0, len(criteria.Criteria))
	summary := inspection.Summary{}

	for _, criterion := range criteria.Criteria {
		check := checkOf(criterion, runDefault, observations)
		checks = append(checks, check)

		switch check.Status {
		case inspection.StatusPass:
			summary.Pass++
		case inspection.StatusFail:
			summary.Fail++
		case inspection.StatusReview:
			summary.Review++
		}
	}
	summary.NeedsHumanReview = summary.Review > 0

	result := inspection.Result{
		ProductID: criteria.ProductID,
		Checks:    checks,
		Summary:   summary,
		Approval:  approvalFor(summary, len(criteria.Criteria)),
	}

	// The evaluator is built to make this impossible, so a failure here is a
	// defect in this repository, not a caller's mistake.
	if err := Verify(criteria, result, options); err != nil {
		return inspection.Result{}, fmt.Errorf("the evaluator produced a result that fails its own verification: %w", err)
	}
	return result, nil
}

// checkOf derives the status of one criterion from the observations about it.
func checkOf(criterion inspection.Criterion, runDefault float64, observations []inspection.Observation) inspection.Check {
	check := inspection.Check{CriterionID: criterion.ID, Statement: criterion.Statement}

	mine := make([]inspection.Observation, 0, len(observations))
	for _, observation := range observations {
		if observation.CriterionID == criterion.ID {
			mine = append(mine, observation)
		}
	}
	if len(mine) == 0 {
		return resolve(check, inspection.StatusReview, inspection.ReasonNoObservation, "", "", 0)
	}

	complies := make([]inspection.Observation, 0, len(mine))
	violates := make([]inspection.Observation, 0, len(mine))
	for _, observation := range mine {
		switch observation.Finding {
		case inspection.FindingComplies:
			complies = append(complies, observation)
		case inspection.FindingViolates:
			violates = append(violates, observation)
		default:
			// A reader that said "I cannot tell" is not evidence for anything.
			// The first one is reported, so the row names something a person
			// can go and look at.
			return resolve(check, inspection.StatusReview, inspection.ReasonIndeterminate,
				observation.Evidence, observation.Location, observation.Confidence)
		}
	}

	// Two readers that disagree are not settled by counting: a person has to
	// look. Both quotes travel in the row, because the conflict is the finding.
	if len(complies) > 0 && len(violates) > 0 {
		forAgreeing := strongest(complies)
		forViolation := strongest(violates)

		return resolve(check, inspection.StatusReview, inspection.ReasonContradictory,
			forAgreeing.Evidence+" | "+forViolation.Evidence, "", math.Max(forAgreeing.Confidence, forViolation.Confidence))
	}

	best := strongest(mine)
	if best.Confidence < criterion.MinimumConfidence(runDefault) {
		return resolve(check, inspection.StatusReview, inspection.ReasonLowConfidence,
			best.Evidence, best.Location, best.Confidence)
	}

	status := inspection.StatusPass
	if best.Finding == inspection.FindingViolates {
		status = inspection.StatusFail
	}
	return resolve(check, status, inspection.ReasonResolved,
		best.Evidence, best.Location, best.Confidence)
}

// resolve fills a check, so every path through this package sets the same
// fields and a reason is never left implicit.
func resolve(
	check inspection.Check,
	status inspection.Status,
	reason inspection.Reason,
	evidence, location string,
	confidence float64,
) inspection.Check {
	check.Status = status
	check.Reason = reason
	check.Evidence = evidence
	check.Location = location
	check.Confidence = confidence
	return check
}

// strongest returns the observation with the highest confidence, breaking a tie
// by input order so the same input always produces the same row.
func strongest(observations []inspection.Observation) inspection.Observation {
	best := observations[0]
	for _, observation := range observations[1:] {
		if observation.Confidence > best.Confidence {
			best = observation
		}
	}
	return best
}

// approvalFor states, inside the result, whose decision the result feeds.
func approvalFor(summary inspection.Summary, criteria int) inspection.Approval {
	reason := "deciding what to do with inventory is not this program's call, " +
		"so a qualified person has to approve the decision this result informs"
	if summary.NeedsHumanReview {
		reason = fmt.Sprintf(
			"%d of %d criteria still need a person to read the evidence, and deciding what to do with "+
				"inventory is not this program's call",
			summary.Review, criteria)
	}

	return inspection.Approval{
		Required: true,
		Role:     "qualified human inspector",
		Reason:   reason,
	}
}

// verifyQuotes re-checks that every observation quotes the source it claims.
//
// This is the rule that keeps a status backed by text somebody can go and read.
// Text the source does not contain is refused here, before any status exists:
// an invented quote is not weak evidence, it is not evidence at all.
func verifyQuotes(evidence inspection.Evidence, observations []inspection.Observation) error {
	for position, observation := range observations {
		text, supplied := evidence.EvidenceFor(observation.Source, observation.ImageRef)
		if !supplied {
			return fmt.Errorf("criterion %q: observation %d claims a source this run did not supply: %s %q",
				observation.CriterionID, position+1, observation.Source, observation.ImageRef)
		}
		if observation.EvidenceFoundIn(text) {
			continue
		}
		if observation.Source == inspection.SourceImage {
			return fmt.Errorf("criterion %q: observation %d quotes text the description of %q does not contain: %q",
				observation.CriterionID, position+1, observation.ImageRef, observation.Evidence)
		}
		return fmt.Errorf("criterion %q: observation %d quotes text the notes do not contain: %q",
			observation.CriterionID, position+1, observation.Evidence)
	}
	return nil
}

// inputOf projects the evidence onto the input shape the shared observation
// rules validate against, so those rules exist in exactly one place.
func inputOf(evidence inspection.Evidence) inspection.Input {
	return inspection.Input{
		Notes:  evidence.Notes,
		Images: slices.Sorted(maps.Keys(evidence.Images)),
	}
}

// Verify asks independently whether a result keeps every rule this package
// promises, without trusting that it came from Run.
//
// It re-derives the summary from the checks, re-reads every criterion, and
// refuses a result that lost a criterion, invented one, reordered them, invented
// a status, resolved below the confidence the criterion requires, reviewed
// without a reason, or claimed that an approval is optional. The command line
// reports a failure here as a defect in this repository.
func Verify(criteria inspection.Criteria, result inspection.Result, options Options) error {
	problems := inspection.Problems{}
	if err := options.validate(); err != nil {
		problems = append(problems, err.Error())
	}

	if result.ProductID != criteria.ProductID {
		problems = append(problems, fmt.Sprintf(
			"result product_id %q does not match criteria product_id %q", result.ProductID, criteria.ProductID))
	}
	if len(result.Checks) != len(criteria.Criteria) {
		problems = append(problems, fmt.Sprintf(
			"result has %d checks for %d criteria", len(result.Checks), len(criteria.Criteria)))
	}

	declared := make(map[string]bool, len(criteria.Criteria))
	for _, criterion := range criteria.Criteria {
		declared[criterion.ID] = true
	}

	runDefault := options.minConfidence()
	seen := make(map[string]bool, len(result.Checks))

	for position, check := range result.Checks {
		number := position + 1

		if !declared[check.CriterionID] {
			problems = append(problems, fmt.Sprintf(
				"check %d: criterion_id %q is not a declared criterion", number, check.CriterionID))
			continue
		}
		if seen[check.CriterionID] {
			problems = append(problems, fmt.Sprintf(
				"check %d: criterion %q appears more than once", number, check.CriterionID))
			continue
		}
		seen[check.CriterionID] = true

		if position >= len(criteria.Criteria) {
			continue
		}
		criterion := criteria.Criteria[position]

		if check.CriterionID != criterion.ID {
			problems = append(problems, fmt.Sprintf(
				"check %d is for criterion %q, want %q", number, check.CriterionID, criterion.ID))
		}
		if check.Statement != criterion.Statement {
			problems = append(problems, fmt.Sprintf(
				"check %d: statement does not match the declared criterion", number))
		}

		switch check.Status {
		case inspection.StatusPass, inspection.StatusFail:
			if check.Reason != inspection.ReasonResolved {
				problems = append(problems, fmt.Sprintf(
					"check %d: a resolved check needs reason %q, got %q", number, inspection.ReasonResolved, check.Reason))
			}
			if strings.TrimSpace(check.Evidence) == "" {
				problems = append(problems, fmt.Sprintf(
					"check %d: a resolved check needs the evidence it rests on", number))
			}
			if required := criterion.MinimumConfidence(runDefault); check.Confidence < required {
				problems = append(problems, fmt.Sprintf(
					"check %d: resolved at confidence %v, below the %v this criterion requires",
					number, check.Confidence, required))
			}
		case inspection.StatusReview:
			switch check.Reason {
			case inspection.ReasonNoObservation:
				if strings.TrimSpace(check.Evidence) != "" {
					problems = append(problems, fmt.Sprintf(
						"check %d: no_observation cannot carry evidence", number))
				}
			case inspection.ReasonLowConfidence, inspection.ReasonContradictory, inspection.ReasonIndeterminate:
				if strings.TrimSpace(check.Evidence) == "" {
					problems = append(problems, fmt.Sprintf(
						"check %d: a review with reason %q needs evidence", number, check.Reason))
				}
			default:
				problems = append(problems, fmt.Sprintf(
					"check %d: review reason %q is not one of no_observation, low_confidence, "+
						"contradictory_observations, indeterminate_observation", number, check.Reason))
			}
		default:
			problems = append(problems, fmt.Sprintf(
				"check %d: status %q is not one of pass, fail, review", number, check.Status))
		}

		if math.IsNaN(check.Confidence) || check.Confidence < 0 || check.Confidence > 1 {
			problems = append(problems, fmt.Sprintf(
				"check %d: confidence must be in [0, 1], got %v", number, check.Confidence))
		}
	}

	pass, fail, review := 0, 0, 0
	for _, check := range result.Checks {
		switch check.Status {
		case inspection.StatusPass:
			pass++
		case inspection.StatusFail:
			fail++
		case inspection.StatusReview:
			review++
		}
	}
	if result.Summary.Pass != pass || result.Summary.Fail != fail || result.Summary.Review != review {
		problems = append(problems, fmt.Sprintf(
			"summary pass=%d fail=%d review=%d does not match the checks",
			result.Summary.Pass, result.Summary.Fail, result.Summary.Review))
	}
	switch {
	case !result.Summary.NeedsHumanReview && review > 0:
		problems = append(problems, fmt.Sprintf(
			"summary needs_human_review is false while %d checks are for review", review))
	case result.Summary.NeedsHumanReview && review == 0:
		problems = append(problems, "summary needs_human_review is true while no check is for review")
	}

	if !result.Approval.Required {
		problems = append(problems, "approval.required is false: no run of this program authorizes an inventory decision")
	}
	if strings.TrimSpace(result.Approval.Role) == "" {
		problems = append(problems, "approval.role is empty, want the person the decision belongs to")
	}
	if strings.TrimSpace(result.Approval.Reason) == "" {
		problems = append(problems, "approval.reason is empty, want the reason an approval is required")
	}

	return problems.OrNil()
}

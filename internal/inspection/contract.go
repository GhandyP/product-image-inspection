// Package inspection defines the contract of one product inspection: the
// criteria a product is checked against, the notes or images that were
// supplied, the observations a reader extracted from them, and the status each
// criterion ends up with.
//
// Nothing in this package calls a model or reads an image. It holds the shapes
// and the static rules, so that every other package — the evaluator, the note
// reader, the vision adapter, the command line — agrees on one vocabulary and
// on what a malformed input is.
//
// Two rules of this contract carry the promise of the repository:
//
//   - Every observation must quote the text it came from (EvidenceFoundIn).
//     Evidence that was not in the source is not evidence.
//   - An image observation must name where in the image the evidence is
//     (Location). A defect without a location cannot be reviewed by a person.
package inspection

import (
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"
)

// Criteria is the configuration a product is judged against. It is the same
// file for every inspection of that product: changing what an inspection checks
// means changing this contract, not the prompt.
type Criteria struct {
	ProductID string      `json:"product_id"`
	Criteria  []Criterion `json:"criteria"`
}

// Criterion is one thing an inspection checks, in the words a person would use.
type Criterion struct {
	ID        string `json:"id"`
	Statement string `json:"statement"`
	// MinConfidence is the confidence an observation needs before this
	// criterion may resolve. Zero means "use the run default", which is what
	// makes a value outside (0, 1] a refusal rather than a surprise.
	MinConfidence float64 `json:"min_confidence,omitempty"`
}

// MinimumConfidence returns the confidence this criterion needs, falling back
// to the run default when the criterion declares none.
func (c Criterion) MinimumConfidence(runDefault float64) float64 {
	if c.MinConfidence > 0 {
		return c.MinConfidence
	}
	return runDefault
}

// Input is what was supplied for one inspection: the text notes an inspector
// wrote, the images that came with them, or both.
type Input struct {
	ProductID string   `json:"product_id"`
	Notes     string   `json:"notes,omitempty"`
	Images    []string `json:"images,omitempty"`
}

// Evidence is the text an observation has to quote, keyed by where it came
// from. Notes holds the inspector's text; Images maps each supplied image ref
// to the factual description that was made of it.
//
// It exists so that a quote can be checked against the exact source it claims:
// an observation from the image "back.png" must be a quote from the description
// of back.png, not from the inspector's notes and not from another image.
type Evidence struct {
	Notes  string            `json:"notes,omitempty"`
	Images map[string]string `json:"images,omitempty"`
}

// EvidenceFor returns the text an observation with this source and image ref
// has to quote. The second result is false when that source was not supplied.
func (e Evidence) EvidenceFor(source Source, imageRef string) (string, bool) {
	switch source {
	case SourceNote:
		if strings.TrimSpace(e.Notes) == "" {
			return "", false
		}
		return e.Notes, true
	case SourceImage:
		description, ok := e.Images[imageRef]
		if !ok || strings.TrimSpace(description) == "" {
			return "", false
		}
		return description, true
	}
	return "", false
}

// Validate applies every static rule of the evidence contract: an inspection
// needs something to read, and every supplied image needs a description that
// an observation can quote.
func (e Evidence) Validate() error {
	problems := Problems{}

	if strings.TrimSpace(e.Notes) == "" && len(e.Images) == 0 {
		problems = append(problems, "notes or at least one image is required")
	}
	for _, ref := range slices.Sorted(maps.Keys(e.Images)) {
		if strings.TrimSpace(e.Images[ref]) == "" {
			problems = append(problems, fmt.Sprintf("image %q has no description", ref))
		}
	}

	return problems.OrNil()
}

// Source names where an observation came from. It matters because an image
// observation has to say where in the image the evidence is, and that is a
// requirement a note observation cannot satisfy.
type Source string

// The sources an observation may come from.
const (
	SourceNote  Source = "note"
	SourceImage Source = "image"
)

// Finding is what a reader saw for one criterion, before any status exists.
type Finding string

// The findings a reader may report. Indeterminate is the honest answer to "I
// cannot tell", and it is what sends a criterion to review.
const (
	FindingComplies      Finding = "complies"
	FindingViolates      Finding = "violates"
	FindingIndeterminate Finding = "indeterminate"
)

// Observation is one structured statement about one criterion, extracted from
// the notes or from an image description. It is an observation, not a verdict:
// the evaluator decides what a set of observations means.
type Observation struct {
	CriterionID string `json:"criterion_id"`
	Source      Source `json:"source"`
	// Evidence is text quoted from the source. It is checked against that
	// source, so an invented quote is refused rather than believed.
	Evidence string `json:"evidence"`
	// Location is where in the image the evidence is, in words a person can
	// follow. It is required for image observations and ignored otherwise.
	Location string `json:"location,omitempty"`
	// ImageRef names which supplied image the evidence is in. It is required
	// for image observations and refused on note observations, so a reader
	// cannot quietly attribute a note to an image or the other way round.
	ImageRef   string  `json:"image_ref,omitempty"`
	Finding    Finding `json:"finding"`
	Confidence float64 `json:"confidence"`
}

// Status is the outcome of one criterion after evaluation. It is exactly one of
// three values, and the evaluator produces one per declared criterion.
type Status string

// The statuses a criterion may resolve to. There is no fourth status and no
// status that authorizes an inventory decision.
const (
	// StatusPass means every usable observation agreed the criterion holds.
	StatusPass Status = "pass"
	// StatusFail means every usable observation agreed the criterion is broken.
	StatusFail Status = "fail"
	// StatusReview means the evidence was missing, weak, contradictory or
	// inconclusive, so a person has to look.
	StatusReview Status = "review"
)

// Reason records why a criterion resolved the way it did, so a reader does not
// have to guess what review means in a particular row.
type Reason string

// The reasons a check may carry.
const (
	ReasonResolved      Reason = "resolved"
	ReasonNoObservation Reason = "no_observation"
	ReasonLowConfidence Reason = "low_confidence"
	ReasonContradictory Reason = "contradictory_observations"
	ReasonIndeterminate Reason = "indeterminate_observation"
)

// Check is the result for one criterion: the status, the evidence it rests on,
// and the reason, in one row.
type Check struct {
	CriterionID string  `json:"criterion_id"`
	Statement   string  `json:"statement"`
	Status      Status  `json:"status"`
	Reason      Reason  `json:"reason"`
	Evidence    string  `json:"evidence,omitempty"`
	Location    string  `json:"location,omitempty"`
	Confidence  float64 `json:"confidence"`
}

// Summary counts the statuses of one run.
type Summary struct {
	Pass   int `json:"pass"`
	Fail   int `json:"fail"`
	Review int `json:"review"`
	// NeedsHumanReview is true when at least one criterion could not be
	// resolved from the evidence.
	NeedsHumanReview bool `json:"needs_human_review"`
}

// Approval restates, inside every result, that the decision this result feeds
// belongs to a person. No value of this program releases, rejects, or relabels
// inventory, so the field is always required and never optional.
type Approval struct {
	Required bool   `json:"required"`
	Role     string `json:"role"`
	Reason   string `json:"reason"`
}

// Result is the complete outcome of one inspection: one check per declared
// criterion, in the order the criteria were declared.
type Result struct {
	ProductID string   `json:"product_id"`
	Checks    []Check  `json:"checks"`
	Summary   Summary  `json:"summary"`
	Approval  Approval `json:"approval"`
}

// Problems is the complete list of static rules an input violates.
//
// It is returned whole, not one problem at a time, so a caller fixing a
// contract or a fixture sees every mistake in a single run.
type Problems []string

// Error renders the problems on one line.
func (p Problems) Error() string {
	return strings.Join(p, "; ")
}

// OrNil returns nil when nothing was recorded, so a caller can return an empty
// Problems value as a nil error.
func (p Problems) OrNil() error {
	if len(p) == 0 {
		return nil
	}
	return p
}

// Validate applies every static rule of the criteria contract.
func (c Criteria) Validate() error {
	problems := Problems{}

	if strings.TrimSpace(c.ProductID) == "" {
		problems = append(problems, "product_id is required")
	}
	if len(c.Criteria) == 0 {
		problems = append(problems, "at least one criterion is required")
	}

	seen := make(map[string]bool, len(c.Criteria))
	for position, criterion := range c.Criteria {
		number := position + 1
		subject := fmt.Sprintf("criterion %d", number)

		switch {
		case strings.TrimSpace(criterion.ID) == "":
			problems = append(problems, subject+": id is required")
		case seen[criterion.ID]:
			problems = append(problems, fmt.Sprintf("duplicate criterion %q", criterion.ID))
		default:
			seen[criterion.ID] = true
		}
		if strings.TrimSpace(criterion.ID) != "" {
			subject = fmt.Sprintf("criterion %q", criterion.ID)
		}

		if strings.TrimSpace(criterion.Statement) == "" {
			problems = append(problems, subject+": statement is required")
		}
		if math.IsNaN(criterion.MinConfidence) || criterion.MinConfidence < 0 || criterion.MinConfidence > 1 {
			problems = append(problems, fmt.Sprintf(
				"%s: min_confidence must be in (0, 1], got %v", subject, criterion.MinConfidence))
		}
	}

	return problems.OrNil()
}

// Validate applies every static rule of the input contract.
func (i Input) Validate() error {
	problems := Problems{}

	if strings.TrimSpace(i.ProductID) == "" {
		problems = append(problems, "product_id is required")
	}
	if strings.TrimSpace(i.Notes) == "" && len(i.Images) == 0 {
		problems = append(problems, "notes or at least one image is required")
	}

	seen := make(map[string]bool, len(i.Images))
	for position, image := range i.Images {
		switch {
		case strings.TrimSpace(image) == "":
			problems = append(problems, fmt.Sprintf("images[%d]: image ref is required", position))
		case seen[image]:
			problems = append(problems, fmt.Sprintf("duplicate image ref %q", image))
		default:
			seen[image] = true
		}
	}

	return problems.OrNil()
}

// ValidateObservations applies every static rule of the observation contract
// against the criteria those observations claim to be about and the input they
// claim to have come from.
//
// An empty set is valid: it is the honest input of a reader that found nothing,
// and it makes every criterion resolve to review.
func ValidateObservations(criteria Criteria, input Input, observations []Observation) error {
	declared := make(map[string]bool, len(criteria.Criteria))
	for _, criterion := range criteria.Criteria {
		declared[criterion.ID] = true
	}
	supplied := make(map[string]bool, len(input.Images))
	for _, image := range input.Images {
		supplied[image] = true
	}

	problems := Problems{}
	for position, observation := range observations {
		where := fmt.Sprintf("observation %d", position+1)

		switch {
		case strings.TrimSpace(observation.CriterionID) == "":
			problems = append(problems, where+": criterion_id is required")
			continue
		case !declared[observation.CriterionID]:
			problems = append(problems, fmt.Sprintf("%s: unknown criterion %q", where, observation.CriterionID))
			continue
		}

		subject := fmt.Sprintf("criterion %q: %s", observation.CriterionID, where)

		if strings.TrimSpace(observation.Evidence) == "" {
			problems = append(problems, subject+": evidence is required")
		}

		switch observation.Source {
		case SourceNote:
			if strings.TrimSpace(observation.ImageRef) != "" {
				problems = append(problems, subject+": image_ref is only for image observations")
			}
		case SourceImage:
			if strings.TrimSpace(observation.Location) == "" {
				problems = append(problems, subject+": location is required when the observation comes from the image")
			}
			switch {
			case strings.TrimSpace(observation.ImageRef) == "":
				problems = append(problems, subject+": image_ref is required when the observation comes from the image")
			case !supplied[observation.ImageRef]:
				problems = append(problems, fmt.Sprintf(
					"%s: image %q is not one of the supplied images", subject, observation.ImageRef))
			}
		default:
			problems = append(problems, fmt.Sprintf("%s: source %q is not one of note, image", subject, observation.Source))
		}

		switch observation.Finding {
		case FindingComplies, FindingViolates, FindingIndeterminate:
		default:
			problems = append(problems, fmt.Sprintf(
				"%s: finding %q is not one of complies, violates, indeterminate", subject, observation.Finding))
		}

		if math.IsNaN(observation.Confidence) || observation.Confidence < 0 || observation.Confidence > 1 {
			problems = append(problems, fmt.Sprintf(
				"%s: confidence must be in [0, 1], got %v", subject, observation.Confidence))
		}
	}

	return problems.OrNil()
}

// EvidenceFoundIn reports whether this observation's evidence is a real quote
// from text.
//
// The comparison folds case and collapses whitespace, so a quote that a reader
// carried across a line break still counts; it does not paraphrase. A model or
// a person who writes evidence the source does not contain fails this check,
// which is what keeps a status backed by text somebody can go and read.
func (o Observation) EvidenceFoundIn(text string) bool {
	needle := normalizeQuote(o.Evidence)
	if needle == "" {
		return false
	}
	return strings.Contains(normalizeQuote(text), needle)
}

// normalizeQuote folds case and collapses every run of whitespace to one space.
func normalizeQuote(text string) string {
	return strings.ToLower(strings.Join(strings.Fields(text), " "))
}

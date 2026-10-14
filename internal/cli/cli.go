// Package cli wires the whole inspection behind one command: read the criteria,
// supply the evidence (notes, described images, or both), read the observations
// (or take them from a file), evaluate, and print.
//
// It never writes a file and never decides anything about inventory. The only
// things it reads from disk are the files the caller named.
//
// Exit codes are part of the contract, because a caller scripting this command
// has to tell "the evidence was not enough" apart from "your request was
// refused" and from "this repository is broken".
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/GhandyP/product-image-inspection/internal/agent"
	"github.com/GhandyP/product-image-inspection/internal/evaluate"
	"github.com/GhandyP/product-image-inspection/internal/inspection"
	"github.com/GhandyP/product-image-inspection/internal/llm"
	"github.com/GhandyP/product-image-inspection/internal/vision"
)

// Exit codes. They are documented in the README, and they are ordered by what a
// caller should do about them.
const (
	// ExitOK means every criterion resolved from the evidence.
	ExitOK = 0
	// ExitReview means the run completed and at least one criterion needs a
	// person to read the evidence. It is not an error in the request: it is the
	// answer the evidence supported.
	ExitReview = 1
	// ExitUsage means the command line itself was wrong.
	ExitUsage = 2
	// ExitRefused means an input, or an answer, was refused: the criteria, a
	// file that could not be read, or observations that do not quote their
	// source.
	ExitRefused = 3
	// ExitModel means the model could not be used at all: no key, or no usable
	// answer. The request never reached the point of being evaluated.
	ExitModel = 4
	// ExitDefect means the result broke a promise this repository makes about
	// its own output. It is a defect in this repository, never a mistake by the
	// caller.
	ExitDefect = 5
)

// Deps carries the collaborators a run needs, so a caller — and a test — can
// replace them. A nil reader or describer is only a problem when a run actually
// needs one: evaluating observations from a file needs neither.
type Deps struct {
	// NewReader builds the reader used when no -observations file was given.
	NewReader func(ctx context.Context) (agent.Reader, error)
	// NewDescriber builds the describer used for -image when no -descriptions
	// file was given.
	NewDescriber func(ctx context.Context) (vision.Describer, error)
	// Verify re-checks the result independently. A nil value uses
	// evaluate.Verify; it is a field so the defect path can be exercised
	// without corrupting the evaluator.
	Verify func(criteria inspection.Criteria, result inspection.Result, options evaluate.Options) error
}

// Run parses the command line, runs the inspection, prints the result, and
// returns the process exit code.
func Run(ctx context.Context, args []string, deps Deps, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("product-image-inspection", flag.ContinueOnError)
	flags.SetOutput(stderr)

	criteriaPath := flags.String("criteria", "", "path to the criteria file in JSON; the contract of the inspection")
	notesPath := flags.String("notes", "", "path to a text file with the inspector's notes")
	images := &stringList{}
	flags.Var(images, "image", "path to an image of the product; repeat for more than one")
	descriptionsPath := flags.String("descriptions", "",
		"path to a fixture that maps an image reference to its description; describes the images without a model")
	observationsPath := flags.String("observations", "",
		"path to a file with observations in JSON; evaluates them without a model")
	threshold := flags.Float64("threshold", 0,
		"confidence an observation needs when its criterion declares none (default 0.7)")
	asJSON := flags.Bool("json", false, "print the result as JSON instead of the preview")

	flags.Usage = func() { usage(stderr, flags) }

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitOK
		}
		return ExitUsage
	}

	switch {
	case flags.NArg() > 0:
		fmt.Fprintf(stderr, "unexpected argument %q\n\n", flags.Arg(0))
		usage(stderr, flags)
		return ExitUsage
	case *criteriaPath == "":
		fmt.Fprint(stderr, "give -criteria to declare what the inspection checks\n\n")
		usage(stderr, flags)
		return ExitUsage
	case *notesPath == "" && len(*images) == 0:
		fmt.Fprint(stderr, "give -notes or at least one -image: an inspection needs something to read\n\n")
		usage(stderr, flags)
		return ExitUsage
	}

	criteria, code := loadCriteria(*criteriaPath, stderr)
	if code != ExitOK {
		return code
	}
	notes, code := loadNotes(*notesPath, stderr)
	if code != ExitOK {
		return code
	}
	evidence, code := buildEvidence(ctx, deps, notes, *images, *descriptionsPath, stderr)
	if code != ExitOK {
		return code
	}
	observations, code := loadObservations(ctx, deps, criteria, evidence, *observationsPath, stderr)
	if code != ExitOK {
		return code
	}

	options := evaluate.Options{MinConfidence: *threshold}
	result, err := evaluate.Run(criteria, evidence, observations, options)
	if err != nil {
		failure(stderr, "the inspection was refused", err)
		return ExitRefused
	}

	verify := deps.Verify
	if verify == nil {
		verify = evaluate.Verify
	}
	if err := verify(criteria, result, options); err != nil {
		failure(stderr, "the result is a defect in this repository", err)
		return ExitDefect
	}

	if *asJSON {
		encoded, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			failure(stderr, "the result could not be encoded", err)
			return ExitDefect
		}
		fmt.Fprintf(stdout, "%s\n", encoded)
	} else {
		preview(stdout, result)
	}

	if result.Summary.NeedsHumanReview {
		return ExitReview
	}
	return ExitOK
}

// loadCriteria reads and checks the criteria file, which is the contract of the
// inspection: what is checked, and how sure an observation has to be.
func loadCriteria(path string, stderr io.Writer) (inspection.Criteria, int) {
	data, err := os.ReadFile(path)
	if err != nil {
		failure(stderr, "read the criteria", err)
		return inspection.Criteria{}, ExitRefused
	}

	var criteria inspection.Criteria
	if err := json.Unmarshal(data, &criteria); err != nil {
		failure(stderr, "decode the criteria", err)
		return inspection.Criteria{}, ExitRefused
	}
	if err := criteria.Validate(); err != nil {
		failure(stderr, "the criteria were refused", err)
		return inspection.Criteria{}, ExitRefused
	}
	return criteria, ExitOK
}

// loadNotes reads the inspector's text. No notes at all is fine when an image
// was supplied.
func loadNotes(path string, stderr io.Writer) (string, int) {
	if path == "" {
		return "", ExitOK
	}

	data, err := os.ReadFile(path)
	if err != nil {
		failure(stderr, "read the notes", err)
		return "", ExitRefused
	}
	return string(data), ExitOK
}

// buildEvidence turns the supplied notes and images into the evidence an
// observation has to quote.
//
// Images are described by the fixture when one was named, and by the model
// otherwise; either way what comes back is a description, never a verdict.
func buildEvidence(
	ctx context.Context,
	deps Deps,
	notes string,
	images []string,
	descriptionsPath string,
	stderr io.Writer,
) (inspection.Evidence, int) {
	evidence := inspection.Evidence{Notes: notes}
	if len(images) == 0 {
		return evidence, ExitOK
	}

	describe := func(describer vision.Describer) int {
		evidence.Images = make(map[string]string, len(images))
		for _, ref := range images {
			description, err := describer.Describe(ctx, ref)
			if err != nil {
				return modelOrRefused(stderr, "the image could not be described", err)
			}
			evidence.Images[ref] = description
		}
		return ExitOK
	}

	if descriptionsPath != "" {
		fixture, err := vision.LoadFixture(descriptionsPath)
		if err != nil {
			failure(stderr, "the descriptions were refused", err)
			return inspection.Evidence{}, ExitRefused
		}
		return evidence, describe(fixture)
	}

	if deps.NewDescriber == nil {
		failure(stderr, "the image could not be described",
			fmt.Errorf("%w: no describer is configured, and no -descriptions file was given", llm.ErrMissingAPIKey))
		return inspection.Evidence{}, ExitModel
	}
	describer, err := deps.NewDescriber(ctx)
	if err != nil {
		return inspection.Evidence{}, modelOrRefused(stderr, "the image could not be described", err)
	}
	return evidence, describe(describer)
}

// loadObservations reads the observations from a file when one was named, and
// otherwise asks the model. Either way every observation has to quote the
// evidence it claims before anything is evaluated.
func loadObservations(
	ctx context.Context,
	deps Deps,
	criteria inspection.Criteria,
	evidence inspection.Evidence,
	path string,
	stderr io.Writer,
) ([]inspection.Observation, int) {
	if path != "" {
		observations, err := readObservations(path)
		if err != nil {
			failure(stderr, "decode the observations", err)
			return nil, ExitRefused
		}
		return observations, ExitOK
	}

	if deps.NewReader == nil {
		failure(stderr, "the evidence could not be read",
			fmt.Errorf("%w: no reader is configured, and no -observations file was given", llm.ErrMissingAPIKey))
		return nil, ExitModel
	}
	reader, err := deps.NewReader(ctx)
	if err != nil {
		return nil, modelOrRefused(stderr, "the evidence could not be read", err)
	}

	observations, err := reader.Read(ctx, criteria, evidence)
	if err != nil {
		return nil, modelOrRefused(stderr, "the evidence could not be read", err)
	}
	return observations, ExitOK
}

// readObservations reads a file shaped like the answer a reader returns, so a
// recorded answer can be evaluated again without a model.
func readObservations(path string) ([]inspection.Observation, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var answer struct {
		Observations []inspection.Observation `json:"observations"`
	}
	if err := json.Unmarshal(data, &answer); err != nil {
		return nil, err
	}
	return answer.Observations, nil
}

// modelOrRefused maps a failure to the exit code that says whose problem it is:
// the model being unusable, or an answer that was refused.
func modelOrRefused(stderr io.Writer, subject string, err error) int {
	if errors.Is(err, llm.ErrMissingAPIKey) || errors.Is(err, llm.ErrModelUnavailable) {
		failure(stderr, subject, err)
		return ExitModel
	}
	failure(stderr, subject, err)
	return ExitRefused
}

// failure prints one failure to the error stream.
func failure(w io.Writer, subject string, err error) {
	fmt.Fprintf(w, "%s: %v\n", subject, err)
}

// usage prints what the command does, what it accepts, and what its exit codes
// mean.
func usage(w io.Writer, flags *flag.FlagSet) {
	fmt.Fprint(w, "Evaluate a product inspection against declared criteria, one evidence-backed status per criterion.\n\n")
	fmt.Fprint(w, "Usage:\n"+
		"  product-image-inspection -criteria <file> (-notes <file> | -image <file>...)\n"+
		"      [-descriptions <file>] [-observations <file>] [-threshold <number>] [-json]\n\n")
	flags.PrintDefaults()
	fmt.Fprint(w, "\nExit codes:\n"+
		"  0  every criterion resolved from the evidence\n"+
		"  1  the run completed and at least one criterion needs a person\n"+
		"  2  the command line was wrong\n"+
		"  3  an input, or an answer, was refused\n"+
		"  4  the model could not be used\n"+
		"  5  the result is a defect in this repository\n")
}

// stringList collects a flag given more than once, in the order it was given.
type stringList []string

// String renders the collected values for the flag package.
func (l *stringList) String() string {
	return fmt.Sprint([]string(*l))
}

// Set appends one value and refuses an empty one, which could never be an image.
func (l *stringList) Set(value string) error {
	if value == "" {
		return errors.New("an empty image reference is not an image")
	}
	*l = append(*l, value)
	return nil
}

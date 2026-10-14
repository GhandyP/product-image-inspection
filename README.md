# product-image-inspection

Evaluate a product inspection against declared criteria, one evidence-backed status per criterion. The program
never decides anything about inventory: a criterion whose evidence is missing, weak or contradictory comes out as
`review`, and every result declares a qualified human approval as required.

Go + Google ADK (Go v2). Built with ODD; the specification, the tasks and the log are in `odd/tasks/`.

## The guarantee

Every criterion declared in the criteria file comes out of a run with exactly one of three statuses — `pass`,
`fail` or `review` — plus the evidence it rests on, where that evidence is, and the confidence that was reported
for it. A criterion with no usable evidence does not disappear and is not guessed: it becomes `review`, and the
command exits `1` instead of `0`.

| Status | Meaning | Reasons it appears |
| --- | --- | --- |
| `pass` | every usable observation agreed the criterion holds | `resolved` |
| `fail` | every usable observation agreed the criterion is broken | `resolved` |
| `review` | a person has to look | `no_observation`, `indeterminate_observation`, `low_confidence`, `contradictory_observations` |

The result is produced by `internal/evaluate`, which is ordinary deterministic code, and then re-checked by
`evaluate.Verify`: the summary is re-derived from the checks, every criterion has to be present exactly once and
in the order it was declared, a resolved row has to carry the evidence it rests on and meet the confidence its
criterion requires, and the approval has to be there. A result that fails that check is reported as a defect in
this repository — exit `5` — instead of being printed as if it were plausible.

**No field in the output authorizes an inventory decision**, because the program never makes one. It does not
release, reject, scrap, or relabel anything; it reports what the evidence says and who has to decide.

## The model is not in the decision path

Two calls can reach a model, and neither one decides anything:

1. **Describing an image** (`internal/vision`). The describer returns what is visible and where, in prose, and is
   told never to judge. That description becomes *evidence*.
2. **Reading the evidence into observations** (`internal/agent`). The reader reports, per criterion, what the
   evidence shows. It answers with observations only: the output schema has no field for a status or a verdict.

What the reader answers is treated as untrusted input and refused whole if any of it breaks a rule:

- every observation must **quote** the source it claims, and the quote is re-checked against the notes or against
  the description of the image it names (case and line breaks are folded; paraphrasing is not); a confidence
  outside `0..1` and a criterion that was never declared are refused too, and an image observation has to name its
  image and where in it the evidence is;
- one violation refuses the whole answer. A partly usable answer would let a status look backed by evidence the
  model invented — so instead nothing is evaluated, and the command exits `3`.

The status is then derived in Go: observations that disagree are not settled by majority, a confidence below the
threshold its criterion requires is not enough, and `indeterminate` from a reader that could not tell sends the
criterion to a person. The evaluator re-runs the same quote check on its own input, so a hand-written observations
file cannot smuggle in text the notes do not contain either.

## Quick start

Requirements: Go 1.26.5 or newer. No key and no network are needed for any of the commands below.

```bash
# Notes that satisfy every criterion: three passes, exit 0.
go run . -criteria testdata/criteria/label-inspection.json \
         -notes testdata/notes/pass.txt \
         -observations testdata/observations/notes-pass.json

# The same inspection as JSON, with the evidence, the confidence and the approval.
go run . -criteria testdata/criteria/label-inspection.json \
         -notes testdata/notes/pass.txt \
         -observations testdata/observations/notes-pass.json -json

# An occluded part: the seam cannot be judged, so it goes to a person and the exit code is 1.
go run . -criteria testdata/criteria/label-inspection.json \
         -notes testdata/notes/occluded.txt \
         -observations testdata/observations/notes-occluded.json

# An image of a product that fails. The description stands in for the vision model here.
go run . -criteria testdata/criteria/label-inspection.json \
         -image testdata/images/fail.png \
         -descriptions testdata/descriptions.json \
         -observations testdata/observations/images-fail.json
```

`-observations` is the offline path: it evaluates observations kept in a file, so a run is reproducible and needs
no model. Without it, the notes are read by Gemini. `-descriptions` does the same for images: without it, the
image is described by Gemini.

The live paths need a key from [Google AI Studio](https://aistudio.google.com/apikey):

```bash
export GOOGLE_API_KEY="your-key"
go run . -criteria testdata/criteria/label-inspection.json -notes testdata/notes/pass.txt
go run . -criteria testdata/criteria/label-inspection.json -image testdata/images/fail.png
```

Set `ADK_MODEL` to use another Gemini model; the default is `gemini-flash-latest`.

## Exit codes

They are part of the contract, because a caller has to tell "the evidence was not enough" apart from "your request
was refused".

| Code | Meaning |
| --- | --- |
| `0` | every criterion resolved from the evidence |
| `1` | the run completed and at least one criterion needs a person |
| `2` | the command line was wrong |
| `3` | an input, or an answer, was refused |
| `4` | the model could not be used: no key, or no usable answer |
| `5` | the result is a defect in this repository |

## Fixtures

The fixtures under `testdata/` are the cases the brief asks to cover, and `internal/cli/e2e_test.go` runs the
built command over every one of them. Each case pairs the notes a person would write with the observations a
reader is expected to report, so the case is checkable offline and the expected answer is written down.

| Case | Notes | Observations | Exit |
| --- | --- | --- | --- |
| pass | `notes/pass.txt` | `observations/notes-pass.json` | 0 |
| fail | `notes/fail.txt` | `observations/notes-fail.json` | 0 |
| occluded | `notes/occluded.txt` | `observations/notes-occluded.json` | 1 |
| low light | `notes/low-light.txt` | `observations/notes-low-light.json` | 1 |
| missing evidence | `notes/missing-evidence.txt` | `observations/notes-missing.json` | 1 |
| contradictory | `notes/contradictory.txt` | `observations/notes-contradictory.json` | 1 |
| invented quote | `notes/pass.txt` | `observations/notes-invented-quote.json` | 3 |
| image: pass | `images/pass.png` | `observations/images-pass.json` | 0 |
| image: fail | `images/fail.png` | `observations/images-fail.json` | 0 |
| image: ambiguous | `images/ambiguous.png` | `observations/images-ambiguous.json` | 1 |

The three images are 64×64 PNGs generated for this repository; the descriptions in `testdata/descriptions.json`
are what the offline path returns for them. The ambiguous frame is underexposed on purpose, and it has to
escalate rather than guess.

## Limits, and what is not verified

- **The live paths are not verified by the suite.** The test suite runs with no key and no network, so it exercises
  the reader and the describer through the offline fakes only. `go run . -notes ...` and `go run . -image ...`
  against Gemini are documented, not proven here.
- **A description is a model's reading of an image, not a measurement.** Anything that needs a measurement
  (dimensions, tolerances, colour in a calibrated space) belongs in a device adapter, not in this program.
- **The criteria file is trusted input.** It is validated for shape, not for being the right criteria: a criterion
  that is impossible to observe will simply keep coming back as `review`.
- **`-observations` trusts the file the way it trusts the model.** The quote check and the contract rules are
  applied, but a person who writes an observation by hand is the only one who saw the product.
- **One image, one description, no history.** There is no storage, no queue, no database and no record of previous
  inspections; the quality record belongs to the system that calls this command.

## Layout

```text
product-image-inspection/
├── main.go                    the command: wires the CLI to the real reader, describer and verifier
├── internal/inspection/       criteria, evidence, observations, statuses, and the static rules
├── internal/evaluate/         the decision path: one status per criterion, and Verify
├── internal/agent/            evidence -> quoted observations, through Google ADK
├── internal/vision/           image -> description, behind a port with an offline fake
├── internal/cli/              the command line, the preview, the JSON output, the exit codes
├── internal/llm/              the one place that reads ADK_MODEL and builds the Gemini client
└── odd/tasks/                 the ODD specification, the tasks, the log, and the source brief
```

## Development

```bash
gofmt -l .          # must print nothing
go vet ./...
go build ./...
go test -race ./...  # offline: no key, no network
```

## License

MIT. See `LICENSE`.

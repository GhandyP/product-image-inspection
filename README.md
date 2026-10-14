# product-image-inspection

Evaluate a product inspection against declared criteria, one evidence-backed status per criterion. The program
never decides what to do with inventory: a missing, ambiguous or contradictory observation becomes `review`, and
every result declares a qualified human approval as required.

Go + Google ADK (Go v2). Status: under construction, built in the open with ODD (see `odd/tasks/`).

## The guarantee

Every criterion declared in the criteria file comes out of a run with exactly one of three statuses — `pass`,
`fail` or `review` — plus the evidence it rests on and the confidence that was reported for it. A criterion with
no usable observation does not disappear and is not guessed: it becomes `review`.

The model is not in the decision path. Its only job is to turn free-form inspection notes, or a plain
description of an image, into structured observations that quote the text they came from. `internal/evaluate`
derives the status from those observations. An observation that quotes text the note does not contain, names a
criterion that was never declared, or carries a confidence outside `0..1`, rejects the whole payload: the run
then has no observations to evaluate and every criterion is `review`.

Inventory decisions remain outside this program. It does not release, reject, or relabel anything.

The repository is not finished yet; the README will carry the quick start, the exit codes and the declared
limits once the implementation lands.

## Layout

```text
product-image-inspection/
├── internal/inspection/  criteria, observations, statuses, and the static rules
├── internal/evaluate/    the deterministic evidence gate: one status per criterion
├── internal/agent/       notes -> quoted observations, through Google ADK
├── internal/vision/      image -> description, behind a port with an offline fake
├── internal/cli/         the command line, the JSON output, and the exit codes
└── odd/tasks/            the ODD specification, tasks, and log
```

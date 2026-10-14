# 37 — Product image analysis

## Goal

Evaluate supplied product descriptions or inspection notes against stated criteria without claiming to see a product image in the current prototype.

## Run the current prototype

From this case directory:

```bash
export GOOGLE_API_KEY="your-key"
go run .
```

`internal/adkhelper.Run` uses ADK Go v2.2.0, Gemini, and `gemini-flash-latest`; set `ADK_MODEL` to override the model.

Sample prompt:

```text
Evaluate this product inspection description against the pass criteria.
```

## What exists now

- `main.go (exists)` sends inspection text and criteria to one focused Gemini ADK agent.
- The response is free-form; criterion ID, observation, pass/fail, confidence, and evidence location are not validated.
- This is intentionally text-only: it does not accept image input, call a vision model, or write a quality result.

## Missing elements

- **Contract:** Define `InspectionInput{productID, criteria[], notes?, imageRefs[]}` -> `InspectionResult{checks[{criterion, status, evidence, confidence}], escalation}`.
- **ADK layer:** Add a criteria/check schema and product session; use an image-analysis sub-agent only after an approved vision adapter supplies the image.
- **Domain boundary:** Add image storage, vision-model, measurement/criteria, and quality-record adapters with evidence retention.
- **Fixtures and validation:** Cover pass, fail, occluded, missing, low-light, and contradictory observations; test one result per criterion and confidence thresholds.
- **Safety:** Route uncertain or failed inspections to a qualified human; do not release, reject, or relabel inventory automatically.

## Target structure

```text
37-product-image-analysis/
├── main.go (exists)
├── README.md (exists)
├── schema.go (next)
├── inspection.go (next)
├── adapters/vision.go (next)
├── testdata/inspections.json (next)
└── main_test.go (next)
```

## Implementation order

1. Define criteria, observations, status, confidence, and evidence schemas.
2. Make the text-note path deterministic and criterion-complete.
3. Add image/vision adapters with bounded image metadata and session state.
4. Test occlusion, missing evidence, contradictory notes, and thresholds.
5. Add a human-reviewed quality record workflow before operational decisions.

## Done when

- [ ] Each criterion has one validated status, evidence, and confidence.
- [ ] Missing or ambiguous visual evidence becomes `review`, not a guess.
- [ ] Fixtures cover pass/fail/occluded/low-light cases.
- [ ] Inventory decisions require qualified human approval.

## Next improvement

Stabilize criterion-level output on text notes before connecting a vision model.

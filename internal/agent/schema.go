package agent

import (
	"google.golang.org/genai"

	"github.com/GhandyP/product-image-inspection/internal/inspection"
)

// OutputSchema is the structured schema the reader agent has to answer with.
//
// It mirrors inspection.Observation and nothing more: there is no field for a
// status, a verdict, or an inventory decision, so a model cannot answer with
// one. A model that answers outside this schema cannot reach the evaluator, and
// a model that answers inside it still passes ValidateObservations and
// VerifyQuotes before anything is evaluated.
func OutputSchema() *genai.Schema {
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"observations": {
				Type:        genai.TypeArray,
				Description: "One entry for each criterion you can judge from the supplied evidence.",
				Items: &genai.Schema{
					Type: genai.TypeObject,
					Properties: map[string]*genai.Schema{
						"criterion_id": {
							Type:        genai.TypeString,
							Description: "Identifier of a declared criterion, spelled exactly as it was declared.",
						},
						"source": {
							Type:        genai.TypeString,
							Format:      "enum",
							Enum:        []string{string(inspection.SourceNote), string(inspection.SourceImage)},
							Description: "Where the evidence comes from: the notes or the description of one image.",
						},
						"evidence": {
							Type:        genai.TypeString,
							Description: "Text copied from that source, exactly as it appears there. Never paraphrased.",
						},
						"finding": {
							Type:   genai.TypeString,
							Format: "enum",
							Enum: []string{
								string(inspection.FindingComplies),
								string(inspection.FindingViolates),
								string(inspection.FindingIndeterminate),
							},
							Description: "What the evidence shows, or indeterminate when you cannot tell.",
						},
						"confidence": {
							Type:        genai.TypeNumber,
							Description: "How sure you are, from 0 to 1, reported honestly.",
						},
						"location": {
							Type:        genai.TypeString,
							Description: "Where in the image the evidence is. Required for an image observation.",
						},
						"image_ref": {
							Type:        genai.TypeString,
							Description: "Reference of the image the evidence came from. Required for an image observation.",
						},
					},
					Required: []string{"criterion_id", "source", "evidence", "finding", "confidence"},
				},
			},
		},
		Required: []string{"observations"},
	}
}

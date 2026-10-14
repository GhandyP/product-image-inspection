package agent

import (
	"context"

	"github.com/GhandyP/product-image-inspection/internal/inspection"
)

// Fake is a Reader that answers with fixed observations.
//
// It exists so the rest of the pipeline can be exercised — by the tests and by
// a caller who already has observations — without a model and without an API
// key: the reading step is the only part of this repository that needs either.
type Fake struct {
	// Observations is the answer the fake returns.
	Observations []inspection.Observation
	// Err, when set, is returned instead of Observations.
	Err error
}

var _ Reader = Fake{}

// Read returns the fixed observations and ignores the evidence.
func (f Fake) Read(context.Context, inspection.Criteria, inspection.Evidence) ([]inspection.Observation, error) {
	if f.Err != nil {
		return nil, f.Err
	}
	return f.Observations, nil
}

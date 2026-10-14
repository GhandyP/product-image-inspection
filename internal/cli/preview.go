package cli

import (
	"fmt"
	"io"

	"github.com/GhandyP/product-image-inspection/internal/inspection"
)

// preview prints one inspection: a row per criterion, in the order the criteria
// were declared, with the evidence the row rests on, and then what the run
// counted.
//
// The evidence is printed in full and unparaphrased. A reader deciding whether
// to trust a status needs the text the status came from, and the command line is
// the only place a person will look for it.
func preview(w io.Writer, result inspection.Result) {
	fmt.Fprintf(w, "product: %s\n\n", result.ProductID)

	for _, check := range result.Checks {
		fmt.Fprintf(w, "%-12s %-7s %-26s %s\n", check.CriterionID, check.Status, check.Reason, evidenceLine(check))
		fmt.Fprintf(w, "%-12s %s\n", "", check.Statement)
	}

	fmt.Fprintf(w, "\n%d pass, %d fail, %d review in %d criteria\n",
		result.Summary.Pass, result.Summary.Fail, result.Summary.Review, len(result.Checks))

	if result.Summary.NeedsHumanReview {
		fmt.Fprintf(w, "waiting for a person: %d of %d criteria\n",
			result.Summary.Review, len(result.Checks))
	}

	fmt.Fprintf(w, "approval: required from %s\n", result.Approval.Role)
	if result.Approval.Reason != "" {
		fmt.Fprintf(w, "  %s\n", result.Approval.Reason)
	}
}

// evidenceLine renders the evidence, its confidence and its location in one
// line, and stays empty when the criterion was not resolved from any evidence.
func evidenceLine(check inspection.Check) string {
	if check.Evidence == "" {
		return ""
	}

	line := fmt.Sprintf("%.2f %s", check.Confidence, check.Evidence)
	if check.Location != "" {
		line = fmt.Sprintf("%s (at %s)", line, check.Location)
	}
	return line
}

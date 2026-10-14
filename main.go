// Command product-image-inspection evaluates a product inspection against
// declared criteria and prints one evidence-backed status per criterion.
//
// It never decides anything about inventory: a criterion whose evidence is
// missing, weak or contradictory comes out as review, and every result
// declares a qualified human approval as required.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/GhandyP/product-image-inspection/internal/agent"
	"github.com/GhandyP/product-image-inspection/internal/cli"
	"github.com/GhandyP/product-image-inspection/internal/evaluate"
	"github.com/GhandyP/product-image-inspection/internal/vision"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	code := cli.Run(ctx, os.Args[1:], cli.Deps{
		NewReader: func(ctx context.Context) (agent.Reader, error) {
			return agent.NewGeminiReader(ctx)
		},
		NewDescriber: func(ctx context.Context) (vision.Describer, error) {
			return vision.NewGeminiDescriber(ctx)
		},
		Verify: evaluate.Verify,
	}, os.Stdout, os.Stderr)

	stop()
	os.Exit(code)
}

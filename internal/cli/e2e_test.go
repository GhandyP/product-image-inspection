package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/GhandyP/product-image-inspection/internal/inspection"
)

// The fixtures are part of the contract of this repository: they are what the
// case asks to cover, and this test is what keeps them honest. It runs the built
// command, not the package, because the exit codes are a promise to a caller
// and `go run` would hide them behind its own.
var (
	commandOnce sync.Once
	commandPath string
	commandDir  string
	commandErr  error
)

// TestMain removes the built command once the package is done.
func TestMain(m *testing.M) {
	code := m.Run()
	if commandDir != "" {
		os.RemoveAll(commandDir)
	}
	os.Exit(code)
}

// command builds the command once and returns its path.
func command(t *testing.T) string {
	t.Helper()

	commandOnce.Do(func() {
		root, err := filepath.Abs(filepath.Join("..", ".."))
		if err != nil {
			commandErr = err
			return
		}
		commandDir, commandErr = os.MkdirTemp("", "product-image-inspection-")
		if commandErr != nil {
			return
		}

		commandPath = filepath.Join(commandDir, "product-image-inspection")
		build := exec.Command("go", "build", "-o", commandPath, ".")
		build.Dir = root
		if out, err := build.CombinedOutput(); err != nil {
			commandErr = fmt.Errorf("go build: %w\n%s", err, out)
		}
	})

	if commandErr != nil {
		t.Fatalf("the command could not be built: %v", commandErr)
	}
	return commandPath
}

// moduleRoot is the directory the fixtures' relative paths are written against.
func moduleRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve the module root: %v", err)
	}
	return root
}

// withoutEnv runs the command in an environment that has no model configured,
// which is how the offline promise is checked: no key, no network, no model.
func withoutEnv(t *testing.T, args ...string) outcome {
	t.Helper()

	var stdout, stderr bytes.Buffer
	cmd := exec.Command(command(t), args...)
	cmd.Dir = moduleRoot(t)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Env = filteredEnv("GOOGLE_API_KEY", "ADK_MODEL", "GOOGLE_APPLICATION_CREDENTIALS")

	err := cmd.Run()
	got := outcome{code: 0, stdout: stdout.String(), stderr: stderr.String()}

	var exit *exec.ExitError
	if err != nil {
		if !asExitError(err, &exit) {
			t.Fatalf("run the command: %v", err)
		}
		got.code = exit.ExitCode()
	}
	return got
}

// asExitError reports whether a failed command failure carries an exit code.
func asExitError(err error, target **exec.ExitError) bool {
	exit, ok := err.(*exec.ExitError)
	if ok {
		*target = exit
	}
	return ok
}

// filteredEnv copies the environment without the named variables.
func filteredEnv(without ...string) []string {
	blocked := make(map[string]bool, len(without))
	for _, name := range without {
		blocked[name] = true
	}

	env := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		name, _, found := strings.Cut(entry, "=")
		if found && blocked[name] {
			continue
		}
		env = append(env, entry)
	}
	return env
}

func TestTheBuiltCommandCoversTheCasesTheBriefAsksFor(t *testing.T) {
	const criteria = "testdata/criteria/label-inspection.json"

	cases := []struct {
		name     string
		args     []string
		wantCode int
		want     []string
	}{
		{
			name:     "notes that pass every criterion",
			args:     []string{"-criteria", criteria, "-notes", "testdata/notes/pass.txt", "-observations", "testdata/observations/notes-pass.json"},
			wantCode: ExitOK,
			want:     []string{"3 pass, 0 fail, 0 review in 3 criteria", "qualified human inspector"},
		},
		{
			name:     "notes that fail a criterion",
			args:     []string{"-criteria", criteria, "-notes", "testdata/notes/fail.txt", "-observations", "testdata/observations/notes-fail.json"},
			wantCode: ExitOK,
			want:     []string{"1 pass, 2 fail, 0 review in 3 criteria", "The seam along the lid is open on the left side."},
		},
		{
			name:     "an occluded part",
			args:     []string{"-criteria", criteria, "-notes", "testdata/notes/occluded.txt", "-observations", "testdata/observations/notes-occluded.json"},
			wantCode: ExitReview,
			want:     []string{"indeterminate_observation", "waiting for a person: 1 of 3 criteria"},
		},
		{
			name:     "low light",
			args:     []string{"-criteria", criteria, "-notes", "testdata/notes/low-light.txt", "-observations", "testdata/observations/notes-low-light.json"},
			wantCode: ExitReview,
			want:     []string{"indeterminate_observation", "The visible surface is in shadow"},
		},
		{
			name:     "missing evidence",
			args:     []string{"-criteria", criteria, "-notes", "testdata/notes/missing-evidence.txt", "-observations", "testdata/observations/notes-missing.json"},
			wantCode: ExitReview,
			want:     []string{"no_observation", "waiting for a person: 2 of 3 criteria"},
		},
		{
			name:     "contradictory observations",
			args:     []string{"-criteria", criteria, "-notes", "testdata/notes/contradictory.txt", "-observations", "testdata/observations/notes-contradictory.json"},
			wantCode: ExitReview,
			want:     []string{"contradictory_observations", "The seam along the lid is open on the right side."},
		},
		{
			name:     "an image of a product that passes",
			args:     []string{"-criteria", criteria, "-image", "testdata/images/pass.png", "-descriptions", "testdata/descriptions.json", "-observations", "testdata/observations/images-pass.json"},
			wantCode: ExitOK,
			want:     []string{"3 pass, 0 fail, 0 review in 3 criteria", "at centre of the panel"},
		},
		{
			name:     "an image of a product that fails",
			args:     []string{"-criteria", criteria, "-image", "testdata/images/fail.png", "-descriptions", "testdata/descriptions.json", "-observations", "testdata/observations/images-fail.json"},
			wantCode: ExitOK,
			want:     []string{"1 pass, 2 fail, 0 review in 3 criteria", "a dark diagonal scratch across its lower half"},
		},
		{
			name:     "an ambiguous image has to escalate",
			args:     []string{"-criteria", criteria, "-image", "testdata/images/ambiguous.png", "-descriptions", "testdata/descriptions.json", "-observations", "testdata/observations/images-ambiguous.json"},
			wantCode: ExitReview,
			want:     []string{"indeterminate_observation", "The seam along the lid is not visible"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := withoutEnv(t, tc.args...)

			if got.code != tc.wantCode {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s",
					got.code, tc.wantCode, got.stdout, got.stderr)
			}
			for _, want := range tc.want {
				if !strings.Contains(got.stdout, want) {
					t.Errorf("stdout does not contain %q:\n%s", want, got.stdout)
				}
			}
			if got.stderr != "" {
				t.Errorf("stderr = %q, want nothing", got.stderr)
			}
		})
	}
}

func TestTheBuiltCommandRefusesAnInventedQuote(t *testing.T) {
	got := withoutEnv(t,
		"-criteria", "testdata/criteria/label-inspection.json",
		"-notes", "testdata/notes/pass.txt",
		"-observations", "testdata/observations/notes-invented-quote.json",
	)

	if got.code != ExitRefused {
		t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s", got.code, ExitRefused, got.stdout, got.stderr)
	}
	if !strings.Contains(got.stderr, "quotes text the notes do not contain") {
		t.Errorf("stderr does not say the quote was not in the notes:\n%s", got.stderr)
	}
	if got.stdout != "" {
		t.Errorf("stdout = %q, want nothing evaluated from an invented quote", got.stdout)
	}
}

func TestTheBuiltCommandPrintsTheResultAsJSON(t *testing.T) {
	got := withoutEnv(t,
		"-criteria", "testdata/criteria/label-inspection.json",
		"-notes", "testdata/notes/pass.txt",
		"-observations", "testdata/observations/notes-pass.json",
		"-json",
	)
	if got.code != ExitOK {
		t.Fatalf("exit code = %d, want %d\nstderr:\n%s", got.code, ExitOK, got.stderr)
	}

	var result inspection.Result
	if err := json.Unmarshal([]byte(got.stdout), &result); err != nil {
		t.Fatalf("stdout is not the result as JSON: %v\n%s", err, got.stdout)
	}
	if result.ProductID != "SKU-4471" || len(result.Checks) != 3 {
		t.Fatalf("result = %+v, want three checks for SKU-4471", result)
	}
	if result.Summary.Pass != 3 || result.Summary.NeedsHumanReview {
		t.Errorf("summary = %+v, want three passes and nothing waiting for a person", result.Summary)
	}

	// Whatever the JSON says, it must not say that this program decided
	// anything about the inventory.
	for _, forbidden := range []string{"release", "reject", "scrap", "relabel"} {
		if strings.Contains(strings.ToLower(got.stdout), forbidden) {
			t.Errorf("the result mentions %q, which this program never decides:\n%s", forbidden, got.stdout)
		}
	}
}

func TestTheBuiltCommandNeedsAKeyOnlyForTheLivePaths(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{
			name: "reading notes needs a model when no observations were given",
			args: []string{"-criteria", "testdata/criteria/label-inspection.json", "-notes", "testdata/notes/pass.txt"},
		},
		{
			name: "describing an image needs a model when no descriptions were given",
			args: []string{"-criteria", "testdata/criteria/label-inspection.json", "-image", "testdata/images/pass.png"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := withoutEnv(t, tc.args...)

			if got.code != ExitModel {
				t.Fatalf("exit code = %d, want %d\nstdout:\n%s\nstderr:\n%s",
					got.code, ExitModel, got.stdout, got.stderr)
			}
			if !strings.Contains(got.stderr, "GOOGLE_API_KEY") {
				t.Errorf("stderr does not name the missing configuration:\n%s", got.stderr)
			}
		})
	}
}

func TestTheBuiltCommandExplainsItself(t *testing.T) {
	got := withoutEnv(t, "-h")

	if got.code != ExitOK {
		t.Fatalf("exit code = %d, want %d for -h", got.code, ExitOK)
	}
	for _, want := range []string{"-criteria", "-observations", "Exit codes:", "needs a person", "defect in this repository"} {
		if !strings.Contains(got.stderr, want) {
			t.Errorf("the help does not mention %q:\n%s", want, got.stderr)
		}
	}
}

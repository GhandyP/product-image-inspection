package vision

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

const described = "A pale panel with a dark diagonal scratch across its lower half."

// fakeModel stands in for Gemini, so a multimodal request can be inspected
// without a key and without the network.
type fakeModel struct {
	response string
	err      error
	requests []*model.LLMRequest
}

func (m *fakeModel) Name() string { return "fake-model" }

func (m *fakeModel) GenerateContent(_ context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	m.requests = append(m.requests, req)

	return func(yield func(*model.LLMResponse, error) bool) {
		if m.err != nil {
			yield(nil, m.err)
			return
		}
		yield(&model.LLMResponse{
			Content:      genai.NewContentFromText(m.response, genai.RoleModel),
			TurnComplete: true,
		}, nil)
	}
}

func newTestDescriber(t *testing.T, m model.LLM) *ADKDescriber {
	t.Helper()

	describer, err := NewADKDescriber(m)
	if err != nil {
		t.Fatalf("NewADKDescriber() error = %v, want nil", err)
	}
	return describer
}

// writePNG writes a real image, so the request carries bytes a decoder could
// read rather than a stand-in string.
func writePNG(t *testing.T, name string) string {
	t.Helper()

	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatalf("encode the fixture image: %v", err)
	}

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write the fixture image: %v", err)
	}
	return path
}

func TestDescribeSendsTheImageAsAMultimodalPart(t *testing.T) {
	path := writePNG(t, "panel.png")
	imageBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the fixture image: %v", err)
	}
	m := &fakeModel{response: described}

	description, err := newTestDescriber(t, m).Describe(context.Background(), path)
	if err != nil {
		t.Fatalf("Describe() error = %v, want nil", err)
	}
	if description != described {
		t.Errorf("Describe() = %q, want %q", description, described)
	}

	if len(m.requests) != 1 {
		t.Fatalf("model received %d requests, want exactly 1", len(m.requests))
	}
	contents := m.requests[0].Contents
	if len(contents) == 0 {
		t.Fatal("the model received no content")
	}

	var (
		sawText, sawImage bool
	)
	for _, part := range contents[0].Parts {
		if part.Text != "" {
			sawText = true
		}
		if part.InlineData == nil {
			continue
		}
		sawImage = true
		if part.InlineData.MIMEType != "image/png" {
			t.Errorf("the image was sent as %q, want image/png", part.InlineData.MIMEType)
		}
		if !bytes.Equal(part.InlineData.Data, imageBytes) {
			t.Error("the bytes the model received are not the bytes of the image")
		}
	}
	if !sawText || !sawImage {
		t.Errorf("the request has text=%v image=%v, want both: the image alone carries no task", sawText, sawImage)
	}
}

func TestDescribeRefusesAnAnswerThatDescribesNothing(t *testing.T) {
	path := writePNG(t, "panel.png")

	for _, response := range []string{"   ", "\n"} {
		description, err := newTestDescriber(t, &fakeModel{response: response}).Describe(context.Background(), path)
		if err == nil {
			t.Fatalf("Describe() = %q, want an error: an empty description is not evidence", description)
		}
		if !strings.Contains(err.Error(), "described nothing") {
			t.Fatalf("Describe() = %q, want it to say the model described nothing", err)
		}
		if description != "" {
			t.Errorf("Describe() = %q alongside an error, want nothing", description)
		}
	}
}

func TestDescribeReportsAModelFailure(t *testing.T) {
	path := writePNG(t, "panel.png")
	failure := errors.New("the model is unreachable")

	if _, err := newTestDescriber(t, &fakeModel{err: failure}).Describe(context.Background(), path); !errors.Is(err, failure) {
		t.Fatalf("Describe() error = %v, want %v", err, failure)
	}
}

func TestTheVisionInstructionForbidsVerdicts(t *testing.T) {
	for _, want := range []string{"never decide", "visible", "person makes the decision"} {
		if !strings.Contains(Instruction, want) {
			t.Errorf("the instruction does not say %q, which is what keeps a description factual", want)
		}
	}
	if strings.Contains(Instruction, "{") {
		t.Error("the instruction carries a brace: ADK would read it as a session-state placeholder")
	}

	m := &fakeModel{response: described}
	if _, err := newTestDescriber(t, m).Describe(context.Background(), writePNG(t, "panel.png")); err != nil {
		t.Fatalf("Describe() error = %v, want nil", err)
	}
	if got := contentText(m.requests[0].Config.SystemInstruction); !strings.HasPrefix(got, Instruction) {
		t.Errorf("the instruction the model received does not start with the one this package publishes:\n%q", got)
	}
}

func TestLoadRefusesWhatItCannotSend(t *testing.T) {
	empty := filepath.Join(t.TempDir(), "empty.png")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatalf("write the empty fixture: %v", err)
	}
	unsupported := filepath.Join(t.TempDir(), "panel.gif")
	if err := os.WriteFile(unsupported, []byte("GIF89a"), 0o600); err != nil {
		t.Fatalf("write the unsupported fixture: %v", err)
	}

	cases := []struct {
		name string
		ref  string
		want string
	}{
		{name: "an unsupported type", ref: unsupported, want: `unsupported image type ".gif"`},
		{name: "a file that is not there", ref: filepath.Join(t.TempDir(), "missing.png"), want: "read image"},
		{name: "an empty file", ref: empty, want: `image "` + empty + `" is empty`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, mimeType, err := Load(tc.ref)
			if err == nil {
				t.Fatalf("Load() = (%d bytes, %q), want an error naming %q", len(data), mimeType, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Load() = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestLoadAcceptsTheFormatsGeminiTakes(t *testing.T) {
	for _, name := range []string{"panel.png", "panel.jpg", "panel.jpeg", "panel.webp"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			if err := os.WriteFile(path, []byte("bytes"), 0o600); err != nil {
				t.Fatalf("write the fixture: %v", err)
			}

			data, mimeType, err := Load(path)
			if err != nil {
				t.Fatalf("Load() error = %v, want nil", err)
			}
			if string(data) != "bytes" {
				t.Errorf("Load() returned %q, want the bytes of the file", data)
			}
			if !strings.HasPrefix(mimeType, "image/") {
				t.Errorf("Load() reported %q, want an image media type", mimeType)
			}
		})
	}
}

func TestFakeDescribesOnlyTheFixturesItHas(t *testing.T) {
	known := filepath.Join("testdata", "images", "fail.png")
	fake := Fake{Descriptions: map[string]string{known: described}}

	got, err := fake.Describe(context.Background(), known)
	if err != nil {
		t.Fatalf("Describe() error = %v, want nil", err)
	}
	if got != described {
		t.Errorf("Describe() = %q, want %q", got, described)
	}

	unknown := filepath.Join("testdata", "images", "pass.png")
	if _, err := fake.Describe(context.Background(), unknown); err == nil ||
		!strings.Contains(err.Error(), `no fixture description for "testdata/images/pass.png"`) {
		t.Fatalf("Describe() error = %v, want it to name the missing fixture", err)
	}

	failure := errors.New("no describer configured")
	if _, err := (Fake{Err: failure}).Describe(context.Background(), known); !errors.Is(err, failure) {
		t.Fatalf("Describe() error = %v, want %v", err, failure)
	}
}

func TestLoadFixtureReadsADescriptionFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "descriptions.json")
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatalf("write the fixture file: %v", err)
		}
	}

	write(`{"testdata/images/fail.png": "` + described + `"}`)
	fake, err := LoadFixture(path)
	if err != nil {
		t.Fatalf("LoadFixture() error = %v, want nil", err)
	}
	if got, err := fake.Describe(context.Background(), "testdata/images/fail.png"); err != nil || got != described {
		t.Fatalf("Describe() = (%q, %v), want the fixture description", got, err)
	}

	cases := []struct {
		name string
		body string
		want string
	}{
		{name: "not JSON", body: "these are descriptions, trust me", want: "decode descriptions"},
		{name: "no entries", body: `{}`, want: "map no image to a description"},
		{name: "an empty description", body: `{"a.png": "   "}`, want: `the description of "a.png" is empty`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			write(tc.body)

			_, err := LoadFixture(path)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("LoadFixture() = %v, want it to contain %q", err, tc.want)
			}
		})
	}

	if _, err := LoadFixture(filepath.Join(t.TempDir(), "absent.json")); err == nil ||
		!strings.Contains(err.Error(), "read descriptions") {
		t.Fatalf("LoadFixture() error = %v, want it to name the file it could not read", err)
	}
}

func TestNewGeminiDescriberNeedsAKey(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "")

	if _, err := NewGeminiDescriber(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "missing GOOGLE_API_KEY") {
		t.Fatalf("NewGeminiDescriber() error = %v, want it to name the missing key", err)
	}
}

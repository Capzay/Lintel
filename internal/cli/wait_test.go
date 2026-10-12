package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lintel/internal/initcmd"
	"lintel/internal/model"
	"lintel/internal/review"
	"lintel/internal/store"
)

func acceptAll(store.Decision, map[string]string, []store.Decision) model.ReviewResult {
	return model.ReviewResult{Verdict: "accept", Summary: "Fit.", Concerns: []string{}, Valid: true, Evidence: "none"}
}

func project(t *testing.T) store.Project {
	t.Helper()
	root := t.TempDir()
	t.Setenv("LINTEL_PROJECT", root)
	if _, err := initcmd.Project(root); err != nil {
		t.Fatal(err)
	}
	return store.Open(root)
}

func runWith(t *testing.T, args []string, opt Option) (int, string) {
	t.Helper()
	var out bytes.Buffer
	opt.Out, opt.Err, opt.In = &out, &bytes.Buffer{}, strings.NewReader("")
	if opt.Review == nil {
		opt.Review = acceptAll
	}
	if opt.Notify == nil {
		opt.Notify = func(string, string) {}
	}
	return Execute(args, opt), out.String()
}

func kickoff(t *testing.T, item store.Project) string {
	t.Helper()
	if code, out := runWith(t, []string{"propose", "--kind", "kickoff", "--title", "Stack", "--question", "What stack?", "--choice", "Go", "--path", "**"}, Option{}); code != 0 {
		t.Fatal(out)
	}
	open, _ := item.OpenDecisions()
	return open[0].ID
}

func TestWaitOpensTheWindowAndReturnsTheHumanChoice(t *testing.T) {
	item := project(t)
	id := kickoff(t, item)
	opened := 0
	window := func(p store.Project, got string) error {
		opened++
		if got != id {
			t.Fatal(got)
		}
		_, err := p.Approve(got, "fine")
		return err
	}
	code, out := runWith(t, []string{"wait"}, Option{Window: window, Sleep: func(time.Duration) {}})
	if code != 0 || opened != 1 || !strings.Contains(out, "Human authorized this.") {
		t.Fatal(code, opened, out)
	}
}

func TestWaitWithoutAWindowSaysSoAndKeepsWaiting(t *testing.T) {
	item := project(t)
	id := kickoff(t, item)
	var notified []string
	opt := Option{
		Window: func(store.Project, string) error { return errors.New("no DISPLAY") },
		Notify: func(title, body string) { notified = append(notified, body) },
		Sleep:  func(time.Duration) { time.Sleep(time.Millisecond) },
	}
	code, out := runWith(t, []string{"wait", "--timeout", "0.05"}, opt)
	if code != 0 || !strings.Contains(out, "No Jev window could be opened (no DISPLAY)") || !strings.Contains(out, "lintel approve "+id) {
		t.Fatal(out)
	}
	if !strings.Contains(out, "Still waiting for the human on "+id+". Run `lintel wait "+id+"` again") {
		t.Fatal(out)
	}
	if len(notified) == 0 {
		t.Fatal("no notification")
	}
	loaded, _ := item.Load(id)
	if loaded.Status != model.StatusReviewed {
		t.Fatal(loaded.Status)
	}
}

func TestWaitOnAnUnreviewedDecisionDoesNotClaimAuthorization(t *testing.T) {
	item := project(t)
	failing := func(store.Decision, map[string]string, []store.Decision) model.ReviewResult {
		return model.ReviewResult{Verdict: "revise", Summary: "TypeSafe API key is not configured.", Valid: false}
	}
	runWith(t, []string{"propose", "--kind", "kickoff", "--title", "Stack", "--question", "What stack?", "--choice", "Go", "--path", "**"}, Option{Review: failing})
	open, _ := item.OpenDecisions()
	code, out := runWith(t, []string{"wait", open[0].ID}, Option{Window: func(store.Project, string) error { t.Fatal("window"); return nil }})
	if code != 1 || strings.Contains(out, "authorized") || !strings.Contains(out, "API key is not configured") {
		t.Fatal(code, out)
	}
}

func TestWindowRecordsOnlyAClick(t *testing.T) {
	item := project(t)
	id := kickoff(t, item)
	code, _ := runWith(t, []string{"window", id}, Option{Ask: func(string) (string, string, bool) { return "", "", false }})
	if code != 0 {
		t.Fatal(code)
	}
	if loaded, _ := item.Load(id); loaded.Status != model.StatusReviewed {
		t.Fatal(loaded.Status)
	}
	if _, err := os.Stat(filepath.Join(item.Dir, "window-"+id+".pid")); !os.IsNotExist(err) {
		t.Fatal("pid file left behind")
	}
	runWith(t, []string{"window", id}, Option{Ask: func(string) (string, string, bool) { return "reject", "too big", true }})
	loaded, _ := item.Load(id)
	if loaded.Status != model.StatusRejected || loaded.Human.Note != "too big" {
		t.Fatal(loaded.Status)
	}
}

func TestAskRecommendsAndProposeUsesTheAdvice(t *testing.T) {
	item := project(t)
	id := kickoff(t, item)
	if _, err := item.Approve(id, ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(item.Root, "go.mod"), []byte("module app\n\nrequire github.com/yuin/goldmark v1.7.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	previous := review.APIKey
	review.APIKey = func() (string, string) { return "ts_test", "" }
	defer func() { review.APIKey = previous }()
	var sent map[string]any
	transport := func(url string, headers map[string]string, payload map[string]any) (map[string]any, error) {
		sent = payload
		return map[string]any{"answers": map[string]any{
			"recommendation": map[string]any{"choice": "goldmark", "probabilities": map[string]any{"goldmark": 0.8, "blackfriday": 0.2}},
			"affected":       map[string]any{"choice": "none"},
		}}, nil
	}
	code, out := runWith(t, []string{"ask", "--question", "Which Markdown renderer?", "--option", "goldmark: CommonMark", "--option", "blackfriday: older"}, Option{Advise: transport})
	if code != 0 || !strings.Contains(out, "Jev recommends  goldmark: CommonMark") || !strings.Contains(out, "goldmark 0.80, blackfriday 0.20") {
		t.Fatal(code, out)
	}
	codebase := sent["state"].(map[string]any)["codebase"].(map[string]any)
	if deps := codebase["dependencies"].(map[string]any)["go.mod"].([]string); len(deps) != 1 || deps[0] != "github.com/yuin/goldmark" {
		t.Fatal(codebase["dependencies"])
	}
	adviceID := strings.TrimSpace(strings.TrimPrefix(strings.Split(out, "\n")[0], "Advice"))
	code, out = runWith(t, []string{"propose", "--from-advice", adviceID, "--kind", "change", "--path", "docs/**", "--package", "github.com/yuin/goldmark"}, Option{})
	if code != 0 || !strings.Contains(out, "Choice    goldmark: CommonMark") || !strings.Contains(out, "blackfriday: older") {
		t.Fatal(code, out)
	}
	if code, out := runWith(t, []string{"ask", "--question", "Which?", "--option", "only"}, Option{Advise: transport}); code != 1 {
		t.Fatal(out)
	}
}

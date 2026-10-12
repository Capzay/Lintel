package prompt

import (
	"strings"
	"testing"
)

func TestApproveAndRejectKeepTheNote(t *testing.T) {
	choice, note, ok := ParseAnswer(1, "Approve")
	if !ok || choice != "approve" || note != "" {
		t.Fatalf("%s %s %v", choice, note, ok)
	}
	choice, note, ok = ParseAnswer(1, "Reject: too broad")
	if !ok || choice != "reject" || note != "too broad" {
		t.Fatalf("%s %s %v", choice, note, ok)
	}
}

func TestClosingTheDialogIsNotADecision(t *testing.T) {
	for _, item := range []struct {
		code   int
		output string
	}{{1, ""}, {0, "approve"}, {0, "Approve|"}, {0, ""}, {1, "Not yet"}} {
		if _, _, ok := ParseAnswer(item.code, item.output); ok {
			t.Fatalf("decided %d %q", item.code, item.output)
		}
	}
}

func TestRunnerResultIsTheDecision(t *testing.T) {
	choice, note, ok := Ask("card", func(string) (int, string, error) {
		return 1, "Reject: caught", nil
	})
	if !ok || choice != "reject" || note != "caught" {
		t.Fatalf("%s %s %v", choice, note, ok)
	}
}

func TestMissingDialogDoesNotDecide(t *testing.T) {
	_, _, ok := Ask("card", func(string) (int, string, error) {
		return 0, "", errUnbalanced{}
	})
	if ok {
		t.Fatal("decided")
	}
}

type errUnbalanced struct{}

func (errUnbalanced) Error() string { return "no display" }

// A $script: variable set inside a GetNewClosure handler never reaches the
// script's own scope, so the Windows window must report through DialogResult.
func TestWindowsScriptReportsThroughDialogResult(t *testing.T) {
	script := windowsScript(`C:\Temp\it's.txt`)
	for _, want := range []string{"$approve.DialogResult = 'Yes'", "$reject.DialogResult = 'No'", "Write-Output 'Approve'", "Write-Output 'Reject'", `C:\Temp\it''s.txt`} {
		if !strings.Contains(script, want) {
			t.Fatalf("script lacks %q", want)
		}
	}
	if strings.Contains(script, "GetNewClosure") || strings.Contains(script, "$script:") {
		t.Fatal("script reports through a closure-scoped variable")
	}
}

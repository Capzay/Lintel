package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestLoginSavesTheKeyWithoutPrintingIt(t *testing.T) {
	var saved string
	previous := saveSecret
	saveSecret = func(key string) error {
		saved = key
		return nil
	}
	t.Cleanup(func() { saveSecret = previous })

	var out, errOut bytes.Buffer
	code := Execute([]string{"login"}, Option{
		In:  strings.NewReader("ts_from_prompt\n"),
		Out: &out,
		Err: &errOut,
	})
	if code != 0 {
		t.Fatal(code, errOut.String())
	}
	if saved != "ts_from_prompt" {
		t.Fatal(saved)
	}
	if strings.Contains(out.String(), "ts_from_prompt") || strings.Contains(errOut.String(), "ts_from_prompt") {
		t.Fatalf("key leaked\nout=%s\nerr=%s", out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "system keyring") {
		t.Fatal(out.String())
	}
	if !strings.Contains(errOut.String(), "TypeSafe API key:") {
		t.Fatal(errOut.String())
	}
}

func TestLogoutRemovesTheKey(t *testing.T) {
	removed := false
	previous := removeSecret
	removeSecret = func() error {
		removed = true
		return nil
	}
	t.Cleanup(func() { removeSecret = previous })

	var out bytes.Buffer
	code := Execute([]string{"logout"}, Option{Out: &out, Err: &bytes.Buffer{}})
	if code != 0 || !removed || !strings.Contains(out.String(), "Removed") {
		t.Fatal(code, removed, out.String())
	}
}

package initcmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHookCommandOnPathIsBare(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "lintel")
	if err := os.WriteFile(exe, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "lintel")
	if err := os.Symlink(exe, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	for _, found := range []string{exe, link} {
		if got := hookCommand(exe, found, false); got != "lintel hook" {
			t.Fatalf("found %s: got %q", found, got)
		}
	}
}

func TestHookCommandOffPathIsAbsolute(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "lintel")
	other := filepath.Join(dir, "other")
	for _, path := range []string{exe, other} {
		if err := os.WriteFile(path, nil, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, found := range []string{"", other} {
		if got := hookCommand(exe, found, false); got != exe+" hook" {
			t.Fatalf("found %q: got %q", found, got)
		}
	}
	if got := hookCommand("/opt/my tools/lintel", "", false); got != "'/opt/my tools/lintel' hook" {
		t.Fatalf("got %q", got)
	}
}

func TestHookCommandWindowsQuoting(t *testing.T) {
	cases := map[string]string{
		`C:\Users\dev\AppData\Local\Programs\lintel\lintel.exe`: `C:/Users/dev/AppData/Local/Programs/lintel/lintel.exe hook`,
		`C:\Users\Jo Dev\lintel.exe`:                            `"C:/Users/Jo Dev/lintel.exe" hook`,
	}
	for exe, want := range cases {
		if got := hookCommand(exe, "", true); got != want {
			t.Fatalf("%s: got %q, want %q", exe, got, want)
		}
	}
}

func TestOwnHookMatchesEveryForm(t *testing.T) {
	for _, command := range []string{
		"lintel hook claude",
		"/home/dev/.local/bin/lintel hook claude",
		`"C:/Users/Jo Dev/lintel.exe" hook codex`,
		"jev hook cursor",
	} {
		if !ownHook(command) {
			t.Fatalf("%q not recognised", command)
		}
	}
}

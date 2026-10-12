package secret

import (
	"strings"
	"testing"
)

func TestLinuxStoreKeepsTheKeyOffTheCommandLine(t *testing.T) {
	var name string
	var args []string
	var stdin string
	runCommand = func(gotName string, gotArgs []string, gotStdin string) (string, error) {
		name = gotName
		args = append([]string{}, gotArgs...)
		stdin = gotStdin
		return "", nil
	}
	t.Cleanup(func() { runCommand = defaultRun })

	if err := saveLinux("ts_secret"); err != nil {
		t.Fatal(err)
	}
	if name != "secret-tool" || stdin != "ts_secret" {
		t.Fatalf("%s %q", name, stdin)
	}
	for _, arg := range args {
		if strings.Contains(arg, "ts_secret") {
			t.Fatal(args)
		}
	}
	if !contains(args, "store") || !contains(args, "lintel") || !contains(args, "typesafe") {
		t.Fatal(args)
	}
}

func TestDarwinStoreKeepsTheKeyOffTheCommandLine(t *testing.T) {
	var args []string
	var stdin string
	runCommand = func(name string, gotArgs []string, gotStdin string) (string, error) {
		if name != "security" {
			t.Fatal(name)
		}
		args = append([]string{}, gotArgs...)
		stdin = gotStdin
		return "", nil
	}
	t.Cleanup(func() { runCommand = defaultRun })

	if err := saveDarwin(`ts_se"cret`); err != nil {
		t.Fatal(err)
	}
	if len(args) != 1 || args[0] != "-i" {
		t.Fatal(args)
	}
	if strings.Contains(strings.Join(args, " "), "ts_se") {
		t.Fatal(args)
	}
	if !strings.Contains(stdin, `ts_se\"cret`) {
		t.Fatal(stdin)
	}
}

func TestWindowsStoreKeepsTheKeyOffTheCommandLine(t *testing.T) {
	var args []string
	var stdin string
	runCommand = func(name string, gotArgs []string, gotStdin string) (string, error) {
		if name != "powershell" {
			t.Fatal(name)
		}
		args = append([]string{}, gotArgs...)
		stdin = gotStdin
		return "", nil
	}
	t.Cleanup(func() { runCommand = defaultRun })

	if err := saveWindows("ts_secret"); err != nil {
		t.Fatal(err)
	}
	if stdin != "ts_secret" {
		t.Fatal(stdin)
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "ts_secret") {
		t.Fatal(args)
	}
	if !contains(args, "store") {
		t.Fatal(args)
	}
}

func TestLookupTreatsAMissingItemAsNotFound(t *testing.T) {
	runCommand = func(string, []string, string) (string, error) {
		return "", &commandError{Code: 1, Text: "not in the keyring"}
	}
	t.Cleanup(func() { runCommand = defaultRun })

	_, err := Load()
	if err != ErrNotFound {
		t.Fatal(err)
	}
}

func TestEmptyKeyIsRejected(t *testing.T) {
	if err := Save("  \n"); err == nil {
		t.Fatal("accepted a blank key")
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want || strings.Contains(value, want) {
			return true
		}
	}
	return false
}

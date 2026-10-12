package scan_test

import (
	"reflect"
	"testing"

	"lintel/internal/scan"
)

func TestRedirectsInsideQuotesAndToDevicesAreNotWrites(t *testing.T) {
	for _, command := range []string{
		"ls -la 2>/dev/null",
		"cd /repo && ls -la && cat a.json b.json 2> /dev/null",
		"rg '=>' src",
		`echo "a > b"`,
		"go test ./... 2>&1 | tail -15",
		"grep -n foo file >&2",
		"cmd &>/dev/null",
		"cat <<'EOF'\nx > y\nEOF",
		"sort < input.txt",
	} {
		if targets := scan.RedirectTargets(command); len(targets) != 0 {
			t.Fatalf("%q wrote %v", command, targets)
		}
	}
}

func TestRealRedirectsAreWrites(t *testing.T) {
	for command, want := range map[string][]string{
		"echo x > Dockerfile":                  {"Dockerfile"},
		"echo x>>notes.md":                     {"notes.md"},
		"cmd 2>err.log":                        {"err.log"},
		"cmd &> all.log":                       {"all.log"},
		"cat > out.txt <<'EOF'\nbody\nEOF\nls": {"out.txt"},
		"ls; echo 'a;b' > \"my file.txt\"":     {"my file.txt"},
		"printf x | tee -a log.txt > copy.txt": {"copy.txt"},
	} {
		if got := scan.RedirectTargets(command); !reflect.DeepEqual(got, want) {
			t.Fatalf("%q: got %v want %v", command, got, want)
		}
	}
}

func TestSegmentsHonourQuotesAndHeredocs(t *testing.T) {
	got := scan.SplitSegments("rg 'a|b' src && git status; cat <<EOF\nrm -rf /\nEOF\nls")
	want := []string{"rg 'a|b' src", "git status", "cat <<EOF", "ls"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%q", got)
	}
}

func TestReadOnlyCommands(t *testing.T) {
	for _, command := range []string{"cd /repo", "ls -la 2>/dev/null", "git grep foo", "go version", "find . -name '*.go'"} {
		if !scan.IsReadOnlySegment(command) {
			t.Fatal(command)
		}
	}
	for _, command := range []string{"echo $(touch x)", "ls `rm x`", "find . -delete", "go build ./...", "sudo ls", "echo x > y"} {
		if scan.IsReadOnlySegment(command) {
			t.Fatal(command)
		}
	}
}

func TestLintelVerbs(t *testing.T) {
	if scan.LintelVerb("lintel wait 2>&1") != "wait" || scan.LintelVerb("/usr/local/bin/lintel approve x") != "approve" {
		t.Fatal("verb")
	}
	if scan.LintelVerb("echo lintel wait") != "" {
		t.Fatal("echo")
	}
	for _, command := range []string{"lintel approve", "bash -c 'lintel approve d1'", "cd x && lintel approve d1"} {
		if !scan.MentionsApprove(command) {
			t.Fatal(command)
		}
	}
	for _, command := range []string{"lintel status && echo approve", `echo '{"command":"lintel approve x"}' | lintel hook claude`, "rg 'lintel approve' README.md"} {
		if scan.MentionsApprove(command) {
			t.Fatal(command)
		}
	}
	if !scan.MentionsApprove("xargs -I{} lintel approve {}") || !scan.MentionsApprove("env FOO=1 lintel approve") {
		t.Fatal("wrapped")
	}
}

func TestInstallPackagesIgnoreRedirects(t *testing.T) {
	packages, ok := scan.PackagesFromShell("npm install zod 2>/dev/null && ls")
	if !ok || !reflect.DeepEqual(packages, map[string]bool{"zod": true}) {
		t.Fatal(packages, ok)
	}
}

func TestRunsLintelSeesEverySegment(t *testing.T) {
	if !scan.RunsLintel("lintel propose --title x && lintel wait", "wait") || scan.RunsLintel("echo lintel wait", "wait") {
		t.Fatal("RunsLintel")
	}
}

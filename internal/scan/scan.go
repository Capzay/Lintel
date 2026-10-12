package scan

import (
	"encoding/json"
	"regexp"
	"strings"
)

// ManifestError means a dependency manifest could not be parsed.
type ManifestError struct{ Msg string }

func (e *ManifestError) Error() string { return e.Msg }

var (
	jsSpec     = regexp.MustCompile(`(?:import\s+(?:type\s+)?(?:[\w*\s{},]+\s+from\s+)?|export\s+(?:[\w*\s{},]+\s+from\s+)?|require\(\s*|import\(\s*)['"]([^'"]+)['"]`)
	pyImport   = regexp.MustCompile(`(?m)^\s*(?:from|import)\s+([A-Za-z_][\w]*)`)
	goSingle   = regexp.MustCompile(`(?m)^\s*import\s+(?:\w+\s+)?"([^"]+)"\s*$`)
	goBlock    = regexp.MustCompile(`(?s)import\s*\((.*?)\)`)
	goQuoted   = regexp.MustCompile(`"([^"]+)"`)
	rustUse    = regexp.MustCompile(`(?m)^\s*(?:pub(?:\([^)]*\))?\s+)?use\s+([A-Za-z_][\w]*)`)
	rustExtern = regexp.MustCompile(`(?m)^\s*extern\s+crate\s+([A-Za-z_][\w]*)`)
	gemLine    = regexp.MustCompile(`gem\s+['"]([^'"]+)['"]`)
	mixDep     = regexp.MustCompile(`\{:([A-Za-z0-9_]+)`)
	envAssign  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	cargoKey   = regexp.MustCompile(`^([A-Za-z0-9_-]+)\s*=`)
	pep508     = regexp.MustCompile(`[<>=!~;\[]`)
)

var nodeBuiltins = setOf(
	"assert", "async_hooks", "buffer", "child_process", "cluster", "console", "constants",
	"crypto", "dgram", "diagnostics_channel", "dns", "domain", "events", "fs", "http", "http2",
	"https", "inspector", "module", "net", "os", "path", "perf_hooks", "process", "punycode",
	"querystring", "readline", "repl", "stream", "string_decoder", "sys", "timers", "tls",
	"trace_events", "tty", "url", "util", "v8", "vm", "wasi", "worker_threads", "zlib",
)

var rustPrelude = setOf("std", "core", "alloc", "crate", "self", "super")

var valueFlags = setOf(
	"-r", "--requirement", "-c", "--constraint", "-t", "--target", "--features", "-F",
	"-p", "--package", "--manifest-path", "--prefix", "-o", "--output", "--python", "--index-url",
)

var installVerbs = map[string]map[string]bool{
	"npm":    setOf("install", "i", "add", "ci", "create", "init"),
	"pnpm":   setOf("install", "i", "add", "ci", "create", "init"),
	"yarn":   setOf("add", "install", "create", "init"),
	"bun":    setOf("add", "install", "create", "init"),
	"cargo":  setOf("add", "install", "new", "init"),
	"pip":    setOf("install"),
	"pip3":   setOf("install"),
	"poetry": setOf("add", "init", "new"),
	"go":     setOf("get", "install"),
}

var readOnly = setOf(
	"ls", "cat", "head", "tail", "pwd", "find", "rg", "grep", "egrep", "fgrep", "echo", "printf",
	"which", "whereis", "whoami", "id", "wc", "file", "stat", "tree", "fd", "true", "false",
	"basename", "dirname", "realpath", "readlink", "date", "uname", "hostname", "env", "printenv",
	"jq", "cut", "sort", "uniq", "tr", "nl", "od", "hexdump", "diff", "cmp", "less", "more",
	"column", "sleep", "test", "[", "cd", "pushd", "popd", "type",
)

var goRead = setOf("version", "env", "list", "doc", "vet")

var gitRead = setOf(
	"status", "diff", "log", "show", "branch", "rev-parse", "describe", "blame", "ls-files",
	"shortlog", "rev-list", "grep",
)

func setOf(items ...string) map[string]bool {
	out := make(map[string]bool, len(items))
	for _, item := range items {
		out[item] = true
	}
	return out
}

// SourceImports returns third-party imports in a source file.
func SourceImports(relative, content string) map[string]bool {
	suffix := ""
	if i := strings.LastIndex(relative, "."); i >= 0 {
		suffix = strings.ToLower(relative[i+1:])
	}
	switch suffix {
	case "js", "jsx", "ts", "tsx", "mjs", "cjs", "vue", "svelte":
		return jsPackages(content)
	case "py":
		return pyPackages(content)
	case "go":
		return goPackages(content)
	case "rs":
		return rustPackages(content)
	default:
		return map[string]bool{}
	}
}

// ManifestPackages returns dependency names declared by a manifest.
func ManifestPackages(relative, content string) (map[string]bool, error) {
	name := strings.ToLower(relative)
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	switch name {
	case "package.json":
		return packageJSON(content)
	case "cargo.toml":
		return cargoPackages(content), nil
	case "go.mod":
		return goModPackages(content), nil
	case "pyproject.toml":
		return pyprojectPackages(content)
	case "requirements.txt":
		return requirementPackages(content), nil
	case "gemfile":
		return listSet(gemLine.FindAllStringSubmatch(content, -1)), nil
	case "composer.json":
		return composerPackages(content)
	case "mix.exs":
		return listSet(mixDep.FindAllStringSubmatch(content, -1)), nil
	case "pipfile":
		return pipfilePackages(content)
	default:
		return nil, &ManifestError{Msg: "Unsupported manifest " + relative}
	}
}

func listSet(matches [][]string) map[string]bool {
	out := map[string]bool{}
	for _, match := range matches {
		if len(match) > 1 && match[1] != "" {
			out[match[1]] = true
		}
	}
	return out
}

// SplitSegments returns the source of each simple command in a shell line.
func SplitSegments(command string) []string {
	var parts []string
	for _, segment := range Parse(command) {
		parts = append(parts, segment.Text)
	}
	return parts
}

// Argv is the command and its arguments, without leading VAR=value assignments.
func (s Segment) Argv() []string {
	parts := s.Words
	for len(parts) > 0 && envAssign.MatchString(parts[0]) {
		parts = parts[1:]
	}
	return parts
}

// Name is the command's base name.
func (s Segment) Name() string {
	parts := s.Argv()
	if len(parts) == 0 {
		return ""
	}
	command := parts[0]
	if i := strings.LastIndexAny(command, `/\`); i >= 0 {
		command = command[i+1:]
	}
	return strings.TrimSuffix(command, ".exe")
}

// IsLintel reports a segment that runs the lintel gate itself.
func IsLintel(s Segment) bool {
	return s.Name() == "lintel" && !s.Substitution
}

// LintelVerb returns the subcommand of the first lintel segment, or "".
func LintelVerb(command string) string {
	for _, segment := range Parse(command) {
		if IsLintel(segment) {
			return segmentVerb(segment)
		}
	}
	return ""
}

// RunsLintel reports whether any lintel segment of command runs verb.
func RunsLintel(command, verb string) bool {
	for _, segment := range Parse(command) {
		if IsLintel(segment) && segmentVerb(segment) == verb {
			return true
		}
	}
	return false
}

func segmentVerb(segment Segment) string {
	for _, part := range segment.Argv()[1:] {
		if !strings.HasPrefix(part, "-") {
			return part
		}
	}
	return ""
}

var approveText = regexp.MustCompile(`(?i)\blintel(\.exe)?['"]?\s+approve\b`)

// wrappers run their arguments as a command.
var wrappers = setOf("sh", "bash", "zsh", "dash", "fish", "eval", "exec", "xargs", "env", "nohup",
	"timeout", "nice", "sudo", "command", "watch", "script", "su", "doas", "time")

// MentionsApprove reports a command that could run lintel approve, directly or
// through a wrapper such as sh -c. Text passed to echo or printf is not a run.
func MentionsApprove(command string) bool {
	if LintelVerb(command) == "approve" {
		return true
	}
	for _, segment := range Parse(command) {
		if wrappers[segment.Name()] && approveText.MatchString(strings.Join(segment.Argv(), " ")) {
			return true
		}
	}
	return false
}

// IsReadOnly reports a command that does not change the project by itself.
// Redirect targets are checked separately.
func IsReadOnly(s Segment) bool {
	if s.Substitution {
		return false
	}
	parts := s.Argv()
	if len(parts) == 0 {
		return false
	}
	switch command := s.Name(); command {
	case "sudo":
		return false
	case "git":
		return len(parts) >= 2 && gitRead[parts[1]]
	case "go":
		return len(parts) >= 2 && goRead[parts[1]]
	case "find":
		for _, arg := range parts {
			if arg == "-delete" || arg == "-exec" || arg == "-execdir" || arg == "-ok" || arg == "-fprint" {
				return false
			}
		}
		return true
	default:
		return readOnly[command]
	}
}

// IsReadOnlySegment reports a command line piece that does not change the project.
func IsReadOnlySegment(segment string) bool {
	parsed := Parse(segment)
	if len(parsed) != 1 {
		return false
	}
	return len(parsed[0].Writes) == 0 && IsReadOnly(parsed[0])
}

// HasWriteRedirect reports an output redirect to a file.
func HasWriteRedirect(segment string) bool {
	return len(RedirectTargets(segment)) > 0
}

// RedirectTargets returns files a command writes through redirection.
// Quoted text, fd duplication such as 2>&1, and /dev/null are not writes.
func RedirectTargets(command string) []string {
	var targets []string
	for _, segment := range Parse(command) {
		targets = append(targets, segment.Writes...)
	}
	return targets
}

// PackagesFromShell returns packages a command would install.
// The boolean is false when the command is not an install.
func PackagesFromShell(command string) (map[string]bool, bool) {
	found := false
	packages := map[string]bool{}
	for _, segment := range Parse(command) {
		added, ok := packagesFromArgv(segment.Argv())
		if !ok {
			continue
		}
		found = true
		for name := range added {
			packages[name] = true
		}
	}
	if !found {
		return nil, false
	}
	return packages, true
}

func jsPackages(content string) map[string]bool {
	out := map[string]bool{}
	for _, match := range jsSpec.FindAllStringSubmatch(content, -1) {
		if pkg := jsPackage(match[1]); pkg != "" {
			out[pkg] = true
		}
	}
	return out
}

func jsPackage(spec string) string {
	if strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../") || strings.HasPrefix(spec, "/") || strings.HasPrefix(spec, "@/") || strings.HasPrefix(spec, "node:") {
		return ""
	}
	if strings.HasPrefix(spec, "@") {
		rest := spec[1:]
		scope, name, ok := strings.Cut(rest, "/")
		if !ok || scope == "" || name == "" {
			return ""
		}
		name, _, _ = strings.Cut(name, "/")
		return "@" + scope + "/" + name
	}
	root, _, _ := strings.Cut(spec, "/")
	if root == "" || nodeBuiltins[root] {
		return ""
	}
	return root
}

func pyPackages(content string) map[string]bool {
	out := map[string]bool{}
	for _, match := range pyImport.FindAllStringSubmatch(content, -1) {
		name := match[1]
		if pyStdlib[name] || name == "__future__" {
			continue
		}
		out[name] = true
	}
	return out
}

func goPackages(content string) map[string]bool {
	specs := map[string]bool{}
	for _, match := range goSingle.FindAllStringSubmatch(content, -1) {
		specs[match[1]] = true
	}
	for _, match := range goBlock.FindAllStringSubmatch(content, -1) {
		for _, quoted := range goQuoted.FindAllStringSubmatch(match[1], -1) {
			specs[quoted[1]] = true
		}
	}
	out := map[string]bool{}
	for spec := range specs {
		head, _, _ := strings.Cut(spec, "/")
		if strings.Contains(head, ".") {
			out[spec] = true
		}
	}
	return out
}

func rustPackages(content string) map[string]bool {
	out := map[string]bool{}
	for _, match := range rustUse.FindAllStringSubmatch(content, -1) {
		if !rustPrelude[match[1]] {
			out[match[1]] = true
		}
	}
	for _, match := range rustExtern.FindAllStringSubmatch(content, -1) {
		if !rustPrelude[match[1]] {
			out[match[1]] = true
		}
	}
	return out
}

func packageJSON(content string) (map[string]bool, error) {
	var data map[string]any
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		return nil, &ManifestError{Msg: "package.json is not valid JSON"}
	}
	if data == nil {
		return nil, &ManifestError{Msg: "package.json must be an object"}
	}
	names := map[string]bool{}
	for _, key := range []string{"dependencies", "devDependencies", "optionalDependencies", "peerDependencies"} {
		deps, _ := data[key].(map[string]any)
		for name := range deps {
			names[name] = true
		}
	}
	return names, nil
}

func cargoPackages(content string) map[string]bool {
	names := map[string]bool{}
	inDeps := false
	for _, line := range strings.Split(content, "\n") {
		stripped := strings.TrimSpace(line)
		if strings.HasPrefix(stripped, "[") && strings.HasSuffix(stripped, "]") {
			section := strings.Trim(strings.TrimSpace(stripped[1:len(stripped)-1]), `"`)
			inDeps = section == "dependencies" || strings.HasSuffix(section, ".dependencies")
			inDeps = inDeps || section == "dev-dependencies" || section == "build-dependencies"
			inDeps = inDeps || strings.HasSuffix(section, ".dev-dependencies")
			continue
		}
		if !inDeps || stripped == "" || strings.HasPrefix(stripped, "#") {
			continue
		}
		if match := cargoKey.FindStringSubmatch(stripped); match != nil {
			names[match[1]] = true
		}
	}
	return names
}

func goModPackages(content string) map[string]bool {
	names := map[string]bool{}
	inBlock := false
	for _, line := range strings.Split(content, "\n") {
		stripped := line
		if i := strings.Index(stripped, "//"); i >= 0 {
			stripped = stripped[:i]
		}
		stripped = strings.TrimSpace(stripped)
		if stripped == "" {
			continue
		}
		if strings.HasPrefix(stripped, "require (") {
			inBlock = true
			continue
		}
		if inBlock && stripped == ")" {
			inBlock = false
			continue
		}
		if strings.HasPrefix(stripped, "require ") && !strings.HasSuffix(stripped, "(") {
			parts := strings.Fields(stripped)
			if len(parts) >= 2 {
				names[parts[1]] = true
			}
			continue
		}
		if inBlock {
			names[strings.Fields(stripped)[0]] = true
		}
	}
	return names
}

func requirementPackages(content string) map[string]bool {
	names := map[string]bool{}
	for _, line := range strings.Split(content, "\n") {
		stripped := strings.TrimSpace(line)
		if stripped == "" || strings.HasPrefix(stripped, "#") || strings.HasPrefix(stripped, "-") {
			continue
		}
		if name := pep508Name(stripped); name != "" {
			names[name] = true
		}
	}
	return names
}

func composerPackages(content string) (map[string]bool, error) {
	var data map[string]any
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		return nil, &ManifestError{Msg: "composer.json is not valid JSON"}
	}
	names := map[string]bool{}
	for _, key := range []string{"require", "require-dev"} {
		deps, _ := data[key].(map[string]any)
		for name := range deps {
			if name != "php" {
				names[name] = true
			}
		}
	}
	return names, nil
}

func pep508Name(dep string) string {
	parts := pep508.Split(dep, 2)
	return strings.TrimSpace(parts[0])
}

func packagesFromArgv(parts []string) (map[string]bool, bool) {
	if len(parts) == 0 {
		return nil, false
	}
	command := parts[0]
	if i := strings.LastIndex(command, "/"); i >= 0 {
		command = command[i+1:]
	}
	if command == "npx" {
		var args []string
		for _, token := range parts[1:] {
			if !strings.HasPrefix(token, "-") {
				args = append(args, token)
			}
		}
		if len(args) == 0 {
			return map[string]bool{}, true
		}
		name := stripVersion(args[0])
		if name == "" {
			return map[string]bool{}, true
		}
		return map[string]bool{name: true}, true
	}
	if command == "uv" {
		return uvPackages(parts)
	}
	verbs := installVerbs[command]
	if verbs == nil || len(parts) < 2 || !verbs[parts[1]] {
		return nil, false
	}
	if (command == "cargo" || command == "poetry" || command == "go") && (parts[1] == "new" || parts[1] == "init") {
		return map[string]bool{}, true
	}
	if (command == "npm" || command == "pnpm" || command == "yarn" || command == "bun") && (parts[1] == "ci" || parts[1] == "init") {
		return map[string]bool{}, true
	}
	if parts[1] == "create" {
		args := packageArgs(parts[2:])
		out := map[string]bool{}
		if len(args) > 0 {
			out[args[0]] = true
		}
		return out, true
	}
	out := map[string]bool{}
	for _, name := range packageArgs(parts[2:]) {
		out[name] = true
	}
	return out, true
}

func uvPackages(parts []string) (map[string]bool, bool) {
	if len(parts) >= 2 && parts[1] == "add" {
		out := map[string]bool{}
		for _, name := range packageArgs(parts[2:]) {
			out[name] = true
		}
		return out, true
	}
	if len(parts) >= 3 && parts[1] == "pip" && parts[2] == "install" {
		out := map[string]bool{}
		for _, name := range packageArgs(parts[3:]) {
			out[name] = true
		}
		return out, true
	}
	return nil, false
}

func packageArgs(tokens []string) []string {
	var packages []string
	skip := false
	for _, token := range tokens {
		if skip {
			skip = false
			continue
		}
		if valueFlags[token] {
			skip = true
			continue
		}
		if strings.HasPrefix(token, "-") {
			continue
		}
		if token == "." || token == "./..." || token == "..." {
			continue
		}
		if name := stripVersion(token); name != "" {
			packages = append(packages, name)
		}
	}
	return packages
}

func stripVersion(token string) string {
	text := strings.Trim(token, `'"`)
	if strings.HasPrefix(text, "@") {
		rest := text[1:]
		if i := strings.Index(rest, "@"); i >= 0 {
			rest = rest[:i]
		}
		if rest == "" {
			return ""
		}
		return "@" + rest
	}
	head, _, _ := strings.Cut(text, "@")
	return head
}

type unbalanced struct{}

func (unbalanced) Error() string { return "unbalanced quotes" }

var errUnbalanced = unbalanced{}

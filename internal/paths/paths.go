// Package paths classifies project paths and matches gate globs.
package paths

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// CatchAll patterns do not unlock schema, CI, Docker, or framework config.
var CatchAll = map[string]bool{
	"**": true, "*": true, "**/*": true, "**/**": true,
}

var manifestNames = map[string]bool{
	"package.json": true, "cargo.toml": true, "go.mod": true,
	"pyproject.toml": true, "requirements.txt": true, "gemfile": true,
	"composer.json": true, "mix.exs": true, "pipfile": true,
}

var lockfileNames = map[string]bool{
	"package-lock.json": true, "pnpm-lock.yaml": true, "yarn.lock": true,
	"bun.lock": true, "bun.lockb": true, "cargo.lock": true, "go.sum": true,
	"poetry.lock": true, "gemfile.lock": true, "composer.lock": true,
	"pipfile.lock": true,
}

var sourceSuffixes = map[string]bool{
	".py": true, ".js": true, ".jsx": true, ".ts": true, ".tsx": true,
	".mjs": true, ".cjs": true, ".go": true, ".rs": true, ".vue": true, ".svelte": true,
}

var schemaNames = map[string]bool{
	"schema.prisma": true, "schema.graphql": true, "schema.sql": true, "schema.rb": true,
}

var framework = regexp.MustCompile(`(?i)(^|/)(next\.config\.[a-z0-9]+|vite\.config\.[a-z0-9]+|astro\.config\.[a-z0-9]+|nuxt\.config\.[a-z0-9]+|svelte\.config\.[a-z0-9]+|vue\.config\.[a-z0-9]+|webpack\.config\.[a-z0-9]+|angular\.json)$`)

// Error is a user-facing path error.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

// NormalizePattern cleans a scope glob.
func NormalizePattern(pattern string) string {
	text := strings.TrimSpace(strings.ReplaceAll(pattern, "\\", "/"))
	return strings.TrimPrefix(text, "./")
}

// IsCatchAll reports whether a pattern is a whole-tree glob.
func IsCatchAll(pattern string) bool {
	return CatchAll[NormalizePattern(pattern)]
}

// GlobMatch reports whether pattern matches a relative path.
func GlobMatch(pattern, relative string) bool {
	re, err := regexp.Compile(GlobToRegex(pattern))
	if err != nil {
		return false
	}
	return re.MatchString(strings.ReplaceAll(relative, "\\", "/"))
}

// GlobToRegex converts a gate glob to an anchored regular expression.
func GlobToRegex(pattern string) string {
	text := NormalizePattern(pattern)
	var parts []string
	for index := 0; index < len(text); {
		if strings.HasPrefix(text[index:], "**/") {
			parts = append(parts, "(?:.*/)?")
			index += 3
			continue
		}
		if strings.HasPrefix(text[index:], "**") {
			parts = append(parts, ".*")
			index += 2
			continue
		}
		switch text[index] {
		case '*':
			parts = append(parts, "[^/]*")
		case '?':
			parts = append(parts, "[^/]")
		default:
			parts = append(parts, regexp.QuoteMeta(text[index:index+1]))
		}
		index++
	}
	return "^" + strings.Join(parts, "") + "$"
}

// Relativize returns a project-relative slash path or an error when raw is outside root.
func Relativize(root, raw string) (string, error) {
	base, err := filepath.Abs(root)
	if err != nil {
		return "", &Error{Msg: raw + " is outside the project."}
	}
	var candidate string
	if filepath.IsAbs(raw) {
		candidate = raw
	} else {
		candidate = filepath.Join(base, raw)
	}
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return "", &Error{Msg: raw + " is outside the project."}
	}
	rel, err := filepath.Rel(base, candidate)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", &Error{Msg: raw + " is outside the project."}
	}
	return path.Clean(filepath.ToSlash(rel)), nil
}

// IsGatePath reports paths that belong to the Lintel store.
func IsGatePath(relative string) bool {
	return relative == ".lintel" || strings.HasPrefix(relative, ".lintel/")
}

// IsGateConfig reports the one file in .lintel an agent may edit.
func IsGateConfig(relative string) bool {
	return relative == ".lintel/config.toml"
}

// IsProtected reports gate state and hook config. Only lintel commands write
// them, so an agent cannot approve its own decision or remove the hook.
func IsProtected(relative string) bool {
	if IsGatePath(relative) {
		return !IsGateConfig(relative)
	}
	switch relative {
	case ".cursor/hooks.json", ".gemini/settings.json", ".codex/hooks.json", ".codex/config.toml":
		return true
	}
	return strings.HasPrefix(relative, ".claude/settings") && strings.HasSuffix(relative, ".json")
}

// scratch are harness working directories under the home directory. Settings
// files that could switch the hooks off are deliberately not listed.
var scratch = []string{
	".claude/plans", ".claude/projects", ".claude/todos",
	".codex/sessions", ".codex/memories", ".gemini/tmp", ".cursor/plans",
}

// Resolve returns an absolute, cleaned path for raw, relative to root.
func Resolve(root, raw string) string {
	if raw == "~" || strings.HasPrefix(raw, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			raw = filepath.Join(home, strings.TrimPrefix(raw, "~"))
		}
	}
	if !filepath.IsAbs(raw) {
		raw = filepath.Join(root, raw)
	}
	return filepath.Clean(raw)
}

// IsScratch reports an absolute path outside the project that an agent may
// write: the system temp directory and the harness plan, memory, and session
// directories.
func IsScratch(absolute string) bool {
	if under(os.TempDir(), absolute) {
		return true
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	for _, dir := range scratch {
		if under(filepath.Join(home, dir), absolute) {
			return true
		}
	}
	return false
}

func under(dir, absolute string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), absolute)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// FileName returns the last path segment.
func FileName(relative string) string {
	return path.Base(strings.ReplaceAll(relative, "\\", "/"))
}

// IsManifest reports dependency manifests.
func IsManifest(relative string) bool {
	return manifestNames[strings.ToLower(FileName(relative))]
}

// IsLockfile reports lockfiles the agent must not hand-edit.
func IsLockfile(relative string) bool {
	return lockfileNames[strings.ToLower(FileName(relative))]
}

// IsSource reports source files whose imports are checked.
func IsSource(relative string) bool {
	return sourceSuffixes[strings.ToLower(path.Ext(FileName(relative)))]
}

// IsSensitive reports schema, CI, Docker, and framework config.
func IsSensitive(relative string) bool {
	name := FileName(relative)
	lowered := strings.ToLower(name)
	posix := strings.ReplaceAll(relative, "\\", "/")
	if lowered == "dockerfile" || strings.HasPrefix(lowered, "dockerfile.") {
		return true
	}
	switch lowered {
	case "docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml":
		return true
	}
	if strings.HasPrefix(posix, ".github/workflows/") || strings.Contains("/"+posix, "/.github/workflows/") {
		return true
	}
	if strings.HasPrefix(posix, "migrations/") || strings.Contains("/"+posix, "/migrations/") {
		return true
	}
	if strings.HasPrefix(posix, "migration/") || strings.Contains("/"+posix, "/migration/") {
		return true
	}
	if schemaNames[lowered] {
		return true
	}
	return framework.MatchString(posix)
}

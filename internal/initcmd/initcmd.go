// Package initcmd installs the gate into a project.
package initcmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"lintel/internal/instructions"
)

// Tools are the harnesses that receive a hook.
var Tools = []string{"cursor", "claude", "codex", "gemini"}

// HookCommand is the lintel binary plus the hook subcommand. When the running
// binary is the lintel on PATH it is written bare, so a committed hook config
// works on every machine and OS. Any other binary is written as its absolute
// path, quoted for the OS.
func HookCommand() string {
	exe, _ := os.Executable()
	found, _ := exec.LookPath("lintel")
	return hookCommand(exe, found, runtime.GOOS == "windows")
}

func hookCommand(exe, found string, windows bool) string {
	if exe == "" || sameFile(exe, found) {
		return "lintel hook"
	}
	if windows {
		return windowsQuote(exe) + " hook"
	}
	return shellQuote(exe) + " hook"
}

func sameFile(a, b string) bool {
	if b == "" {
		return false
	}
	left, err := os.Stat(a)
	if err != nil {
		return false
	}
	right, err := os.Stat(b)
	return err == nil && os.SameFile(left, right)
}

// windowsQuote writes a Windows path with forward slashes, which Git Bash,
// cmd, and PowerShell all accept, and double-quotes it when it has a space.
func windowsQuote(value string) string {
	value = strings.ReplaceAll(value, `\`, "/")
	if strings.ContainsAny(value, " \t") {
		return `"` + value + `"`
	}
	return value
}

// Project writes .lintel, LINTEL.md, and harness hooks.
func Project(root string) ([]string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	var notes []string
	decisions := filepath.Join(root, ".lintel", "decisions")
	if err := os.MkdirAll(decisions, 0o755); err != nil {
		return nil, err
	}
	notes = append(notes, writeIfMissing(filepath.Join(decisions, ".gitkeep"), ""))
	notes = append(notes, writeIfMissing(filepath.Join(root, ".lintel", "config.toml"), instructions.ConfigTOML))
	notes = append(notes, writeIfMissing(filepath.Join(root, ".lintel", ".gitignore"), "window-*.pid\n"))
	notes = append(notes, importJev(root))
	notes = append(notes, writeIfMissing(filepath.Join(root, "LINTEL.md"), instructions.Markdown))
	for _, name := range []string{"AGENTS.md", "CLAUDE.md", "GEMINI.md"} {
		notes = append(notes, pointAt(filepath.Join(root, name)))
	}
	command := HookCommand()
	notes = append(notes, mergeJSON(filepath.Join(root, ".cursor", "hooks.json"), cursorDocument(command)))
	notes = append(notes, mergeJSON(filepath.Join(root, ".claude", "settings.json"), claudeDocument(command)))
	notes = append(notes, mergeJSON(filepath.Join(root, ".codex", "hooks.json"), codexDocument(command)))
	notes = append(notes, mergeJSON(filepath.Join(root, ".gemini", "settings.json"), geminiDocument(command)))
	var present []string
	for _, note := range notes {
		if note != "" {
			present = append(present, note)
		}
	}
	return present, nil
}

func cursorDocument(command string) map[string]any {
	hook := map[string]any{"command": command + " cursor", "failClosed": true}
	pre := map[string]any{"command": command + " cursor", "failClosed": true, "matcher": "Write|StrReplace|EditNotebook|Delete|Shell"}
	return map[string]any{
		"version": 1,
		"hooks": map[string]any{
			"preToolUse":           []any{pre},
			"beforeShellExecution": []any{hook},
			"sessionStart":         []any{map[string]any{"command": command + " cursor"}},
			"stop":                 []any{map[string]any{"command": command + " cursor", "loop_limit": 2}},
		},
	}
}

// commandHooks is the matcher-and-hooks entry that Claude, Codex, and Gemini share.
func commandHooks(matcher string, hook map[string]any) []any {
	entry := map[string]any{"hooks": []any{hook}}
	if matcher != "" {
		entry["matcher"] = matcher
	}
	return []any{entry}
}

func claudeDocument(command string) map[string]any {
	hook := map[string]any{"type": "command", "command": command + " claude", "timeout": 15}
	return map[string]any{
		"hooks": map[string]any{
			"PreToolUse":   commandHooks("Write|Edit|MultiEdit|Bash|PowerShell|NotebookEdit", hook),
			"SessionStart": commandHooks("", hook),
			"Stop":         commandHooks("", hook),
			"Notification": commandHooks("", hook),
		},
	}
}

func codexDocument(command string) map[string]any {
	hook := map[string]any{"type": "command", "command": command + " codex", "timeout": 15}
	pre := map[string]any{"type": "command", "command": command + " codex", "timeout": 15, "statusMessage": "Checking Lintel"}
	return map[string]any{
		"description": "Lintel change gate. Trust this hook with /hooks before Codex will run it.",
		"hooks": map[string]any{
			"PreToolUse":   commandHooks("Bash|apply_patch|Edit|Write", pre),
			"SessionStart": commandHooks("", hook),
			"Stop":         commandHooks("", hook),
		},
	}
}

func geminiDocument(command string) map[string]any {
	hook := map[string]any{"name": "lintel", "type": "command", "command": command + " gemini", "timeout": 10000}
	return map[string]any{
		"hooks": map[string]any{
			"BeforeTool":   commandHooks("write_file|replace|run_shell_command|shell", hook),
			"SessionStart": commandHooks("", hook),
			"AfterAgent":   commandHooks("", hook),
			"Notification": commandHooks("", hook),
		},
	}
}

// importJev copies decisions from the Python prototype's .jev store the first
// time Lintel is installed, so an approved kickoff carries over.
func importJev(root string) string {
	target := filepath.Join(root, ".lintel", "decisions")
	if existing, _ := filepath.Glob(filepath.Join(target, "*.json")); len(existing) > 0 {
		return ""
	}
	sources, _ := filepath.Glob(filepath.Join(root, ".jev", "decisions", "*.json"))
	count := 0
	for _, source := range sources {
		raw, err := os.ReadFile(source)
		if err != nil {
			continue
		}
		if err := os.WriteFile(filepath.Join(target, filepath.Base(source)), raw, 0o644); err == nil {
			count++
		}
	}
	if count == 0 {
		return ""
	}
	return fmt.Sprintf("imported %d decisions from .jev", count)
}

func writeIfMissing(path, content string) string {
	if _, err := os.Stat(path); err == nil {
		return "kept " + display(path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return ""
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return ""
	}
	return "wrote " + display(path)
}

func pointAt(path string) string {
	title := map[string]string{
		"AGENTS.md": "# Agent instructions\n\n",
		"CLAUDE.md": "# Claude instructions\n\n",
		"GEMINI.md": "# Gemini instructions\n\n",
	}[filepath.Base(path)]
	raw, err := os.ReadFile(path)
	if err != nil {
		if err := os.WriteFile(path, []byte(title+instructions.Pointer), 0o644); err != nil {
			return ""
		}
		return "wrote " + display(path)
	}
	text := string(raw)
	if strings.Contains(text, "LINTEL.md") {
		return "kept " + display(path)
	}
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	if err := os.WriteFile(path, []byte(text+"\n"+instructions.Pointer), 0o644); err != nil {
		return ""
	}
	return "updated " + display(path)
}

// mergeJSON installs the Lintel hooks into a harness config. Earlier Lintel or
// Jev hook entries are replaced, so re-running init repairs a stale hook, and
// every other entry is kept.
func mergeJSON(path string, incoming map[string]any) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return ""
		}
		encoded, err := json.MarshalIndent(incoming, "", "  ")
		if err != nil {
			return ""
		}
		if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
			return ""
		}
		return "wrote " + display(path)
	}
	var current map[string]any
	if err := json.Unmarshal(raw, &current); err != nil {
		return "kept " + display(path) + " (invalid JSON, left unchanged)"
	}
	before, _ := json.Marshal(current)
	stripOwnHooks(current)
	merged := mergeObjects(current, incoming)
	after, err := json.Marshal(merged)
	if err != nil {
		return ""
	}
	if string(before) == string(after) {
		return "kept " + display(path)
	}
	encoded, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return ""
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o644); err != nil {
		return ""
	}
	return "updated " + display(path)
}

// ownHook reports a hook command that this binary, another Lintel build, or
// the Jev prototype installed.
func ownHook(command string) bool {
	if strings.HasPrefix(command, HookCommand()+" ") {
		return true
	}
	return strings.Contains(command, " hook ") && (strings.Contains(command, "lintel") || strings.Contains(command, "jev"))
}

func stripOwnHooks(document map[string]any) {
	events, ok := document["hooks"].(map[string]any)
	if !ok {
		return
	}
	for event, value := range events {
		entries, ok := value.([]any)
		if !ok {
			continue
		}
		var kept []any
		for _, entry := range entries {
			object, ok := entry.(map[string]any)
			if !ok {
				kept = append(kept, entry)
				continue
			}
			if command, ok := object["command"].(string); ok && ownHook(command) {
				continue
			}
			if inner, ok := object["hooks"].([]any); ok {
				var rest []any
				for _, hook := range inner {
					if item, ok := hook.(map[string]any); ok {
						if command, ok := item["command"].(string); ok && ownHook(command) {
							continue
						}
					}
					rest = append(rest, hook)
				}
				if len(rest) == 0 {
					continue
				}
				object["hooks"] = rest
			}
			kept = append(kept, object)
		}
		if len(kept) == 0 {
			delete(events, event)
		} else {
			events[event] = kept
		}
	}
}

func mergeObjects(current, incoming map[string]any) map[string]any {
	for key, value := range incoming {
		existing, ok := current[key]
		if !ok {
			current[key] = value
			continue
		}
		left, leftOK := existing.(map[string]any)
		right, rightOK := value.(map[string]any)
		if leftOK && rightOK {
			current[key] = mergeObjects(left, right)
			continue
		}
		leftList, leftListOK := existing.([]any)
		rightList, rightListOK := value.([]any)
		if leftListOK && rightListOK {
			current[key] = append(leftList, rightList...)
		}
	}
	return current
}

func display(path string) string {
	return filepath.ToSlash(path)
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if !strings.ContainsAny(value, " \t\n'\"\\$") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

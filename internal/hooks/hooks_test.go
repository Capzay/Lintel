package hooks_test

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"lintel/internal/cli"
	"lintel/internal/hooks"
	"lintel/internal/initcmd"
	"lintel/internal/model"
	"lintel/internal/store"
)

const patch = `*** Begin Patch
*** Add File: src/app.py
+import requests
*** Update File: README.md
+hello
*** End Patch
`

func TestPatchAndToolPayloadsBecomeWrites(t *testing.T) {
	action := hooks.ActionFromPatch(patch)
	if len(action.Paths) != 2 || action.Paths[0] != "src/app.py" || action.Paths[1] != "README.md" {
		t.Fatal(action.Paths)
	}
	if !strings.Contains(action.Contents["src/app.py"], "import requests") {
		t.Fatal(action.Contents)
	}
	cursor := hooks.ActionFromPayload(map[string]any{
		"tool_name": "StrReplace", "tool_input": map[string]any{"path": "src/App.tsx", "new_string": "import zod from 'zod';\n"},
	})
	if cursor.Kind != "write" || cursor.Contents["src/App.tsx"] != "import zod from 'zod';\n" {
		t.Fatal(cursor)
	}
	shell := hooks.ActionFromPayload(map[string]any{"command": "ls"})
	if shell.Kind != "shell" || shell.Command != "ls" {
		t.Fatal(shell)
	}
}

func TestEachToolRendersAllowAndDeny(t *testing.T) {
	allowed := model.GateResult{Allow: true}
	denied := model.GateResult{Allow: false, UserMessage: "Blocked for you.", AgentMessage: "Propose first."}
	body, err := hooks.Render("cursor", allowed)
	if err != nil || body["permission"] != "allow" {
		t.Fatal(body, err)
	}
	cursor, err := hooks.Render("cursor", denied)
	if err != nil || cursor["permission"] != "deny" || cursor["user_message"] != "Blocked for you." || cursor["agent_message"] != "Propose first." {
		t.Fatal(cursor, err)
	}
	for _, tool := range []string{"claude", "codex"} {
		deny, err := hooks.Render(tool, denied)
		if err != nil {
			t.Fatal(err)
		}
		output := deny["hookSpecificOutput"].(map[string]any)
		if output["permissionDecision"] != "deny" || output["permissionDecisionReason"] != "Propose first." {
			t.Fatal(output)
		}
		if allow, _ := hooks.Render(tool, allowed); len(allow) != 0 {
			t.Fatal("an allow must leave the harness's own permission prompts in place", allow)
		}
	}
	empty, _ := hooks.Render("gemini", allowed)
	if len(empty) != 0 {
		t.Fatal(empty)
	}
	gemini, _ := hooks.Render("gemini", denied)
	if gemini["decision"] != "deny" || gemini["reason"] != "Propose first." {
		t.Fatal(gemini)
	}
}

func acceptReview(store.Decision, map[string]string, []store.Decision) model.ReviewResult {
	return model.ReviewResult{Verdict: "accept", Summary: "Fit.", Concerns: []string{}, Valid: true, Evidence: "none"}
}

func run(t *testing.T, args []string, in string) (int, string) {
	t.Helper()
	var out bytes.Buffer
	code := cli.Execute(args, cli.Option{In: strings.NewReader(in), Out: &out, Err: &bytes.Buffer{}, Review: acceptReview, Notify: func(string, string) {}})
	return code, out.String()
}

func TestInitInstallsEveryAdapterOnce(t *testing.T) {
	root := t.TempDir()
	notes, err := initcmd.Project(root)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(notes, "\n")
	if !strings.Contains(joined, "LINTEL.md") {
		t.Fatal(notes)
	}
	text := mustRead(t, root+"/LINTEL.md")
	if !strings.Contains(text, "Jev reviews") || !strings.Contains(strings.ToLower(text), "human approves") {
		t.Fatal("instructions")
	}
	for _, name := range []string{"AGENTS.md", "CLAUDE.md", "GEMINI.md"} {
		if !strings.Contains(mustRead(t, root+"/"+name), "LINTEL.md") {
			t.Fatal(name)
		}
	}
	cursor := jsonObject(t, mustRead(t, root+"/.cursor/hooks.json"))
	hooksMap := cursor["hooks"].(map[string]any)
	pre := hooksMap["preToolUse"].([]any)[0].(map[string]any)
	if pre["failClosed"] != true || !strings.Contains(pre["command"].(string), "cursor") {
		t.Fatal(pre)
	}
	if _, ok := hooksMap["beforeShellExecution"]; !ok {
		t.Fatal("before shell")
	}
	claude := jsonObject(t, mustRead(t, root+"/.claude/settings.json"))
	claudeHook := claude["hooks"].(map[string]any)["PreToolUse"].([]any)[0].(map[string]any)
	if matcher := claudeHook["matcher"].(string); !strings.Contains(matcher, "Write|Edit") || !strings.Contains(matcher, "Bash") {
		t.Fatal(claudeHook)
	}
	if !strings.Contains(claudeHook["hooks"].([]any)[0].(map[string]any)["command"].(string), "claude") {
		t.Fatal(claudeHook)
	}
	codex := jsonObject(t, mustRead(t, root+"/.codex/hooks.json"))
	if !strings.Contains(codex["hooks"].(map[string]any)["PreToolUse"].([]any)[0].(map[string]any)["matcher"].(string), "apply_patch") {
		t.Fatal(codex)
	}
	if !strings.Contains(codex["description"].(string), "/hooks") {
		t.Fatal(codex["description"])
	}
	gemini := jsonObject(t, mustRead(t, root+"/.gemini/settings.json"))
	geminiHook := gemini["hooks"].(map[string]any)["BeforeTool"].([]any)[0].(map[string]any)
	if !strings.Contains(geminiHook["matcher"].(string), "write_file") {
		t.Fatal(geminiHook)
	}
	if geminiHook["hooks"].([]any)[0].(map[string]any)["name"] != "lintel" {
		t.Fatal(geminiHook)
	}
	if err := osWrite(root+"/AGENTS.md", "# Existing\n"); err != nil {
		t.Fatal(err)
	}
	if err := osWrite(root+"/.cursor/hooks.json", "{\"version\":1,\"hooks\":{\"preToolUse\":[{\"command\":\"other.sh\"}]}}\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := initcmd.Project(root); err != nil {
		t.Fatal(err)
	}
	agents := mustRead(t, root+"/AGENTS.md")
	if !strings.Contains(agents, "# Existing") || !strings.Contains(agents, "LINTEL.md") {
		t.Fatal(agents)
	}
	merged := jsonObject(t, mustRead(t, root+"/.cursor/hooks.json"))
	var commands []string
	for _, item := range merged["hooks"].(map[string]any)["preToolUse"].([]any) {
		commands = append(commands, item.(map[string]any)["command"].(string))
	}
	if !contains(commands, "other.sh") || !containsSub(commands, " hook ") {
		t.Fatal(commands)
	}
	again, err := initcmd.Project(root)
	if err != nil {
		t.Fatal(err)
	}
	kept := false
	for _, note := range again {
		if strings.Contains(note, "kept") && strings.Contains(note, "hooks.json") {
			kept = true
		}
	}
	if !kept {
		t.Fatal(again)
	}
	mergedAgain := jsonObject(t, mustRead(t, root+"/.cursor/hooks.json"))
	if len(mergedAgain["hooks"].(map[string]any)["preToolUse"].([]any)) != len(merged["hooks"].(map[string]any)["preToolUse"].([]any)) {
		t.Fatal("duplicate hook")
	}
}

func TestCLIProposalApprovalAndHooks(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LINTEL_PROJECT", root)
	if _, err := initcmd.Project(root); err != nil {
		t.Fatal(err)
	}
	code, _ := run(t, []string{"propose", "--kind", "kickoff", "--title", "App stack", "--question", "What stack?", "--choice", "Python", "--path", "**", "--package", "requests"}, "")
	if code != 0 {
		t.Fatal(code)
	}
	item := store.Open(root)
	open, err := item.OpenDecisions()
	if err != nil || len(open) != 1 {
		t.Fatal(err, open)
	}
	id := open[0].ID
	code, status := run(t, []string{"status"}, "")
	if code != 0 || !strings.Contains(status, "shut") {
		t.Fatal(status)
	}
	code, _ = run(t, []string{"approve", id, "--note", "Yes"}, "")
	if code != 0 {
		t.Fatal(code)
	}
	loaded, _ := item.Load(id)
	if loaded.Status != "authorized" {
		t.Fatal(loaded.Status)
	}
	code, denied := run(t, []string{"hook", "claude"}, `{"tool_name":"Write","tool_input":{"path":"src/app.py","contents":"import flask\n"}}`)
	if code != 0 {
		t.Fatal(code)
	}
	body := jsonObject(t, denied)
	output := body["hookSpecificOutput"].(map[string]any)
	if output["permissionDecision"] != "deny" || !strings.Contains(output["permissionDecisionReason"].(string), "flask") {
		t.Fatal(body)
	}
	_, allowed := run(t, []string{"hook", "gemini"}, `{"tool_name":"write_file","tool_input":{"file_path":"src/app.py","content":"import requests\n"}}`)
	if strings.TrimSpace(allowed) != "{}" {
		t.Fatal(allowed)
	}
	_, cursor := run(t, []string{"hook", "cursor"}, `{"command":"ls"}`)
	if jsonObject(t, cursor)["permission"] != "allow" {
		t.Fatal(cursor)
	}
	_, broken := run(t, []string{"hook", "codex"}, "not-json")
	if jsonObject(t, broken)["hookSpecificOutput"].(map[string]any)["permissionDecision"] != "deny" {
		t.Fatal(broken)
	}
	code, check := run(t, []string{"check", "--action", "shell", "--command", "pip install flask"}, "")
	if code != 1 || jsonObject(t, check)["allow"] != false {
		t.Fatal(code, check)
	}
}

func proposeKickoff(t *testing.T) (string, store.Project) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("LINTEL_PROJECT", root)
	if _, err := initcmd.Project(root); err != nil {
		t.Fatal(err)
	}
	if code, _ := run(t, []string{"propose", "--kind", "kickoff", "--title", "App stack", "--question", "What stack?", "--choice", "Python", "--path", "**"}, ""); code != 0 {
		t.Fatal(code)
	}
	item := store.Open(root)
	open, _ := item.OpenDecisions()
	return open[0].ID, item
}

func hook(t *testing.T, tool, payload string) map[string]any {
	t.Helper()
	_, output := run(t, []string{"hook", tool}, payload)
	return jsonObject(t, output)
}

func TestWaitBecomesTheHarnessPrompt(t *testing.T) {
	id, item := proposeKickoff(t)
	claude := hook(t, "claude", `{"tool_name":"Bash","tool_input":{"command":"lintel wait"}}`)
	specific := claude["hookSpecificOutput"].(map[string]any)
	if specific["permissionDecision"] != "ask" || !strings.Contains(specific["permissionDecisionReason"].(string), "Allow approves.") {
		t.Fatal(specific)
	}
	if specific["updatedInput"].(map[string]any)["command"] != "lintel approve "+id {
		t.Fatal(specific["updatedInput"])
	}
	gemini := hook(t, "gemini", `{"hook_event_name":"BeforeTool","tool_name":"run_shell_command","tool_input":{"command":"lintel wait"}}`)
	if gemini["decision"] != "ask" || !strings.Contains(gemini["systemMessage"].(string), id) {
		t.Fatal(gemini)
	}
	if gemini["hookSpecificOutput"].(map[string]any)["tool_input"].(map[string]any)["command"] != "lintel approve "+id {
		t.Fatal(gemini)
	}
	cursorWait := hook(t, "cursor", `{"hook_event_name":"beforeShellExecution","command":"lintel wait"}`)
	if cursorWait["permission"] != "deny" || !strings.Contains(cursorWait["agent_message"].(string), "lintel approve "+id) {
		t.Fatal(cursorWait)
	}
	cursorApprove := hook(t, "cursor", `{"hook_event_name":"beforeShellExecution","command":"lintel approve `+id+`"}`)
	if cursorApprove["permission"] != "ask" || !strings.Contains(cursorApprove["user_message"].(string), id) {
		t.Fatal(cursorApprove)
	}
	cursorPre := hook(t, "cursor", `{"hook_event_name":"preToolUse","tool_name":"Shell","tool_input":{"command":"lintel approve `+id+`"}}`)
	if cursorPre["permission"] != "allow" {
		t.Fatal(cursorPre)
	}
	cursorChained := hook(t, "cursor", `{"hook_event_name":"beforeShellExecution","command":"lintel approve `+id+` && rm -rf src"}`)
	if cursorChained["permission"] != "deny" {
		t.Fatal(cursorChained)
	}
	codexWait := hook(t, "codex", `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"lintel wait"}}`)
	if len(codexWait) != 0 {
		t.Fatal(codexWait)
	}
	loaded, _ := item.Load(id)
	if loaded.Status != "reviewed" {
		t.Fatal(loaded.Status)
	}
}

func TestAgentCannotApproveItself(t *testing.T) {
	id, _ := proposeKickoff(t)
	for _, command := range []string{"lintel approve " + id, "bash -c 'lintel approve " + id + "'"} {
		codex := hook(t, "codex", `{"tool_name":"Bash","tool_input":{"command":`+quote(command)+`}}`)
		if codex["hookSpecificOutput"].(map[string]any)["permissionDecision"] != "deny" {
			t.Fatal(command, codex)
		}
		claude := hook(t, "claude", `{"tool_name":"Bash","tool_input":{"command":`+quote(command)+`}}`)
		specific := claude["hookSpecificOutput"].(map[string]any)
		if specific["permissionDecision"] != "ask" || specific["updatedInput"].(map[string]any)["command"] != "lintel approve "+id {
			t.Fatal(command, specific)
		}
	}
	reject := hook(t, "codex", `{"tool_name":"Bash","tool_input":{"command":"lintel reject --note no"}}`)
	if len(reject) != 0 {
		t.Fatal(reject)
	}
}

func TestStopKeepsTheTurnWhileADecisionWaits(t *testing.T) {
	id, item := proposeKickoff(t)
	claude := hook(t, "claude", `{"hook_event_name":"Stop","stop_hook_active":false}`)
	if claude["decision"] != "block" || !strings.Contains(claude["reason"].(string), "lintel wait "+id) {
		t.Fatal(claude)
	}
	if again := hook(t, "claude", `{"hook_event_name":"Stop","stop_hook_active":true}`); len(again) != 0 {
		t.Fatal(again)
	}
	if codex := hook(t, "codex", `{"hook_event_name":"Stop","stop_hook_active":false}`); codex["decision"] != "block" {
		t.Fatal(codex)
	}
	if gemini := hook(t, "gemini", `{"hook_event_name":"AfterAgent","stop_hook_active":false}`); gemini["decision"] != "deny" {
		t.Fatal(gemini)
	}
	cursor := hook(t, "cursor", `{"hook_event_name":"stop","loop_count":0}`)
	if !strings.Contains(cursor["followup_message"].(string), "lintel approve "+id) {
		t.Fatal(cursor)
	}
	if _, err := item.Approve(id, ""); err != nil {
		t.Fatal(err)
	}
	if done := hook(t, "claude", `{"hook_event_name":"Stop","stop_hook_active":false}`); len(done) != 0 {
		t.Fatal(done)
	}
}

func TestSessionStartCarriesTheGateState(t *testing.T) {
	id, _ := proposeKickoff(t)
	claude := hook(t, "claude", `{"hook_event_name":"SessionStart","source":"startup"}`)
	text := claude["hookSpecificOutput"].(map[string]any)["additionalContext"].(string)
	if !strings.Contains(text, id) || !strings.Contains(text, "lintel wait") || !strings.Contains(text, "lintel ask") {
		t.Fatal(text)
	}
	cursor := hook(t, "cursor", `{"hook_event_name":"sessionStart"}`)
	if !strings.Contains(cursor["additional_context"].(string), id) {
		t.Fatal(cursor)
	}
	if note := hook(t, "claude", `{"hook_event_name":"Notification","message":"Claude needs your permission"}`); len(note) != 0 {
		t.Fatal(note)
	}
}

func TestLifecycleHooksOutsideAProjectDoNothing(t *testing.T) {
	t.Setenv("LINTEL_PROJECT", t.TempDir())
	if body := hook(t, "claude", `{"hook_event_name":"Stop"}`); len(body) != 0 {
		t.Fatal(body)
	}
}

func TestInitReplacesTheJevPrototypeHooks(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(root+"/.claude", 0o755); err != nil {
		t.Fatal(err)
	}
	old := `{"permissions":{"allow":["Bash(ls:*)"]},"hooks":{"PreToolUse":[{"matcher":"Write|Edit|Bash|NotebookEdit","hooks":[{"type":"command","command":"/p/.venv/bin/python3 -m jev hook claude","timeout":15}]},{"matcher":"Bash","hooks":[{"type":"command","command":"audit.sh"}]}]}}`
	if err := osWrite(root+"/.claude/settings.json", old); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root+"/.jev/decisions", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := osWrite(root+"/.jev/decisions/2026-09-27-stack.json", `{"id":"2026-09-27-stack","kind":"kickoff","status":"authorized"}`); err != nil {
		t.Fatal(err)
	}
	notes, err := initcmd.Project(root)
	if err != nil {
		t.Fatal(err)
	}
	if !containsSub(notes, "imported 1 decisions from .jev") {
		t.Fatal(notes)
	}
	if _, err := os.Stat(root + "/.lintel/decisions/2026-09-27-stack.json"); err != nil {
		t.Fatal(err)
	}
	text := mustRead(t, root+"/.claude/settings.json")
	if strings.Contains(text, "-m jev hook") || !strings.Contains(text, "audit.sh") || !strings.Contains(text, "Bash(ls:*)") {
		t.Fatal(text)
	}
	settings := jsonObject(t, text)["hooks"].(map[string]any)
	for _, event := range []string{"PreToolUse", "SessionStart", "Stop", "Notification"} {
		if !strings.Contains(mustJSONText(t, settings[event]), " hook claude") {
			t.Fatal(event, settings[event])
		}
	}
	if len(settings["PreToolUse"].([]any)) != 2 {
		t.Fatal(settings["PreToolUse"])
	}
	again, _ := initcmd.Project(root)
	if !containsSub(again, "kept "+root+"/.claude/settings.json") {
		t.Fatal(again)
	}
}

func quote(text string) string {
	raw, _ := json.Marshal(text)
	return string(raw)
}

func mustJSONText(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func jsonObject(t *testing.T, raw string) map[string]any {
	t.Helper()
	var data map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &data); err != nil {
		t.Fatal(err, raw)
	}
	return data
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func osWrite(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsSub(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}

// Claude Code on Windows runs shell commands through its PowerShell tool.
func TestPowerShellWaitBecomesTheHarnessPrompt(t *testing.T) {
	id, _ := proposeKickoff(t)
	claude := hook(t, "claude", `{"hook_event_name":"PreToolUse","tool_name":"PowerShell","tool_input":{"command":"lintel wait"}}`)
	specific := claude["hookSpecificOutput"].(map[string]any)
	if specific["permissionDecision"] != "ask" || specific["updatedInput"].(map[string]any)["command"] != "lintel approve "+id {
		t.Fatal(specific)
	}
}

// A wait that reaches the hook before Jev has reviewed anything, such as
// `lintel propose ... && lintel wait`, would fall back to the Jev window.
func TestWaitBeforeReviewIsDeniedWhereTheHarnessCanAsk(t *testing.T) {
	root := t.TempDir()
	t.Setenv("LINTEL_PROJECT", root)
	if _, err := initcmd.Project(root); err != nil {
		t.Fatal(err)
	}
	chained := `lintel propose --kind kickoff --title x --question y --choice z && lintel wait`
	claude := hook(t, "claude", `{"tool_name":"Bash","tool_input":{"command":"`+chained+`"}}`)
	specific := claude["hookSpecificOutput"].(map[string]any)
	if specific["permissionDecision"] != "deny" || !strings.Contains(specific["permissionDecisionReason"].(string), "separate command") {
		t.Fatal(specific)
	}
	gemini := hook(t, "gemini", `{"hook_event_name":"BeforeTool","tool_name":"run_shell_command","tool_input":{"command":"lintel wait"}}`)
	if gemini["decision"] != "deny" {
		t.Fatal(gemini)
	}
	cursor := hook(t, "cursor", `{"hook_event_name":"beforeShellExecution","command":"lintel wait"}`)
	if cursor["permission"] != "deny" {
		t.Fatal(cursor)
	}
	if codex := hook(t, "codex", `{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"lintel wait"}}`); len(codex) != 0 {
		t.Fatal(codex)
	}
}

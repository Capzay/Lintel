// Package hooks translates each coding tool's hook payload into one gate decision.
package hooks

import (
	"encoding/json"
	"strings"

	"lintel/internal/model"
	"lintel/internal/store"
)

var readTools = map[string]bool{
	"Read": true, "Grep": true, "Glob": true, "SemanticSearch": true, "WebSearch": true,
	"WebFetch": true, "ReadLints": true, "TodoWrite": true, "Task": true,
	"FetchMcpResource": true, "ListMcpResources": true,
}

var shellTools = map[string]bool{
	"Shell": true, "Bash": true, "PowerShell": true, "shell": true, "run_shell_command": true, "ShellCommand": true,
}

var pathKeys = []string{"path", "file_path", "target_file", "notebook_path", "target_notebook"}
var contentKeys = []string{"contents", "content", "new_string", "new_source", "text"}

// ActionFromPayload maps a harness payload to a gate action.
func ActionFromPayload(payload map[string]any) model.Action {
	tool := str(payload["tool_name"])
	if tool == "" {
		tool = str(payload["tool"])
	}
	toolInput, _ := payload["tool_input"].(map[string]any)
	if toolInput == nil {
		toolInput = map[string]any{}
	}
	if tool == "apply_patch" || ((tool == "Edit" || tool == "Write") && looksLikePatch(toolInput)) {
		patch := str(toolInput["command"])
		if patch == "" {
			patch = str(toolInput["input"])
		}
		return ActionFromPatch(patch)
	}
	if shellTools[tool] || (tool == "" && payload["command"] != nil) {
		command := str(toolInput["command"])
		if command == "" {
			command = str(payload["command"])
		}
		return model.Action{Kind: "shell", Command: command}
	}
	if readTools[tool] {
		return model.Action{Kind: "read"}
	}
	return writeAction(toolInput)
}

// ActionFromPatch reads an apply_patch document.
func ActionFromPatch(patch string) model.Action {
	var paths []string
	contents := map[string]string{}
	current := ""
	var added []string
	flush := func() {
		if current != "" {
			paths = append(paths, current)
			if len(added) > 0 {
				contents[current] = strings.Join(added, "\n")
			}
		}
		current = ""
		added = nil
	}
	for _, line := range strings.Split(patch, "\n") {
		if strings.HasPrefix(line, "*** Add File:") || strings.HasPrefix(line, "*** Update File:") || strings.HasPrefix(line, "*** Delete File:") {
			flush()
			current = strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
			added = nil
			continue
		}
		if current != "" && strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			added = append(added, line[1:])
		}
	}
	flush()
	return model.Action{Kind: "write", Paths: paths, Contents: contents}
}

// Hook events, normalized across harnesses.
const (
	EventTool    = "tool"
	EventSession = "session"
	EventStop    = "stop"
	EventNotify  = "notify"
)

// Event reads the lifecycle event from a hook payload. A payload without one
// is a tool call, which is what older hook configs send.
func Event(payload map[string]any) string {
	switch str(payload["hook_event_name"]) {
	case "SessionStart", "sessionStart":
		return EventSession
	case "Stop", "AfterAgent", "stop":
		return EventStop
	case "Notification":
		return EventNotify
	default:
		return EventTool
	}
}

// RawEvent is the harness's own event name.
func RawEvent(payload map[string]any) string {
	return str(payload["hook_event_name"])
}

// CanAsk reports harnesses whose pre-tool hook can raise their own approval
// prompt. Codex 0.156 rejects "ask", so Codex uses the Jev window instead.
func CanAsk(tool string) bool {
	return tool == "claude" || tool == "gemini" || tool == "cursor"
}

// Ask raises the harness's own approval prompt for a reviewed decision. The
// command is rewritten to lintel approve <id> where the harness allows it, so
// Allow approves exactly the decision on the card.
func Ask(tool string, decision store.Decision) (map[string]any, error) {
	card := store.FormatPrompt(decision)
	approve := "lintel approve " + decision.ID
	switch tool {
	case "claude":
		return map[string]any{
			"hookSpecificOutput": map[string]any{
				"hookEventName":            "PreToolUse",
				"permissionDecision":       "ask",
				"permissionDecisionReason": card + "\n\nAllow approves. Deny rejects; add a note to say why.",
				"updatedInput":             map[string]any{"command": approve},
			},
		}, nil
	case "gemini":
		return map[string]any{
			"decision":      "ask",
			"reason":        "Asking the human to approve decision " + decision.ID + ".",
			"systemMessage": card + "\n\nAllow approves. Deny rejects.",
			"hookSpecificOutput": map[string]any{
				"hookEventName": "BeforeTool",
				"tool_input":    map[string]any{"command": approve},
			},
		}, nil
	case "cursor":
		return map[string]any{
			"permission":    "ask",
			"user_message":  card + "\n\nRun approves. Skip leaves it open.",
			"agent_message": "The human is being asked to approve decision " + decision.ID + ".",
		}, nil
	}
	return nil, errUnknown(tool + " decides inside lintel wait")
}

// Stop keeps the agent in the turn while a reviewed decision waits for the
// human. repeat is true when the harness already continued once for this stop,
// and then the turn may end so the loop cannot spin.
func Stop(tool string, open *store.Decision, repeat bool) map[string]any {
	if open == nil || repeat || open.Status != model.StatusReviewed {
		return map[string]any{}
	}
	next := "run `lintel wait " + open.ID + "` now"
	if tool == "cursor" {
		next = "run `lintel approve " + open.ID + "` now; Cursor asks the human"
	}
	reason := "Decision " + open.ID + " is waiting for the human. Do not end the turn to ask in prose: " + next +
		". If the human already denied it, record that with `lintel reject " + open.ID + " --note \"<their words>\"` and stop."
	switch tool {
	case "cursor":
		return map[string]any{"followup_message": reason}
	case "gemini":
		return map[string]any{"decision": "deny", "reason": reason}
	default:
		return map[string]any{"decision": "block", "reason": reason}
	}
}

// Session gives a new session the gate state, so the agent does not depend on
// having read LINTEL.md.
func Session(tool, status string) map[string]any {
	text := "This project uses the Lintel change gate (see LINTEL.md).\n" + status + "\n" +
		"Unsure which package or approach fits? Run `lintel ask --question ... --option id:description --option ...` and Jev recommends one from this codebase.\n" +
		"Before new packages, architecture, schema, CI, Docker, or framework config: `lintel propose`, then run `lintel wait` in the same turn. " +
		"The human approves or rejects; never run `lintel approve` to approve on their behalf, and never edit .lintel/ directly."
	if tool == "cursor" {
		return map[string]any{"additional_context": text}
	}
	return map[string]any{
		"hookSpecificOutput": map[string]any{"hookEventName": "SessionStart", "additionalContext": text},
	}
}

// Allowed is an explicit allow for one harness.
func Allowed(tool string) map[string]any {
	body, err := Render(tool, model.Allow())
	if err != nil {
		return map[string]any{}
	}
	return body
}

// Denied is a deny whose message goes to the agent and the human.
func Denied(tool, message string) map[string]any {
	body, err := Render(tool, model.GateResult{Allow: false, UserMessage: message, AgentMessage: message})
	if err != nil {
		return map[string]any{"permission": "deny", "agent_message": message}
	}
	return body
}

// Render encodes an allow or deny for one harness.
func Render(tool string, result model.GateResult) (map[string]any, error) {
	switch tool {
	case "cursor":
		if result.Allow {
			return map[string]any{"permission": "allow"}, nil
		}
		return map[string]any{
			"permission": "deny", "user_message": result.UserMessage, "agent_message": result.AgentMessage,
		}, nil
	case "claude", "codex":
		// An allow says nothing, so the harness's own permission rules and
		// prompts still apply. An explicit allow would skip them.
		if result.Allow {
			return map[string]any{}, nil
		}
		return map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": result.AgentMessage,
		}}, nil
	case "gemini":
		if result.Allow {
			return map[string]any{}, nil
		}
		return map[string]any{
			"decision": "deny", "reason": result.AgentMessage, "systemMessage": result.UserMessage,
		}, nil
	default:
		return nil, errUnknown("Unknown hook tool " + tool)
	}
}

// FailedClosed denies the action when the hook itself breaks.
func FailedClosed(tool, message string) map[string]any {
	result := model.GateResult{
		Allow:        false,
		UserMessage:  "Lintel failed closed: " + message,
		AgentMessage: "Lintel failed closed: " + message + " Do not work around this block. Fix the gate, then propose the choice again.",
	}
	body, err := Render(tool, result)
	if err != nil {
		return map[string]any{"permission": "deny", "agent_message": result.AgentMessage}
	}
	return body
}

// LoadsPayload parses a hook body.
func LoadsPayload(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		raw = "{}"
	}
	var data any
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return nil, err
	}
	object, ok := data.(map[string]any)
	if !ok {
		return nil, errUnknown("Hook input must be a JSON object.")
	}
	return object, nil
}

func writeAction(toolInput map[string]any) model.Action {
	if edits, ok := toolInput["edits"].([]any); ok && len(edits) > 0 {
		var paths []string
		contents := map[string]string{}
		for _, edit := range edits {
			object, ok := edit.(map[string]any)
			if !ok {
				continue
			}
			merged := map[string]any{}
			for key, value := range toolInput {
				merged[key] = value
			}
			for key, value := range object {
				merged[key] = value
			}
			path, content, has := pathAndContent(merged)
			if path != "" {
				paths = append(paths, path)
				if has {
					contents[path] = content
				}
			}
		}
		return model.Action{Kind: "write", Paths: paths, Contents: contents}
	}
	path, content, has := pathAndContent(toolInput)
	contents := map[string]string{}
	if path != "" && has {
		contents[path] = content
	}
	var paths []string
	if path != "" {
		paths = []string{path}
	}
	return model.Action{Kind: "write", Paths: paths, Contents: contents}
}

func pathAndContent(toolInput map[string]any) (string, string, bool) {
	path := ""
	for _, key := range pathKeys {
		if text := str(toolInput[key]); text != "" {
			path = text
			break
		}
	}
	for _, key := range contentKeys {
		if _, ok := toolInput[key]; ok && toolInput[key] != nil {
			return path, str(toolInput[key]), true
		}
	}
	return path, "", false
}

func looksLikePatch(toolInput map[string]any) bool {
	command, ok := toolInput["command"].(string)
	return ok && strings.Contains(command, "*** ") && strings.Contains(command, "File:")
}

func str(value any) string {
	text, _ := value.(string)
	return text
}

type hookError string

func (e hookError) Error() string { return string(e) }

func errUnknown(message string) error { return hookError(message) }

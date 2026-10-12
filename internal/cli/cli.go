// Package cli is the lintel command line.
package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"lintel/internal/advise"
	"lintel/internal/gate"
	"lintel/internal/hooks"
	"lintel/internal/initcmd"
	"lintel/internal/model"
	"lintel/internal/prompt"
	"lintel/internal/review"
	"lintel/internal/scan"
	"lintel/internal/secret"
	"lintel/internal/store"
)

// Option overrides process defaults for tests.
type Option struct {
	In     io.Reader
	Out    io.Writer
	Err    io.Writer
	Review store.Reviewer
	Ask    func(string) (string, string, bool)
	// Window opens the Jev decision window for a reviewed decision.
	Window func(project store.Project, id string) error
	Notify func(title, body string)
	Sleep  func(time.Duration)
	Advise review.Transport
}

// Execute runs one lintel invocation.
func Execute(args []string, opt Option) int {
	if opt.In == nil {
		opt.In = os.Stdin
	}
	if opt.Out == nil {
		opt.Out = os.Stdout
	}
	if opt.Err == nil {
		opt.Err = os.Stderr
	}
	if opt.Review == nil {
		opt.Review = func(decision store.Decision, config map[string]string, decisions []store.Decision) model.ReviewResult {
			return review.Proposal(decision, config, decisions, nil)
		}
	}
	if opt.Ask == nil {
		opt.Ask = func(card string) (string, string, bool) {
			return prompt.Ask(card, nil)
		}
	}
	if opt.Window == nil {
		opt.Window = launchWindow
	}
	if opt.Notify == nil {
		opt.Notify = prompt.Notify
	}
	if opt.Sleep == nil {
		opt.Sleep = time.Sleep
	}
	if len(args) == 0 {
		fmt.Fprintln(opt.Err, "usage: lintel <init|login|logout|ask|propose|show|status|approve|reject|review|wait|check|hook>")
		return 1
	}
	var code int
	var err error
	switch args[0] {
	case "init":
		code, err = cmdInit(args[1:], opt)
	case "login":
		code, err = cmdLogin(opt)
	case "logout":
		code, err = cmdLogout(opt)
	case "propose":
		code, err = cmdPropose(args[1:], opt)
	case "show":
		code, err = cmdShow(args[1:], opt)
	case "status":
		code, err = cmdStatus(opt)
	case "approve":
		code, err = cmdApprove(args[1:], opt)
	case "reject":
		code, err = cmdReject(args[1:], opt)
	case "review":
		code, err = cmdReview(args[1:], opt)
	case "wait":
		code, err = cmdWait(args[1:], opt)
	case "window":
		code, err = cmdWindow(args[1:], opt)
	case "ask":
		code, err = cmdAsk(args[1:], opt)
	case "check":
		code, err = cmdCheck(args[1:], opt)
	case "hook":
		return cmdHook(args[1:], opt)
	default:
		fmt.Fprintf(opt.Err, "unknown command %s\n", args[0])
		return 1
	}
	if err != nil {
		fmt.Fprintln(opt.Err, err.Error())
		return 1
	}
	return code
}

func cmdInit(args []string, opt Option) (int, error) {
	root, _ := os.Getwd()
	flags := parse(args)
	if value := flags.first("root"); value != "" {
		root = value
	}
	notes, err := initcmd.Project(root)
	if err != nil {
		return 1, err
	}
	for _, note := range notes {
		fmt.Fprintln(opt.Out, note)
	}
	return 0, nil
}

func cmdLogin(opt Option) (int, error) {
	fmt.Fprint(opt.Err, "TypeSafe API key: ")
	key, err := readKey(opt.In, opt.Err)
	if err != nil {
		return 1, err
	}
	if err := saveSecret(key); err != nil {
		return 1, err
	}
	fmt.Fprintln(opt.Out, "Saved the TypeSafe API key in the system keyring.")
	return 0, nil
}

func cmdLogout(opt Option) (int, error) {
	if err := removeSecret(); err != nil {
		return 1, err
	}
	fmt.Fprintln(opt.Out, "Removed the TypeSafe API key from the system keyring.")
	return 0, nil
}

var (
	saveSecret   = secret.Save
	removeSecret = secret.Remove
)

func readKey(in io.Reader, errOut io.Writer) (string, error) {
	if file, ok := in.(*os.File); ok && isTTY(file) {
		key, err := readHidden(file)
		fmt.Fprintln(errOut)
		return key, err
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func cmdPropose(args []string, opt Option) (int, error) {
	project, err := store.Discover("")
	if err != nil {
		return 1, err
	}
	fields, err := proposalFields(args, opt)
	if err != nil {
		return 1, err
	}
	decision, err := project.Propose(fields, opt.Review)
	if err != nil {
		return 1, err
	}
	fmt.Fprintln(opt.Out, store.FormatCard(decision))
	return 0, nil
}

func cmdShow(args []string, opt Option) (int, error) {
	project, err := store.Discover("")
	if err != nil {
		return 1, err
	}
	id, err := project.ResolveID(positional(args))
	if err != nil {
		return 1, err
	}
	decision, err := project.Load(id)
	if err != nil {
		return 1, err
	}
	fmt.Fprintln(opt.Out, store.FormatCard(decision))
	return 0, nil
}

func cmdStatus(opt Option) (int, error) {
	project, err := store.Discover("")
	if err != nil {
		return 1, err
	}
	text, err := store.FormatStatus(project)
	if err != nil {
		return 1, err
	}
	fmt.Fprintln(opt.Out, text)
	return 0, nil
}

func cmdApprove(args []string, opt Option) (int, error) {
	project, err := store.Discover("")
	if err != nil {
		return 1, err
	}
	flags := parse(args)
	id, err := project.ResolveID(flags.position)
	if err != nil {
		return 1, err
	}
	decision, err := project.Approve(id, flags.first("note"))
	if err != nil {
		return 1, err
	}
	fmt.Fprintln(opt.Out, store.FormatCard(decision))
	return 0, nil
}

func cmdReject(args []string, opt Option) (int, error) {
	project, err := store.Discover("")
	if err != nil {
		return 1, err
	}
	flags := parse(args)
	id, err := project.ResolveID(flags.position)
	if err != nil {
		return 1, err
	}
	decision, err := project.Reject(id, flags.first("note"))
	if err != nil {
		return 1, err
	}
	fmt.Fprintln(opt.Out, store.FormatCard(decision))
	return 0, nil
}

func cmdReview(args []string, opt Option) (int, error) {
	project, err := store.Discover("")
	if err != nil {
		return 1, err
	}
	id, err := project.ResolveID(positional(args))
	if err != nil {
		return 1, err
	}
	decision, err := project.Review(id, opt.Review)
	if err != nil {
		return 1, err
	}
	fmt.Fprintln(opt.Out, store.FormatCard(decision))
	return 0, nil
}

func cmdWait(args []string, opt Option) (int, error) {
	project, err := store.Discover("")
	if err != nil {
		return 1, err
	}
	flags := parse(args)
	id, err := project.ResolveID(flags.position)
	if err != nil {
		return 1, err
	}
	current, err := project.Load(id)
	if err != nil {
		return 1, err
	}
	fmt.Fprintln(opt.Out, store.FormatCard(current))
	switch current.Status {
	case model.StatusProposed:
		fmt.Fprintf(opt.Out, "Jev has not reviewed %s yet: %s\nFix that, then run `lintel review %s`. There is nothing for the human to approve until then.\n",
			id, orDash(current.ReviewError), id)
		return 1, nil
	case model.StatusReviewed:
		timeout := 540 * time.Second
		if raw := flags.first("timeout"); raw != "" {
			if parsed, err := strconv.ParseFloat(raw, 64); err == nil {
				timeout = time.Duration(parsed * float64(time.Second))
			}
		}
		decision, done, err := waitForHuman(project, id, timeout, opt)
		if err != nil {
			return 1, err
		}
		if !done {
			fmt.Fprintf(opt.Out, "Still waiting for the human on %s. Run `lintel wait %s` again in this same turn. Do not approve it yourself.\n", id, id)
			return 0, nil
		}
		current = decision
		fmt.Fprintln(opt.Out, store.FormatCard(current))
	}
	switch current.Status {
	case model.StatusRejected:
		fmt.Fprintln(opt.Out, "Human rejected this. Stop. Do not propose a replacement unless the human asks for one.")
	case model.StatusOverridden:
		fmt.Fprintln(opt.Out, "Human override. Jev blocked this. Implement only the authorized scope.")
	case model.StatusAuthorized, model.StatusApproved:
		fmt.Fprintln(opt.Out, "Human authorized this. Implement only the authorized scope.")
	default:
		fmt.Fprintf(opt.Out, "Decision %s is %s.\n", id, current.Status)
	}
	return 0, nil
}

// waitForHuman opens the Jev window and polls the decision until the human
// decides or timeout passes. It prints a line every 20 seconds so harnesses
// that stop silent commands keep this one alive, and it reopens the window
// when the human closed it with Not yet.
func waitForHuman(project store.Project, id string, timeout time.Duration, opt Option) (store.Decision, bool, error) {
	const (
		heartbeat = 20 * time.Second
		reopen    = 3 * time.Minute
		interval  = 500 * time.Millisecond
	)
	open := func() {
		if err := opt.Window(project, id); err != nil {
			opt.Notify("Jev decision needs you", "Decision "+id+": approve with `lintel approve "+id+"` in a terminal.")
			fmt.Fprintf(opt.Out, "No Jev window could be opened (%s). The human can approve with `lintel approve %s` or reject with `lintel reject %s --note \"...\"` in a terminal. This command keeps waiting.\n", err.Error(), id, id)
			return
		}
		opt.Notify("Jev decision needs you", "Decision "+id+" is waiting for Approve or Reject.")
		fmt.Fprintln(opt.Out, "A window titled Jev decision is open. Click Approve or Reject. Not yet closes it; it opens again in a few minutes. Do not run lintel approve yourself.")
	}
	started := time.Now()
	opened, beat := started, started
	open()
	for {
		decision, err := project.Load(id)
		if err != nil {
			return store.Decision{}, false, err
		}
		if model.IsGateOpen(decision.Status) || decision.Status == model.StatusRejected {
			return decision, true, nil
		}
		if !model.OpenStatuses[decision.Status] {
			return store.Decision{}, false, fmt.Errorf("Decision %s is %s and cannot be waited on.", id, decision.Status)
		}
		if time.Since(started) >= timeout {
			return decision, false, nil
		}
		if time.Since(opened) >= reopen && !windowOpen(project, id) {
			opened = time.Now()
			open()
		}
		if time.Since(beat) >= heartbeat {
			beat = time.Now()
			fmt.Fprintf(opt.Out, "Still waiting for the human on %s (%s).\n", id, time.Since(started).Round(time.Second))
		}
		opt.Sleep(interval)
	}
}

func windowFile(project store.Project, id string) string {
	return filepath.Join(project.Dir, "window-"+id+".pid")
}

func windowOpen(project store.Project, id string) bool {
	info, err := os.Stat(windowFile(project, id))
	if err != nil || time.Since(info.ModTime()) > 30*time.Minute {
		return false
	}
	raw, err := os.ReadFile(windowFile(project, id))
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	return err == nil && prompt.Alive(pid)
}

func launchWindow(project store.Project, id string) error {
	if windowOpen(project, id) {
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return prompt.Launch(exe, project.Root, id)
}

// cmdWindow is the Jev decision window. lintel wait starts it detached; only a
// click on Approve or Reject records a decision.
func cmdWindow(args []string, opt Option) (int, error) {
	project, err := store.Discover("")
	if err != nil {
		return 1, err
	}
	id, err := project.ResolveID(positional(args))
	if err != nil {
		return 1, err
	}
	current, err := project.Load(id)
	if err != nil || current.Status != model.StatusReviewed {
		return 0, err
	}
	pidFile := windowFile(project, id)
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0o644); err == nil {
		defer os.Remove(pidFile)
	}
	choice, note, ok := opt.Ask(store.FormatPrompt(current))
	if !ok {
		return 0, nil
	}
	current, err = project.Load(id)
	if err != nil || current.Status != model.StatusReviewed {
		return 0, err
	}
	if choice == "approve" {
		_, err = project.Approve(id, note)
	} else {
		_, err = project.Reject(id, note)
	}
	if err != nil {
		return 1, err
	}
	return 0, nil
}

func cmdAsk(args []string, opt Option) (int, error) {
	project, err := store.Discover("")
	if err != nil {
		return 1, err
	}
	flags := parse(args)
	options, err := advise.ParseOptions(flags.list("option"))
	if err != nil {
		return 1, err
	}
	advice, err := advise.Ask(project, flags.first("question"), flags.first("context"), options, flags.list("path"), opt.Advise)
	if err != nil {
		return 1, err
	}
	fmt.Fprintln(opt.Out, advise.Format(advice))
	return 0, nil
}

func orDash(text string) string {
	if strings.TrimSpace(text) == "" {
		return "-"
	}
	return text
}

func cmdCheck(args []string, opt Option) (int, error) {
	project, err := store.Discover("")
	if err != nil {
		return 1, err
	}
	flags := parse(args)
	actionKind := flags.first("action")
	if actionKind != "read" && actionKind != "write" && actionKind != "shell" {
		return 1, fmt.Errorf("--action is required")
	}
	contents := map[string]string{}
	if content, ok := flags.values["content"]; ok && len(flags.list("path")) == 1 {
		contents[flags.list("path")[0]] = content[len(content)-1]
	}
	action := model.Action{Kind: actionKind, Paths: flags.list("path"), Contents: contents, Command: flags.first("command")}
	open, _ := singleOpen(project)
	all, err := project.All()
	if err != nil {
		return 1, err
	}
	result := gate.Evaluate(project.Root, action, all, open)
	encoded, err := json.Marshal(map[string]any{
		"allow": result.Allow, "user_message": result.UserMessage, "agent_message": result.AgentMessage,
	})
	if err != nil {
		return 1, err
	}
	fmt.Fprintln(opt.Out, string(encoded))
	if result.Allow {
		return 0, nil
	}
	return 1, nil
}

func cmdHook(args []string, opt Option) int {
	if len(args) != 1 || !knownTool(args[0]) {
		fmt.Fprintln(opt.Err, "usage: lintel hook <cursor|claude|codex|gemini>")
		return 1
	}
	tool := args[0]
	raw, err := io.ReadAll(opt.In)
	if err != nil {
		fmt.Fprintln(opt.Out, mustJSON(hooks.FailedClosed(tool, err.Error())))
		return 0
	}
	payload, err := hooks.LoadsPayload(string(raw))
	if err != nil {
		fmt.Fprintln(opt.Out, mustJSON(hooks.FailedClosed(tool, err.Error())))
		return 0
	}
	project, err := store.Discover("")
	if event := hooks.Event(payload); event != hooks.EventTool {
		body := map[string]any{}
		if err == nil {
			body = lifecycle(tool, event, payload, project, opt)
		}
		fmt.Fprintln(opt.Out, mustJSON(body))
		return 0
	}
	if err != nil {
		fmt.Fprintln(opt.Out, mustJSON(hooks.FailedClosed(tool, err.Error())))
		return 0
	}
	action := hooks.ActionFromPayload(payload)
	if action.Kind == "shell" {
		if body := approval(tool, hooks.RawEvent(payload), action.Command, project); body != nil {
			fmt.Fprintln(opt.Out, mustJSON(body))
			return 0
		}
	}
	open, _ := singleOpen(project)
	all, err := project.All()
	if err != nil {
		fmt.Fprintln(opt.Out, mustJSON(hooks.FailedClosed(tool, err.Error())))
		return 0
	}
	result := gate.Evaluate(project.Root, action, all, open)
	body, err := hooks.Render(tool, result)
	if err != nil {
		body = hooks.FailedClosed(tool, err.Error())
	}
	fmt.Fprintln(opt.Out, mustJSON(body))
	return 0
}

// approval routes lintel wait and lintel approve. Where the harness can ask,
// its own prompt is the human's decision, so the human never has to leave it.
// Codex cannot ask, so there lintel wait opens the Jev window and the agent
// may not run lintel approve at all. It returns nil for other commands.
func approval(tool, event, command string, project store.Project) map[string]any {
	approve := scan.MentionsApprove(command)
	if !approve && !scan.RunsLintel(command, "wait") {
		return nil
	}
	reviewed := reviewedDecision(project)
	if approve {
		switch {
		case !hooks.CanAsk(tool):
			return hooks.Denied(tool, "Only the human approves a decision. Run `lintel wait` in this same turn; the human decides in the Jev window.")
		case tool == "cursor" && event == "preToolUse":
			return hooks.Allowed(tool)
		case reviewed == nil:
			return hooks.Denied(tool, "No reviewed decision is waiting for approval. Run `lintel status`.")
		case tool == "cursor" && !exactApprove(command, reviewed.ID):
			return hooks.Denied(tool, "Run exactly `lintel approve "+reviewed.ID+"` so Cursor can ask the human.")
		}
		body, err := hooks.Ask(tool, *reviewed)
		if err != nil {
			return nil
		}
		return body
	}
	if !hooks.CanAsk(tool) {
		return nil
	}
	// Letting wait run here would open the Jev window instead of the
	// harness's own prompt, so wait only runs as the harness prompt.
	if reviewed == nil {
		return hooks.Denied(tool, "No reviewed decision is waiting yet, so "+toolName(tool)+" cannot ask the human. Run `lintel propose` on its own first, then run `lintel wait` as a separate command. If the proposal failed review, run `lintel status`.")
	}
	if tool == "cursor" {
		return hooks.Denied(tool, "In Cursor the human decides in Cursor's own prompt. Run `lintel approve "+reviewed.ID+"` now; Cursor asks the human before it runs.")
	}
	body, err := hooks.Ask(tool, *reviewed)
	if err != nil {
		return nil
	}
	return body
}

func toolName(tool string) string {
	return map[string]string{"claude": "Claude Code", "gemini": "Gemini CLI", "cursor": "Cursor"}[tool]
}

func exactApprove(command, id string) bool {
	segments := scan.Parse(command)
	if len(segments) != 1 || !scan.IsLintel(segments[0]) || len(segments[0].Writes) > 0 {
		return false
	}
	argv := segments[0].Argv()
	return (len(argv) == 2 && argv[1] == "approve") || (len(argv) == 3 && argv[1] == "approve" && argv[2] == id)
}

// lifecycle answers session, stop, and notification hooks. They never block a
// tool; they keep the agent and the human aware of a waiting decision.
func lifecycle(tool, event string, payload map[string]any, project store.Project, opt Option) map[string]any {
	switch event {
	case hooks.EventSession:
		status, err := store.FormatStatus(project)
		if err != nil {
			status = ""
		}
		return hooks.Session(tool, status)
	case hooks.EventStop:
		reviewed := reviewedDecision(project)
		active, _ := payload["stop_hook_active"].(bool)
		loops, _ := payload["loop_count"].(float64)
		repeat := active || loops > 0
		if reviewed != nil && repeat {
			opt.Notify("Jev decision needs you", "Decision "+reviewed.ID+" is still waiting for Approve or Reject.")
		}
		return hooks.Stop(tool, reviewed, repeat)
	case hooks.EventNotify:
		message, _ := payload["message"].(string)
		if message == "" {
			message = "Your coding agent is waiting for you."
		}
		title := "Input needed"
		if reviewed := reviewedDecision(project); reviewed != nil {
			title = "Jev decision " + reviewed.ID
		}
		opt.Notify(title, message)
	}
	return map[string]any{}
}

func proposalFields(args []string, opt Option) (map[string]any, error) {
	flags := parse(args)
	if id := flags.first("from-advice"); id != "" {
		if err := fromAdvice(flags, id); err != nil {
			return nil, err
		}
	}
	stdin := flags.has("stdin")
	if stdin || (flags.first("title") == "" && !isTTY(opt.In)) {
		raw, err := io.ReadAll(opt.In)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(string(raw)) == "" {
			raw = []byte("{}")
		}
		var data any
		if err := json.Unmarshal(raw, &data); err != nil {
			return nil, fmt.Errorf("Invalid JSON: %s", err.Error())
		}
		object, ok := data.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Proposal JSON must be an object.")
		}
		return object, nil
	}
	choice := flags.first("choice")
	change := flags.first("change")
	if choice == "" {
		choice = change
	}
	if change == "" {
		change = choice
	}
	return map[string]any{
		"kind": flags.first("kind"), "title": flags.first("title"), "question": flags.first("question"),
		"context": flags.first("context"), "choice": choice, "intent": flags.first("intent"),
		"proposed_change": change, "alternatives": flags.list("alternative"), "why": flags.first("why"),
		"rejected_because": flags.first("rejected-because"), "risks": flags.list("risk"),
		"paths": flags.list("path"), "packages": flags.list("package"), "components": flags.list("component"),
		"invariants": flags.list("invariant"), "implementation": flags.list("implementation"),
		"constraints": flags.list("constraint"), "supersedes": flags.list("supersedes"),
		"acceptance": flags.list("acceptance"),
	}, nil
}

// fromAdvice fills choice, alternatives, question, and context from Jev's
// advice when the agent did not give them.
func fromAdvice(flags flagSet, id string) error {
	project, err := store.Discover("")
	if err != nil {
		return err
	}
	advice, err := advise.Load(project, id)
	if err != nil {
		return err
	}
	chosen := advice.Option(advice.Recommendation)
	if flags.first("choice") == "" {
		flags.values["choice"] = []string{chosen.ID + ": " + chosen.Description}
	}
	if len(flags.list("alternative")) == 0 {
		for _, option := range advice.Options {
			if option.ID != chosen.ID {
				flags.values["alternative"] = append(flags.values["alternative"], option.ID+": "+option.Description)
			}
		}
	}
	if flags.first("question") == "" {
		flags.values["question"] = []string{advice.Question}
	}
	if flags.first("title") == "" {
		flags.values["title"] = []string{advice.Question}
	}
	note := fmt.Sprintf("Jev advice %s recommended %s.", advice.ID, chosen.ID)
	if context := flags.first("context"); context != "" {
		note = context + " " + note
	} else if advice.Context != "" {
		note = advice.Context + " " + note
	}
	flags.values["context"] = []string{note}
	return nil
}

func reviewedDecision(project store.Project) *store.Decision {
	open, err := project.OpenDecisions()
	if err != nil {
		return nil
	}
	var found *store.Decision
	for _, item := range open {
		if item.Status == model.StatusReviewed && item.Jev != nil {
			if found != nil {
				return nil
			}
			copy := item
			found = &copy
		}
	}
	return found
}

func singleOpen(project store.Project) (*store.Decision, error) {
	open, err := project.OpenDecisions()
	if err != nil || len(open) != 1 {
		return nil, err
	}
	return &open[0], nil
}

func knownTool(tool string) bool {
	for _, name := range initcmd.Tools {
		if name == tool {
			return true
		}
	}
	return false
}

func mustJSON(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func isTTY(reader io.Reader) bool {
	file, ok := reader.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func positional(args []string) string {
	return parse(args).position
}

type flagSet struct {
	values   map[string][]string
	position string
}

func (f flagSet) first(name string) string {
	values := f.values[name]
	if len(values) == 0 {
		return ""
	}
	return values[len(values)-1]
}

func (f flagSet) list(name string) []string {
	return f.values[name]
}

func (f flagSet) has(name string) bool {
	_, ok := f.values[name]
	return ok
}

func parse(args []string) flagSet {
	set := flagSet{values: map[string][]string{}}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			continue
		}
		if !strings.HasPrefix(arg, "--") {
			set.position = arg
			continue
		}
		name := strings.TrimPrefix(arg, "--")
		value := ""
		if left, right, ok := strings.Cut(name, "="); ok {
			name, value = left, right
		} else if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			i++
			value = args[i]
		}
		set.values[name] = append(set.values[name], value)
	}
	return set
}

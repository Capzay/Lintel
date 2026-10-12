// Package store loads, saves, and moves decision records through the gate.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"lintel/internal/model"
)

const defaultModel = "jev-latest"

var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

// Error is a user-facing gate error.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

func fail(msg string) error { return &Error{Msg: msg} }

// Decision is one proposal and its review.
type Decision struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	Title        string   `json:"title"`
	Status       string   `json:"status"`
	Question     string   `json:"question"`
	Context      string   `json:"context"`
	Proposal     Proposal `json:"proposal"`
	Supersedes   []string `json:"supersedes"`
	SupersededBy string   `json:"superseded_by"`
	Jev          *Jev     `json:"jev"`
	Human        *Human   `json:"human"`
	ReviewError  string   `json:"review_error"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
}

// Proposal is the choice the agent wants reviewed.
type Proposal struct {
	Choice                  string       `json:"choice"`
	Intent                  string       `json:"intent"`
	ProposedChange          string       `json:"proposed_change"`
	Alternatives            []string     `json:"alternatives"`
	Why                     string       `json:"why"`
	RejectedBecause         string       `json:"rejected_because"`
	Risks                   []string     `json:"risks"`
	Invariants              []Named      `json:"invariants"`
	ImplementationDecisions []Named      `json:"implementation_decisions"`
	Constraints             []Constraint `json:"constraints"`
	Supersedes              []string     `json:"supersedes"`
	AcceptanceCriteria      []string     `json:"acceptance_criteria"`
	Scope                   Scope        `json:"scope"`
}

// Scope is the paths, packages, and components a decision covers.
type Scope struct {
	Paths      []string `json:"paths"`
	Packages   []string `json:"packages"`
	Components []string `json:"components"`
}

// Named is an invariant or implementation decision.
type Named struct {
	ID        string `json:"id"`
	Statement string `json:"statement"`
}

// Constraint is a temporary requirement.
type Constraint struct {
	ID        string `json:"id"`
	Statement string `json:"statement"`
	Until     string `json:"until,omitempty"`
}

// Jev is the reviewer's stored verdict.
type Jev struct {
	Verdict            string         `json:"verdict"`
	Code               string         `json:"code"`
	Summary            string         `json:"summary"`
	Concerns           []string       `json:"concerns"`
	AffectedInvariant  string         `json:"affected_invariant"`
	Conflict           string         `json:"conflict"`
	RequiredChange     string         `json:"required_change"`
	AcceptanceCriteria string         `json:"acceptance_criteria"`
	Evidence           string         `json:"evidence"`
	Probabilities      map[string]any `json:"probabilities"`
}

// Human is the person's decision.
type Human struct {
	Decision      string `json:"decision"`
	Authorization string `json:"authorization,omitempty"`
	Note          string `json:"note"`
	At            string `json:"at"`
}

// Reviewer judges a proposal. The store does not grade its own decision.
type Reviewer func(decision Decision, config map[string]string, decisions []Decision) model.ReviewResult

// Project is the .lintel directory for one repository.
type Project struct {
	Root         string
	Dir          string
	DecisionsDir string
}

// Discover finds the project from LINTEL_PROJECT or a parent .lintel directory.
func Discover(start string) (Project, error) {
	if override := os.Getenv("LINTEL_PROJECT"); override != "" {
		root, err := filepath.Abs(override)
		if err != nil {
			return Project{}, fail(fmt.Sprintf("No .lintel directory in LINTEL_PROJECT=%s. Run lintel init.", override))
		}
		if st, err := os.Stat(filepath.Join(root, ".lintel")); err != nil || !st.IsDir() {
			return Project{}, fail(fmt.Sprintf("No .lintel directory in LINTEL_PROJECT=%s. Run lintel init.", root))
		}
		return bind(root), nil
	}
	if start == "" {
		var err error
		start, err = os.Getwd()
		if err != nil {
			return Project{}, fail("No .lintel directory found. Run lintel init in the project.")
		}
	}
	current, err := filepath.Abs(start)
	if err != nil {
		return Project{}, fail("No .lintel directory found. Run lintel init in the project.")
	}
	for {
		if st, err := os.Stat(filepath.Join(current, ".lintel")); err == nil && st.IsDir() {
			return bind(current), nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return Project{}, fail("No .lintel directory found. Run lintel init in the project.")
}

func bind(root string) Project {
	dir := filepath.Join(root, ".lintel")
	return Project{Root: root, Dir: dir, DecisionsDir: filepath.Join(dir, "decisions")}
}

// Open binds a project that already has .lintel.
func Open(root string) Project { return bind(root) }

// Config reads the reviewer model.
func (p Project) Config() map[string]string {
	modelName := defaultModel
	raw, err := os.ReadFile(filepath.Join(p.Dir, "config.toml"))
	if err == nil {
		section := ""
		for _, line := range strings.Split(string(raw), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") || trimmed == "" {
				continue
			}
			if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
				section = strings.TrimSpace(trimmed[1 : len(trimmed)-1])
				continue
			}
			if section == "reviewer" {
				key, value, ok := strings.Cut(trimmed, "=")
				if ok && strings.TrimSpace(key) == "model" {
					modelName = strings.Trim(strings.TrimSpace(value), `"'`)
				}
			}
		}
	}
	if modelName == "" {
		modelName = defaultModel
	}
	return map[string]string{"model": modelName}
}

// All returns every decision record.
func (p Project) All() ([]Decision, error) {
	entries, err := os.ReadDir(p.DecisionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var records []Decision
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		decision, err := p.Load(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			return nil, err
		}
		records = append(records, decision)
	}
	return records, nil
}

// OpenDecisions returns proposals that are not yet authorized or rejected.
func (p Project) OpenDecisions() ([]Decision, error) {
	all, err := p.All()
	if err != nil {
		return nil, err
	}
	var open []Decision
	for _, item := range all {
		if model.OpenStatuses[item.Status] {
			open = append(open, item)
		}
	}
	return open, nil
}

// Approved returns decisions that open the gate.
func (p Project) Approved() ([]Decision, error) {
	all, err := p.All()
	if err != nil {
		return nil, err
	}
	var approved []Decision
	for _, item := range all {
		if model.IsGateOpen(item.Status) {
			approved = append(approved, item)
		}
	}
	return approved, nil
}

// Load reads one decision.
func (p Project) Load(id string) (Decision, error) {
	raw, err := os.ReadFile(filepath.Join(p.DecisionsDir, id+".json"))
	if err != nil {
		return Decision{}, fail("No decision named " + id + ".")
	}
	var decision Decision
	if err := json.Unmarshal(raw, &decision); err != nil {
		return Decision{}, fail("No decision named " + id + ".")
	}
	return decision, nil
}

// Save writes one decision atomically.
func (p Project) Save(decision Decision) error {
	if err := os.MkdirAll(p.DecisionsDir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(decision, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	path := filepath.Join(p.DecisionsDir, decision.ID+".json")
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

// Propose records a choice and reviews it.
func (p Project) Propose(fields map[string]any, reviewer Reviewer) (Decision, error) {
	cleaned, err := NormalizeFields(fields)
	if err != nil {
		return Decision{}, err
	}
	all, err := p.All()
	if err != nil {
		return Decision{}, err
	}
	if cleaned.Kind == model.KindChange && len(approvedKickoffs(all)) == 0 {
		return Decision{}, fail("Approve a kickoff decision before proposing a change.")
	}
	open, err := p.OpenDecisions()
	if err != nil {
		return Decision{}, err
	}
	if len(open) > 1 {
		return Decision{}, fail("More than one decision is open. Approve or reject one first.")
	}
	var decision Decision
	if len(open) == 1 {
		current := open[0]
		verdict := ""
		if current.Jev != nil {
			verdict = current.Jev.Verdict
		}
		if current.Status == model.StatusReviewed && verdict == model.VerdictAccept {
			return Decision{}, fail(fmt.Sprintf("Decision %s is waiting for human review. Approve it or reject it before proposing another choice.", current.ID))
		}
		if current.Status == model.StatusReviewed && verdict != model.VerdictRevise && verdict != model.VerdictBlock {
			return Decision{}, fail(fmt.Sprintf("Decision %s is still open.", current.ID))
		}
		decision = applyFields(current, cleaned)
		decision.Status = model.StatusProposed
		decision.Human = nil
		decision.Jev = nil
		decision.UpdatedAt = utcNow()
	} else {
		decision = blank(p.DecisionsDir, cleaned)
	}
	if err := p.Save(decision); err != nil {
		return Decision{}, err
	}
	return p.Review(decision.ID, reviewer)
}

// Review asks the reviewer to judge an open decision.
func (p Project) Review(id string, reviewer Reviewer) (Decision, error) {
	decision, err := p.Load(id)
	if err != nil {
		return Decision{}, err
	}
	if !model.OpenStatuses[decision.Status] {
		return Decision{}, fail(fmt.Sprintf("Decision %s is %s and cannot be reviewed.", id, decision.Status))
	}
	all, err := p.All()
	if err != nil {
		return Decision{}, err
	}
	result := reviewer(decision, p.Config(), all)
	if result.Verdict != model.VerdictAccept && result.Verdict != model.VerdictRevise && result.Verdict != model.VerdictBlock {
		return Decision{}, fail("Reviewer returned unknown verdict " + result.Verdict + ".")
	}
	if !result.Valid || !reviewIsComplete(result) {
		decision.Jev = nil
		decision.Status = model.StatusProposed
		decision.ReviewError = result.Summary
		if decision.ReviewError == "" {
			decision.ReviewError = "The review did not name a complete conflict."
		}
		decision.UpdatedAt = utcNow()
		_ = p.Save(decision)
		return Decision{}, fail(decision.ReviewError)
	}
	code := result.Code
	if code == "" && result.Verdict == model.VerdictBlock {
		code = "JEV_BLOCK"
	}
	decision.ReviewError = ""
	decision.Jev = &Jev{
		Verdict:            result.Verdict,
		Code:               code,
		Summary:            result.Summary,
		Concerns:           nonNil(result.Concerns),
		AffectedInvariant:  result.AffectedInvariant,
		Conflict:           result.Conflict,
		RequiredChange:     result.RequiredChange,
		AcceptanceCriteria: result.AcceptanceCriteria,
		Evidence:           result.Evidence,
		Probabilities:      result.Probabilities,
	}
	if decision.Jev.Probabilities == nil {
		decision.Jev.Probabilities = map[string]any{}
	}
	decision.Status = model.StatusReviewed
	decision.UpdatedAt = utcNow()
	if err := p.Save(decision); err != nil {
		return Decision{}, err
	}
	return decision, nil
}

// Approve records a human yes. A yes after a block is HUMAN_OVERRIDE.
func (p Project) Approve(id, note string) (Decision, error) {
	decision, err := p.Load(id)
	if err != nil {
		return Decision{}, err
	}
	if decision.Status != model.StatusReviewed || decision.Jev == nil {
		return Decision{}, fail(fmt.Sprintf("Decision %s has not been reviewed yet.", id))
	}
	now := utcNow()
	verdict := decision.Jev.Verdict
	authorization := "authorized"
	if verdict == model.VerdictBlock {
		decision.Status = model.StatusOverridden
		authorization = "HUMAN_OVERRIDE"
		decision.Jev.Code = "JEV_BLOCK"
	} else {
		decision.Status = model.StatusAuthorized
	}
	if decision.Kind == model.KindKickoff {
		all, err := p.All()
		if err != nil {
			return Decision{}, err
		}
		for _, other := range approvedKickoffs(all) {
			if other.ID == decision.ID {
				continue
			}
			other.Status = model.StatusSuperseded
			other.SupersededBy = decision.ID
			other.UpdatedAt = now
			if err := p.Save(other); err != nil {
				return Decision{}, err
			}
		}
	}
	if err := p.applySupersession(&decision, now); err != nil {
		return Decision{}, err
	}
	decision.Human = &Human{Decision: authorization, Authorization: authorization, Note: note, At: now}
	decision.UpdatedAt = now
	if err := p.Save(decision); err != nil {
		return Decision{}, err
	}
	return decision, nil
}

// Reject records a human no.
func (p Project) Reject(id, note string) (Decision, error) {
	decision, err := p.Load(id)
	if err != nil {
		return Decision{}, err
	}
	if !model.OpenStatuses[decision.Status] {
		return Decision{}, fail(fmt.Sprintf("Decision %s is %s and cannot be rejected.", id, decision.Status))
	}
	decision.Status = model.StatusRejected
	decision.Human = &Human{Decision: "rejected", Note: note, At: utcNow()}
	decision.UpdatedAt = decision.Human.At
	if err := p.Save(decision); err != nil {
		return Decision{}, err
	}
	return decision, nil
}

// ResolveID picks the only open decision when id is empty.
func (p Project) ResolveID(id string) (string, error) {
	if id != "" {
		return id, nil
	}
	open, err := p.OpenDecisions()
	if err != nil {
		return "", err
	}
	if len(open) == 1 {
		return open[0].ID, nil
	}
	if len(open) == 0 {
		return "", fail("No open decision. Pass a decision id.")
	}
	return "", fail("More than one decision is open. Pass a decision id.")
}

// WaitForHuman blocks until a person authorizes or rejects. It never authorizes by itself.
func (p Project) WaitForHuman(id string, timeout time.Duration, interval time.Duration, sleeper func(time.Duration)) (Decision, error) {
	if sleeper == nil {
		sleeper = time.Sleep
	}
	started := time.Now()
	for {
		decision, err := p.Load(id)
		if err != nil {
			return Decision{}, err
		}
		if model.IsGateOpen(decision.Status) || decision.Status == model.StatusRejected {
			return decision, nil
		}
		if !model.OpenStatuses[decision.Status] {
			return Decision{}, fail(fmt.Sprintf("Decision %s is %s and cannot be waited on.", id, decision.Status))
		}
		if time.Since(started) >= timeout {
			return Decision{}, fail("Timed out waiting for a human decision. Run `lintel approve` or `lintel reject`, then `lintel wait` again.")
		}
		sleeper(interval)
	}
}

func (p Project) applySupersession(decision *Decision, now string) error {
	listed := append([]string{}, decision.Proposal.Supersedes...)
	decision.Supersedes = listed
	for _, oldID := range listed {
		if oldID == decision.ID {
			continue
		}
		older, err := p.Load(oldID)
		if err != nil {
			continue
		}
		older.Status = model.StatusSuperseded
		older.SupersededBy = decision.ID
		older.UpdatedAt = now
		if err := p.Save(older); err != nil {
			return err
		}
	}
	return nil
}

// NormalizeFields checks a proposal object.
func NormalizeFields(fields map[string]any) (ProposalFields, error) {
	kind := strings.TrimSpace(asString(fields["kind"]))
	if kind != model.KindKickoff && kind != model.KindChange {
		return ProposalFields{}, fail("kind must be kickoff or change.")
	}
	title := strings.TrimSpace(asString(fields["title"]))
	question := strings.TrimSpace(asString(fields["question"]))
	choice := strings.TrimSpace(asString(fields["choice"]))
	if title == "" || question == "" || choice == "" {
		return ProposalFields{}, fail("title, question, and choice are required.")
	}
	paths := stringList(first(fields["paths"], fields["path"]))
	if len(paths) == 0 {
		return ProposalFields{}, fail("At least one path scope is required.")
	}
	proposed := strings.TrimSpace(asString(fields["proposed_change"]))
	if proposed == "" {
		proposed = choice
	}
	intent := strings.TrimSpace(asString(fields["intent"]))
	if intent == "" {
		intent = strings.TrimSpace(asString(fields["question"]))
	}
	return ProposalFields{
		Kind: kind, Title: title, Question: question,
		Context: strings.TrimSpace(asString(fields["context"])),
		Choice:  choice, Intent: intent, ProposedChange: proposed,
		Alternatives:    stringList(first(fields["alternatives"], fields["alternative"])),
		Why:             strings.TrimSpace(asString(fields["why"])),
		RejectedBecause: strings.TrimSpace(asString(fields["rejected_because"])),
		Risks:           stringList(first(fields["risks"], fields["risk"])),
		Paths:           paths,
		Packages:        stringList(first(fields["packages"], fields["package"])),
		Components:      stringList(first(fields["components"], fields["component"])),
		Invariants:      namedItems(first(fields["invariants"], fields["invariant"])),
		Implementation:  namedItems(first(fields["implementation_decisions"], fields["implementation"])),
		Constraints:     constraints(first(fields["constraints"], fields["constraint"])),
		Supersedes:      stringList(fields["supersedes"]),
		Acceptance:      stringList(first(fields["acceptance_criteria"], fields["acceptance"])),
	}, nil
}

// ProposalFields is a cleaned proposal.
type ProposalFields struct {
	Kind, Title, Question, Context, Choice, Intent, ProposedChange, Why, RejectedBecause string
	Alternatives, Risks, Paths, Packages, Components, Supersedes, Acceptance             []string
	Invariants, Implementation                                                           []Named
	Constraints                                                                          []Constraint
}

// HumanAdvice is the plain warning for the person. Jev advises. The person decides.
func HumanAdvice(decision *Decision) string {
	if decision == nil || decision.Jev == nil {
		return ""
	}
	verdict := decision.Jev.Verdict
	choice := decision.Proposal.Choice
	if choice == "" {
		choice = "this"
	}
	if verdict == model.VerdictAccept {
		return fmt.Sprintf("Jev would do this: %s. Approve or reject it in the prompt your coding tool shows, in the Jev decision window, or with `lintel approve` or `lintel reject` in a terminal.", choice)
	}
	if verdict != model.VerdictRevise && verdict != model.VerdictBlock {
		return ""
	}
	text := fmt.Sprintf("Jev would not do this: %s.", choice)
	if decision.Jev.AffectedInvariant != "" {
		text += " Invariant: " + decision.Jev.AffectedInvariant + "."
	}
	if decision.Jev.Conflict != "" {
		text += " Conflict: " + decision.Jev.Conflict
	}
	if verdict == model.VerdictBlock {
		text += " A yes here is HUMAN_OVERRIDE. Jev's verdict stays JEV_BLOCK."
	}
	text += " Approve or reject it in the prompt your coding tool shows or in the Jev decision window. `lintel reject` stops this attempt."
	return text
}

// FormatCard renders a decision for the human.
func FormatCard(decision Decision) string {
	advice := HumanAdvice(&decision)
	var lines []string
	if advice != "" && model.OpenStatuses[decision.Status] {
		lines = append(lines, advice, "")
	}
	jev := "pending"
	evidence, summary, conflict, required, criteria := "-", "-", "-", "-", "-"
	if decision.Jev != nil {
		if decision.Jev.Code != "" {
			jev = decision.Jev.Code
		} else if decision.Jev.Verdict != "" {
			jev = decision.Jev.Verdict
		}
		evidence = orDash(decision.Jev.Evidence)
		summary = orDash(decision.Jev.Summary)
		conflict = orDash(decision.Jev.Conflict)
		required = orDash(decision.Jev.RequiredChange)
		criteria = orDash(decision.Jev.AcceptanceCriteria)
	}
	human := "pending"
	note := "-"
	if decision.Human != nil {
		if decision.Human.Authorization != "" {
			human = decision.Human.Authorization
		} else if decision.Human.Decision != "" {
			human = decision.Human.Decision
		}
		note = orDash(decision.Human.Note)
	}
	supersedes := decision.Supersedes
	if len(supersedes) == 0 {
		supersedes = decision.Proposal.Supersedes
	}
	lines = append(lines,
		"Decision  "+decision.ID,
		"Kind      "+decision.Kind,
		"Status    "+decision.Status,
		"Title     "+decision.Title,
		"Question  "+decision.Question,
	)
	// Optional fields appear only when they say something.
	for _, field := range [][2]string{
		{"Context   ", orDash(decision.Context)},
		{"Choice    ", orDash(decision.Proposal.Choice)},
		{"Alternatives  ", join(decision.Proposal.Alternatives)},
		{"Why       ", orDash(decision.Proposal.Why)},
		{"Rejected  ", orDash(decision.Proposal.RejectedBecause)},
		{"Risks     ", join(decision.Proposal.Risks)},
		{"Intent    ", orDash(decision.Proposal.Intent)},
		{"Change    ", orDash(changeText(decision))},
		{"Paths     ", join(decision.Proposal.Scope.Paths)},
		{"Components  ", join(decision.Proposal.Scope.Components)},
		{"Packages  ", join(decision.Proposal.Scope.Packages)},
		{"Invariants  ", join(statements(decision.Proposal.Invariants, nil))},
		{"Decisions   ", join(statements(decision.Proposal.ImplementationDecisions, nil))},
		{"Constraints  ", join(constraintStatements(decision.Proposal.Constraints))},
		{"Supersedes  ", join(supersedes)},
		{"Acceptance  ", join(decision.Proposal.AcceptanceCriteria)},
	} {
		if field[1] != "-" {
			lines = append(lines, field[0]+field[1])
		}
	}
	lines = append(lines, "Jev       "+jev)
	for _, field := range [][2]string{
		{"Evidence  ", evidence}, {"Summary   ", summary}, {"Conflict  ", conflict},
		{"Required  ", required}, {"Criteria  ", criteria},
	} {
		if field[1] != "-" && field[1] != "none" {
			lines = append(lines, field[0]+field[1])
		}
	}
	lines = append(lines, "Human     "+human)
	if note != "-" {
		lines = append(lines, "Note      "+note)
	}
	if decision.ReviewError != "" && decision.Jev == nil {
		lines = append(lines, "Error     "+decision.ReviewError)
	}
	return strings.Join(lines, "\n")
}

// changeText is the proposed change, unless it only repeats the choice.
func changeText(decision Decision) string {
	if decision.Proposal.ProposedChange == decision.Proposal.Choice {
		return ""
	}
	return decision.Proposal.ProposedChange
}

// FormatPrompt is the short card a human reads before approving: what is
// being decided, what Jev thinks in plain words, and only the fields that say
// something. `lintel show` prints the full card.
func FormatPrompt(decision Decision) string {
	var lines []string
	add := func(label, value string) {
		if strings.TrimSpace(value) != "" {
			lines = append(lines, label+value)
		}
	}
	title := decision.Title
	if title == "" {
		title = decision.Proposal.Choice
	}
	lines = append(lines, title, "")
	add("", decision.Question)
	add("Proposed: ", decision.Proposal.Choice)
	add("Why: ", decision.Proposal.Why)
	if len(decision.Proposal.Alternatives) > 0 {
		instead := strings.Join(decision.Proposal.Alternatives, "; ")
		if decision.Proposal.RejectedBecause != "" {
			instead += " (" + decision.Proposal.RejectedBecause + ")"
		}
		add("Instead of: ", instead)
	}
	add("Risks: ", strings.Join(decision.Proposal.Risks, "; "))
	add("Touches: ", strings.Join(decision.Proposal.Scope.Paths, ", "))
	add("New packages: ", strings.Join(decision.Proposal.Scope.Packages, ", "))
	add("Done when: ", strings.Join(decision.Proposal.AcceptanceCriteria, "; "))
	lines = append(lines, "")
	if decision.Jev != nil {
		switch decision.Jev.Verdict {
		case model.VerdictAccept:
			lines = append(lines, "Jev: would do this.")
		case model.VerdictRevise:
			lines = append(lines, "Jev: would change this first.")
		case model.VerdictBlock:
			lines = append(lines, "Jev: would NOT do this. Approving is a HUMAN_OVERRIDE.")
		}
		add("  Conflict: ", decision.Jev.Conflict)
		add("  Needed: ", decision.Jev.RequiredChange)
	} else {
		lines = append(lines, "Jev: not reviewed yet.")
	}
	lines = append(lines, "", "("+decision.Kind+" "+decision.ID+")")
	return strings.Join(lines, "\n")
}

// FormatStatus reports whether the gate is open.
func FormatStatus(p Project) (string, error) {
	approved, err := p.Approved()
	if err != nil {
		return "", err
	}
	kickoffs := approvedKickoffs(approved)
	open, err := p.OpenDecisions()
	if err != nil {
		return "", err
	}
	lines := []string{"Gate      " + map[bool]string{true: "open", false: "shut"}[len(kickoffs) > 0]}
	if len(kickoffs) > 0 {
		var ids []string
		var packages []string
		for _, item := range kickoffs {
			ids = append(ids, item.ID)
		}
		for _, item := range approved {
			packages = append(packages, item.Proposal.Scope.Packages...)
		}
		lines = append(lines, "Kickoff   "+strings.Join(ids, ", "))
		if len(packages) == 0 {
			lines = append(lines, "Packages  -")
		} else {
			lines = append(lines, "Packages  "+strings.Join(packages, ", "))
		}
	} else {
		lines = append(lines, "Kickoff   none")
	}
	if len(open) == 0 {
		lines = append(lines, "Open      none")
	} else {
		for _, item := range open {
			verdict := "pending"
			if item.Jev != nil && item.Jev.Verdict != "" {
				verdict = item.Jev.Verdict
			}
			lines = append(lines, fmt.Sprintf("Open      %s (%s, Jev %s)", item.ID, item.Status, verdict))
			if advice := HumanAdvice(&item); advice != "" {
				lines = append(lines, advice)
			}
		}
	}
	return strings.Join(lines, "\n"), nil
}

func approvedKickoffs(decisions []Decision) []Decision {
	var out []Decision
	for _, item := range decisions {
		if model.IsGateOpen(item.Status) && item.Kind == model.KindKickoff {
			out = append(out, item)
		}
	}
	return out
}

func reviewIsComplete(result model.ReviewResult) bool {
	if result.Verdict == model.VerdictAccept {
		return true
	}
	if result.Verdict == model.VerdictRevise && strings.TrimSpace(result.AffectedInvariant) == "" {
		return strings.TrimSpace(result.RequiredChange) != "" && strings.TrimSpace(result.AcceptanceCriteria) != ""
	}
	return strings.TrimSpace(result.AffectedInvariant) != "" &&
		strings.TrimSpace(result.Conflict) != "" &&
		strings.TrimSpace(result.RequiredChange) != "" &&
		strings.TrimSpace(result.AcceptanceCriteria) != ""
}

func blank(directory string, fields ProposalFields) Decision {
	now := utcNow()
	return Decision{
		ID: uniqueID(directory, fields.Title, now[:10]), Kind: fields.Kind, Title: fields.Title,
		Status: model.StatusProposed, Question: fields.Question, Context: fields.Context,
		Proposal: proposalOf(fields), Supersedes: append([]string{}, fields.Supersedes...),
		CreatedAt: now, UpdatedAt: now,
	}
}

func applyFields(decision Decision, fields ProposalFields) Decision {
	decision.Kind = fields.Kind
	decision.Title = fields.Title
	decision.Question = fields.Question
	decision.Context = fields.Context
	decision.Proposal = proposalOf(fields)
	decision.Supersedes = append([]string{}, fields.Supersedes...)
	return decision
}

func proposalOf(fields ProposalFields) Proposal {
	return Proposal{
		Choice: fields.Choice, Intent: fields.Intent, ProposedChange: fields.ProposedChange,
		Alternatives: nonNil(fields.Alternatives), Why: fields.Why, RejectedBecause: fields.RejectedBecause,
		Risks: nonNil(fields.Risks), Invariants: fields.Invariants, ImplementationDecisions: fields.Implementation,
		Constraints: fields.Constraints, Supersedes: nonNil(fields.Supersedes),
		AcceptanceCriteria: nonNil(fields.Acceptance),
		Scope:              Scope{Paths: nonNil(fields.Paths), Packages: nonNil(fields.Packages), Components: nonNil(fields.Components)},
	}
}

func namedItems(value any) []Named {
	var items []Named
	for _, raw := range anyList(value) {
		switch typed := raw.(type) {
		case map[string]any:
			statement := strings.TrimSpace(asString(typed["statement"]))
			ident := strings.TrimSpace(asString(typed["id"]))
			if ident == "" {
				ident = slug(statement)
			}
			if statement != "" {
				items = append(items, Named{ID: ident, Statement: statement})
			}
		case Named:
			if typed.Statement != "" {
				if typed.ID == "" {
					typed.ID = slug(typed.Statement)
				}
				items = append(items, typed)
			}
		default:
			text := strings.TrimSpace(asString(typed))
			if text == "" {
				continue
			}
			ident, statement := slug(text), text
			if left, right, ok := strings.Cut(text, ":"); ok {
				ident, statement = strings.TrimSpace(left), strings.TrimSpace(right)
			}
			if statement != "" {
				if ident == "" {
					ident = slug(statement)
				}
				items = append(items, Named{ID: ident, Statement: statement})
			}
		}
	}
	return items
}

func constraints(value any) []Constraint {
	var items []Constraint
	for _, raw := range anyList(value) {
		switch typed := raw.(type) {
		case map[string]any:
			statement := strings.TrimSpace(asString(typed["statement"]))
			if statement == "" {
				continue
			}
			ident := strings.TrimSpace(asString(typed["id"]))
			if ident == "" {
				ident = slug(statement)
			}
			items = append(items, Constraint{ID: ident, Statement: statement, Until: strings.TrimSpace(asString(typed["until"]))})
		default:
			text := strings.TrimSpace(asString(typed))
			until := ""
			if left, right, ok := strings.Cut(text, "|"); ok {
				text, until = left, strings.TrimSpace(right)
			}
			named := namedItems([]any{text})
			if len(named) > 0 {
				items = append(items, Constraint{ID: named[0].ID, Statement: named[0].Statement, Until: until})
			}
		}
	}
	return items
}

func statements(items []Named, _ any) []string {
	var lines []string
	for _, item := range items {
		lines = append(lines, item.Statement)
	}
	return lines
}

func constraintStatements(items []Constraint) []string {
	var lines []string
	for _, item := range items {
		if item.Until != "" {
			lines = append(lines, fmt.Sprintf("%s (until %s)", item.Statement, item.Until))
		} else {
			lines = append(lines, item.Statement)
		}
	}
	return lines
}

func slug(text string) string {
	out := strings.Trim(slugRE.ReplaceAllString(strings.ToLower(text), "-"), "-")
	if len(out) > 40 {
		out = out[:40]
	}
	if out == "" {
		return "item"
	}
	return out
}

func uniqueID(directory, title, day string) string {
	text := strings.Trim(slugRE.ReplaceAllString(strings.ToLower(title), "-"), "-")
	if len(text) > 40 {
		text = strings.Trim(text[:40], "-")
	}
	if text == "" {
		text = "decision"
	}
	base := day + "-" + text
	candidate := base
	suffix := 2
	for {
		if _, err := os.Stat(filepath.Join(directory, candidate+".json")); err != nil {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", base, suffix)
		suffix++
	}
}

func utcNow() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05Z")
}

func asString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}

func first(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func stringList(value any) []string {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return nil
		}
		return []string{text}
	case []string:
		var out []string
		for _, item := range typed {
			if strings.TrimSpace(item) != "" {
				out = append(out, strings.TrimSpace(item))
			}
		}
		return out
	case []any:
		var out []string
		for _, item := range typed {
			text := strings.TrimSpace(asString(item))
			if text != "" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func anyList(value any) []any {
	switch typed := value.(type) {
	case nil:
		return nil
	case []any:
		return typed
	case []string:
		var out []any
		for _, item := range typed {
			out = append(out, item)
		}
		return out
	case string:
		if strings.TrimSpace(typed) == "" {
			return nil
		}
		return []any{typed}
	default:
		return []any{typed}
	}
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func join(values []string) string {
	if len(values) == 0 {
		return "-"
	}
	return strings.Join(values, ", ")
}

func orDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

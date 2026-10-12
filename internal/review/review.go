// Package review asks TypeSafe's Jev to judge a proposal and keeps the verdict as returned.
package review

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"lintel/internal/model"
	"lintel/internal/paths"
	"lintel/internal/secret"
	"lintel/internal/store"
)

const (
	Endpoint     = "https://api.typesafe.ai/v1/systemone"
	apiKeyEnv    = "TYPESAFE_API_KEY"
	defaultModel = "jev-latest"
)

var legacyModels = map[string]bool{
	"claude-sonnet-4-5": true,
	"gpt-4.1":           true,
	"gemini-2.5-flash":  true,
	"gemini-2.5-pro":    true,
}

var wordRE = regexp.MustCompile(`[a-z0-9]{5,}`)

// Transport posts the review request and returns the JSON object.
type Transport func(url string, headers map[string]string, payload map[string]any) (map[string]any, error)

// APIKey returns the TypeSafe credential and any setup problem. Tests replace it.
var APIKey = defaultAPIKey

func defaultAPIKey() (string, string) {
	key, err := secret.Load()
	if err == nil && strings.TrimSpace(key) != "" {
		return strings.TrimSpace(key), ""
	}
	if env := strings.TrimSpace(os.Getenv(apiKeyEnv)); env != "" {
		return env, ""
	}
	if err != nil && !errors.Is(err, secret.ErrNotFound) {
		return "", err.Error() + " "
	}
	return "", ""
}

// Proposal asks Jev to judge a decision. A nil transport uses HTTPS.
func Proposal(decision store.Decision, config map[string]string, decisions []store.Decision, transport Transport) model.ReviewResult {
	key, problem := APIKey()
	key = strings.TrimSpace(key)
	modelName := reviewerModel(config)
	active, match := SelectActive(decision, decisions)
	if key == "" {
		return model.ReviewResult{
			Verdict: "revise",
			Summary: "TypeSafe API key is not configured.",
			Concerns: []string{
				problem + "Run `lintel login` and paste a key from https://console.typesafe.ai/keys, then run lintel review.",
			},
			Valid:    false,
			Evidence: "none",
		}
	}
	payload := map[string]any{
		"model":     modelName,
		"state":     state(decision, active, match),
		"questions": questions(active),
	}
	headers := map[string]string{
		"authorization": "Bearer " + key,
		"content-type":  "application/json",
	}
	if transport == nil {
		transport = DefaultTransport
	}
	response, err := transport(Endpoint, headers, payload)
	if err != nil {
		return failed(err.Error())
	}
	return policy(response, decision, active, match)
}

// SelectActive picks in-scope decisions. Path and component scope come first.
func SelectActive(proposal store.Decision, decisions []store.Decision) ([]store.Decision, string) {
	var others []store.Decision
	for _, item := range decisions {
		if item.ID != proposal.ID && model.IsGateOpen(item.Status) {
			others = append(others, item)
		}
	}
	var pathHits []store.Decision
	for _, item := range others {
		if pathsOverlap(proposal, item) {
			pathHits = append(pathHits, item)
		}
	}
	if len(pathHits) > 0 {
		return pathHits, "path"
	}
	var componentHits []store.Decision
	for _, item := range others {
		if componentsOverlap(proposal, item) {
			componentHits = append(componentHits, item)
		}
	}
	if len(componentHits) > 0 {
		return componentHits, "component"
	}
	var textHits []store.Decision
	for _, item := range others {
		if textOverlap(proposal, item) {
			textHits = append(textHits, item)
		}
	}
	if len(textHits) > 0 {
		return textHits, "weak"
	}
	return nil, "none"
}

// DefaultTransport POSTs JSON and returns the decoded object.
func DefaultTransport(url string, headers map[string]string, payload map[string]any) (map[string]any, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	client := &http.Client{Timeout: 60 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		detail := string(raw)
		if len(detail) > 240 {
			detail = detail[:240]
		}
		return nil, fmt.Errorf("HTTP %d. %s", response.StatusCode, strings.TrimSpace(detail))
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

func reviewerModel(config map[string]string) string {
	name := strings.TrimSpace(config["model"])
	if name == "" {
		name = defaultModel
	}
	if legacyModels[name] {
		return defaultModel
	}
	return name
}

func state(decision store.Decision, active []store.Decision, match string) map[string]any {
	proposal := decision.Proposal
	intent := proposal.Intent
	if intent == "" {
		intent = decision.Question
	}
	change := proposal.ProposedChange
	if change == "" {
		change = proposal.Choice
	}
	var activeRecords []any
	for _, item := range active {
		activeRecords = append(activeRecords, activeRecord(item))
	}
	if activeRecords == nil {
		activeRecords = []any{}
	}
	return map[string]any{
		"kind": decision.Kind, "title": decision.Title, "question": decision.Question,
		"intent": intent, "proposed_change": change, "context": decision.Context,
		"alternatives": proposal.Alternatives, "why": proposal.Why,
		"rejected_because": proposal.RejectedBecause, "risks": proposal.Risks,
		"scope": map[string]any{
			"paths": proposal.Scope.Paths, "packages": proposal.Scope.Packages, "components": proposal.Scope.Components,
		},
		"invariants_added":         namedAny(proposal.Invariants),
		"implementation_decisions": namedAny(proposal.ImplementationDecisions),
		"constraints":              constraintAny(proposal.Constraints),
		"supersedes":               orEmpty(proposal.Supersedes),
		"acceptance_criteria":      orEmpty(proposal.AcceptanceCriteria),
		"active":                   activeRecords, "scope_match": match,
	}
}

func activeRecord(decision store.Decision) map[string]any {
	proposal := decision.Proposal
	return map[string]any{
		"id": decision.ID, "kind": decision.Kind,
		"invariants":               namedAny(proposal.Invariants),
		"implementation_decisions": namedAny(proposal.ImplementationDecisions),
		"constraints":              constraintAny(proposal.Constraints),
		"scope": map[string]any{
			"paths": proposal.Scope.Paths, "packages": proposal.Scope.Packages, "components": proposal.Scope.Components,
		},
	}
}

func questions(active []store.Decision) map[string]any {
	criteria := map[string]any{"none": "No active invariant is contradicted."}
	for _, item := range invariants(active) {
		criteria[item.ID] = item.Statement
	}
	return map[string]any{
		"verdict": map[string]any{
			"type": "choice",
			"instructions": "Judge this proposal only against the active invariants in scope. " +
				"accept: none of them is contradicted, and the proposal has intent, " +
				"a change, scope, and acceptance criteria. " +
				"Also accept when the proposal supersedes the decision that established " +
				"the contradicted invariant, restates every other invariant on that decision, " +
				"and is a kickoff if that decision is a kickoff. " +
				"revise: intent, the change, the scope, or the acceptance criteria are missing, " +
				"or the proposal supersedes a decision that still carries invariants it does not restate, " +
				"or it supersedes a kickoff without itself being a kickoff. " +
				"block: an in-scope invariant is contradicted and the proposal does not supersede " +
				"the decision that established it, or a tenant or authorization boundary would be crossed.",
			"criteria": map[string]any{
				"accept": "No in-scope invariant is contradicted, or the contradicted one is retired by supersedes without dropping other rules.",
				"revise": "The proposal is missing intent, a change, scope, or acceptance criteria, or supersedes drops other rules or the kickoff.",
				"block":  "An in-scope invariant is contradicted and its decision is not in supersedes.",
			},
		},
		"affected": map[string]any{
			"type":         "choice",
			"instructions": "Which active invariant does this proposal contradict? Choose none when it contradicts none.",
			"criteria":     criteria,
		},
	}
}

func policy(response map[string]any, decision store.Decision, active []store.Decision, match string) model.ReviewResult {
	answers, _ := response["answers"].(map[string]any)
	if answers == nil {
		return failed("TypeSafe Jev returned no answers.")
	}
	verdictAnswer, _ := answers["verdict"].(map[string]any)
	if verdictAnswer == nil {
		return failed("TypeSafe Jev returned no verdict.")
	}
	choice, _ := verdictAnswer["choice"].(string)
	if choice != "accept" && choice != "revise" && choice != "block" {
		return failed("TypeSafe Jev returned an invalid verdict.")
	}
	probabilities, _ := verdictAnswer["probabilities"].(map[string]any)
	if probabilities == nil {
		probabilities = map[string]any{}
	}
	affectedAnswer, _ := answers["affected"].(map[string]any)
	affected := "none"
	if affectedAnswer != nil {
		if text, ok := affectedAnswer["choice"].(string); ok && text != "" {
			affected = text
		}
	}
	known := map[string]string{}
	for _, item := range invariants(active) {
		known[item.ID] = item.Statement
	}
	evidence := "unsupported"
	if _, ok := known[affected]; ok {
		evidence = "direct"
	}
	summary := "Jev chose " + choice + "."
	if match == "weak" {
		summary += " Scope match is weak text overlap."
	} else if match == "none" {
		summary += " No in-scope decision matched."
	}
	if retired, ok := retireSuperseded(decision, active, choice, affected, summary, probabilities); ok {
		return retired
	}
	if choice == "accept" {
		ev := evidence
		if _, ok := known[affected]; !ok {
			ev = "none"
		}
		return model.ReviewResult{Verdict: "accept", Summary: summary, Concerns: []string{}, Valid: true, Evidence: ev, Probabilities: probabilities}
	}
	if _, ok := known[affected]; !ok {
		if choice == "revise" {
			return incompleteRevise(decision, summary, probabilities)
		}
		return model.ReviewResult{
			Verdict: choice, Summary: summary + " The review named no in-scope invariant, so it was rejected.",
			Concerns: []string{}, Valid: false, Evidence: "unsupported", Probabilities: probabilities,
		}
	}
	statement := known[affected]
	conflict := fmt.Sprintf("The proposal conflicts with %s: %s", affected, statement)
	required := "Preserve this invariant, or supersede the decision that established it."
	criteria := "State how the invariant remains true, or list its decision in supersedes."
	code := ""
	if choice == "block" {
		code = "JEV_BLOCK"
	}
	return model.ReviewResult{
		Verdict: choice, Summary: summary, Concerns: []string{affected + ": " + statement}, Valid: true,
		AffectedInvariant: affected, Conflict: conflict, RequiredChange: required, AcceptanceCriteria: criteria,
		Evidence: "direct", Probabilities: probabilities, Code: code,
	}
}

func incompleteRevise(decision store.Decision, summary string, probabilities map[string]any) model.ReviewResult {
	proposal := decision.Proposal
	var missing []string
	if strings.TrimSpace(proposal.Intent) == "" {
		missing = append(missing, "intent")
	}
	change := strings.TrimSpace(proposal.ProposedChange)
	if change == "" {
		change = strings.TrimSpace(proposal.Choice)
	}
	if change == "" {
		missing = append(missing, "a change")
	}
	if len(proposal.Scope.Paths) == 0 {
		missing = append(missing, "a scope")
	}
	if len(proposal.AcceptanceCriteria) == 0 {
		missing = append(missing, "acceptance criteria")
	}
	var detail, required string
	if len(missing) > 0 {
		detail = "Missing " + strings.Join(missing, ", ") + "."
		required = "Add " + strings.Join(missing, ", ") + "."
	} else {
		detail = "Jev asked to revise this proposal and named no invariant."
		required = "State the conflict with an active invariant, or say what is missing."
	}
	return model.ReviewResult{
		Verdict: "revise", Summary: summary + " " + detail, Concerns: []string{detail}, Valid: true,
		RequiredChange:     required,
		AcceptanceCriteria: "The proposal includes intent, a change, a scope, and acceptance criteria.",
		Evidence:           "none", Probabilities: probabilities,
	}
}

func retireSuperseded(decision store.Decision, active []store.Decision, choice, affected, summary string, probabilities map[string]any) (model.ReviewResult, bool) {
	listed := map[string]bool{}
	for _, item := range decision.Proposal.Supersedes {
		if strings.TrimSpace(item) != "" {
			listed[item] = true
		}
	}
	if len(listed) == 0 {
		return model.ReviewResult{}, false
	}
	var owners []store.Decision
	for _, item := range active {
		if listed[item.ID] {
			owners = append(owners, item)
		}
	}
	if len(owners) == 0 {
		return model.ReviewResult{}, false
	}
	owned := invariants(owners)
	ownedIDs := map[string]bool{}
	for _, item := range owned {
		ownedIDs[item.ID] = true
	}
	restated := map[string]bool{}
	for _, item := range invariants([]store.Decision{decision}) {
		restated[item.ID] = true
	}
	retired := map[string]bool{}
	if ownedIDs[affected] {
		retired[affected] = true
	}
	var dropped []store.Named
	for _, item := range owned {
		if !restated[item.ID] && !retired[item.ID] {
			dropped = append(dropped, item)
		}
	}
	shuts := false
	for _, item := range owners {
		if item.Kind == model.KindKickoff && decision.Kind != model.KindKickoff {
			shuts = true
		}
	}
	if len(dropped) > 0 || shuts {
		names := "the kickoff"
		if len(dropped) > 0 {
			var ids []string
			for _, item := range dropped {
				ids = append(ids, item.ID)
			}
			names = strings.Join(ids, ", ")
		}
		var sorted []string
		for id := range listed {
			sorted = append(sorted, id)
		}
		sort.Strings(sorted)
		conflict := fmt.Sprintf("Superseding %s drops rules this proposal does not replace: %s.", strings.Join(sorted, ", "), names)
		if shuts {
			conflict += " Superseding the kickoff shuts the gate."
		}
		var concerns []string
		for _, item := range dropped {
			concerns = append(concerns, item.ID+": "+item.Statement)
		}
		affectedID := ""
		evidence := "none"
		if len(dropped) > 0 {
			affectedID = dropped[0].ID
			evidence = "direct"
		}
		return model.ReviewResult{
			Verdict: "revise", Summary: summary + " " + conflict, Concerns: concerns, Valid: true,
			AffectedInvariant: affectedID, Conflict: conflict,
			RequiredChange:     "Restate every rule you are keeping. Propose a kickoff when you are replacing the kickoff.",
			AcceptanceCriteria: "The proposal restates the rules it keeps, or it is a kickoff that replaces the old kickoff.",
			Evidence:           evidence, Probabilities: probabilities,
		}, true
	}
	if (choice == "block" || choice == "revise") && retired[affected] {
		return model.ReviewResult{
			Verdict:  "accept",
			Summary:  summary + " The proposal supersedes the decision that established the contradicted invariant.",
			Concerns: []string{}, Valid: true, Evidence: "none", Probabilities: probabilities,
		}, true
	}
	return model.ReviewResult{}, false
}

func failed(detail string) model.ReviewResult {
	return model.ReviewResult{
		Verdict:  "revise",
		Summary:  "TypeSafe Jev request failed: " + detail,
		Concerns: []string{"Fix the TypeSafe configuration and run lintel review."},
		Valid:    false, Evidence: "none",
	}
}

func invariants(decisions []store.Decision) []store.Named {
	var found []store.Named
	seen := map[string]bool{}
	for _, decision := range decisions {
		for _, item := range decision.Proposal.Invariants {
			ident := strings.TrimSpace(item.ID)
			statement := strings.TrimSpace(item.Statement)
			if ident == "" || statement == "" || seen[ident] || ident == "none" {
				continue
			}
			seen[ident] = true
			found = append(found, store.Named{ID: ident, Statement: statement})
		}
	}
	return found
}

func pathsOverlap(left, right store.Decision) bool {
	for _, candidate := range left.Proposal.Scope.Paths {
		for _, pattern := range right.Proposal.Scope.Paths {
			if candidate == pattern || covers(pattern, candidate) || covers(candidate, pattern) {
				return true
			}
		}
	}
	return false
}

func covers(pattern, path string) bool {
	if !strings.Contains(pattern, "*") {
		return false
	}
	return paths.GlobMatch(pattern, path)
}

func componentsOverlap(left, right store.Decision) bool {
	rightItems := map[string]bool{}
	for _, item := range right.Proposal.Scope.Components {
		rightItems[item] = true
	}
	for _, item := range left.Proposal.Scope.Components {
		if rightItems[item] {
			return true
		}
	}
	return false
}

func textOverlap(left, right store.Decision) bool {
	leftWords := words(proposalText(left))
	rightWords := words(recordText(right))
	for word := range leftWords {
		if rightWords[word] {
			return true
		}
	}
	return false
}

func proposalText(decision store.Decision) string {
	return strings.Join([]string{
		decision.Title, decision.Question, decision.Proposal.Intent, decision.Proposal.Choice, decision.Proposal.ProposedChange,
	}, " ")
}

func recordText(decision store.Decision) string {
	parts := []string{decision.Title}
	for _, item := range decision.Proposal.Invariants {
		parts = append(parts, item.Statement)
	}
	for _, item := range decision.Proposal.ImplementationDecisions {
		parts = append(parts, item.Statement)
	}
	for _, item := range decision.Proposal.Constraints {
		parts = append(parts, item.Statement)
	}
	return strings.Join(parts, " ")
}

func words(text string) map[string]bool {
	out := map[string]bool{}
	for _, word := range wordRE.FindAllString(strings.ToLower(text), -1) {
		out[word] = true
	}
	return out
}

func namedAny(items []store.Named) []any {
	out := []any{}
	for _, item := range items {
		out = append(out, map[string]any{"id": item.ID, "statement": item.Statement})
	}
	return out
}

func constraintAny(items []store.Constraint) []any {
	out := []any{}
	for _, item := range items {
		out = append(out, map[string]any{"id": item.ID, "statement": item.Statement, "until": item.Until})
	}
	return out
}

func orEmpty(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// Model is the Jev model named in the project config.
func Model(config map[string]string) string { return reviewerModel(config) }

// ActiveRecord is how a decision is described to Jev.
func ActiveRecord(decision store.Decision) map[string]any { return activeRecord(decision) }

// Invariants lists the named invariants on these decisions, once each.
func Invariants(decisions []store.Decision) []store.Named { return invariants(decisions) }

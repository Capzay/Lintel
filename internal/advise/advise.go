// Package advise asks TypeSafe's Jev which of several options fits this
// codebase best, before the agent proposes one. Jev chooses; the agent supplies
// the candidates, and the human still approves the proposal that follows.
package advise

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"lintel/internal/paths"
	"lintel/internal/review"
	"lintel/internal/scan"
	"lintel/internal/store"
)

const (
	maxOptions   = 8
	maxFileBytes = 12 << 10
	maxAllBytes  = 40 << 10
	maxTree      = 200
)

var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, ".venv": true, "venv": true,
	"target": true, "dist": true, "build": true, ".next": true, ".lintel": true, "__pycache__": true,
}

var idRE = regexp.MustCompile(`[^a-z0-9]+`)

// Option is one candidate answer.
type Option struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

// Advice is Jev's recommendation, as stored in .lintel/advice.
type Advice struct {
	ID             string         `json:"id"`
	Question       string         `json:"question"`
	Context        string         `json:"context"`
	Options        []Option       `json:"options"`
	Recommendation string         `json:"recommendation"`
	Probabilities  map[string]any `json:"probabilities"`
	AtRisk         string         `json:"at_risk"`
	CreatedAt      string         `json:"created_at"`
}

// ParseOptions reads --option values written as "id: description" or "id".
func ParseOptions(values []string) ([]Option, error) {
	var options []Option
	seen := map[string]bool{}
	for _, value := range values {
		id, description, _ := strings.Cut(value, ":")
		id = strings.TrimSpace(id)
		if id == "" {
			return nil, fmt.Errorf("Option %q has no id. Write --option \"id: description\".", value)
		}
		if seen[id] {
			return nil, fmt.Errorf("Option %s is listed twice.", id)
		}
		seen[id] = true
		description = strings.TrimSpace(description)
		if description == "" {
			description = id
		}
		options = append(options, Option{ID: id, Description: description})
	}
	if len(options) < 2 {
		return nil, fmt.Errorf("Give Jev at least two --option values to choose between.")
	}
	if len(options) > maxOptions {
		return nil, fmt.Errorf("Give Jev at most %d options.", maxOptions)
	}
	return options, nil
}

// Ask sends the question, the options, and the codebase to Jev. A nil
// transport uses HTTPS.
func Ask(project store.Project, question, context string, options []Option, files []string, transport review.Transport) (Advice, error) {
	if strings.TrimSpace(question) == "" {
		return Advice{}, fmt.Errorf("--question is required.")
	}
	key, problem := review.APIKey()
	if strings.TrimSpace(key) == "" {
		return Advice{}, fmt.Errorf("%sTypeSafe API key is not configured. Run `lintel login`.", problem)
	}
	approved, err := project.Approved()
	if err != nil {
		return Advice{}, err
	}
	codebase, err := Codebase(project.Root, files)
	if err != nil {
		return Advice{}, err
	}
	var active []any
	for _, item := range approved {
		record := review.ActiveRecord(item)
		record["title"] = item.Title
		record["choice"] = item.Proposal.Choice
		active = append(active, record)
	}
	if active == nil {
		active = []any{}
	}
	var optionList []any
	criteria := map[string]any{}
	for _, option := range options {
		optionList = append(optionList, map[string]any{"id": option.ID, "description": option.Description})
		criteria[option.ID] = option.Description
	}
	risk := map[string]any{"none": "The recommended option contradicts no active invariant."}
	for _, item := range review.Invariants(approved) {
		risk[item.ID] = item.Statement
	}
	payload := map[string]any{
		"model": review.Model(project.Config()),
		"state": map[string]any{
			"kind": "advice", "question": question, "context": context,
			"options": optionList, "codebase": codebase, "active": active,
		},
		"questions": map[string]any{
			"recommendation": map[string]any{
				"type": "choice",
				"instructions": "Recommend the option that best fits this codebase. Weigh the dependencies it already has, " +
					"its approved decisions and invariants, the files shown, and the stated context. " +
					"Prefer an option that reuses what the codebase already depends on over adding something new, " +
					"unless the context says otherwise.",
				"criteria": criteria,
			},
			"affected": map[string]any{
				"type":         "choice",
				"instructions": "Which active invariant would the recommended option put at risk? Choose none when it risks none.",
				"criteria":     risk,
			},
		},
	}
	headers := map[string]string{"authorization": "Bearer " + strings.TrimSpace(key), "content-type": "application/json"}
	if transport == nil {
		transport = review.DefaultTransport
	}
	response, err := transport(review.Endpoint, headers, payload)
	if err != nil {
		return Advice{}, fmt.Errorf("TypeSafe Jev request failed: %s", err.Error())
	}
	answers, _ := response["answers"].(map[string]any)
	recommendation, _ := answers["recommendation"].(map[string]any)
	choice, _ := recommendation["choice"].(string)
	if _, ok := criteria[choice]; !ok {
		return Advice{}, fmt.Errorf("TypeSafe Jev did not choose one of the options.")
	}
	probabilities, _ := recommendation["probabilities"].(map[string]any)
	if probabilities == nil {
		probabilities = map[string]any{}
	}
	atRisk := "none"
	if affected, ok := answers["affected"].(map[string]any); ok {
		if text, ok := affected["choice"].(string); ok && risk[text] != nil {
			atRisk = text
		}
	}
	now := time.Now().UTC()
	advice := Advice{
		ID: uniqueID(project.Dir, question, now.Format("2006-01-02")), Question: question, Context: context,
		Options: options, Recommendation: choice, Probabilities: probabilities, AtRisk: atRisk,
		CreatedAt: now.Format("2006-01-02T15:04:05Z"),
	}
	if err := Save(project, advice); err != nil {
		return Advice{}, err
	}
	return advice, nil
}

// Codebase describes the project to Jev: declared dependencies, a shallow
// file tree, and the contents of the files the agent named.
func Codebase(root string, files []string) (map[string]any, error) {
	dependencies := map[string]any{}
	var tree []string
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || path == root {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		depth := strings.Count(rel, "/")
		if entry.IsDir() {
			if skipDirs[entry.Name()] || depth >= 3 {
				return filepath.SkipDir
			}
			if depth < 2 && len(tree) < maxTree {
				tree = append(tree, rel+"/")
			}
			return nil
		}
		if depth < 2 && len(tree) < maxTree {
			tree = append(tree, rel)
		}
		if paths.IsManifest(rel) {
			raw, err := os.ReadFile(path)
			if err == nil {
				if found, err := scan.ManifestPackages(rel, string(raw)); err == nil {
					dependencies[rel] = sortedKeys(found)
				}
			}
		}
		return nil
	})
	contents := map[string]any{}
	total := 0
	for _, raw := range files {
		rel, err := paths.Relativize(root, raw)
		if err != nil {
			return nil, err
		}
		if paths.IsGatePath(rel) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return nil, fmt.Errorf("Cannot read %s: %s", rel, err.Error())
		}
		text := string(data)
		if len(text) > maxFileBytes {
			text = text[:maxFileBytes] + "\n[truncated]"
		}
		if total+len(text) > maxAllBytes {
			break
		}
		total += len(text)
		contents[rel] = text
	}
	if tree == nil {
		tree = []string{}
	}
	return map[string]any{"dependencies": dependencies, "tree": tree, "files": contents}, nil
}

// Save writes advice to .lintel/advice/<id>.json.
func Save(project store.Project, advice Advice) error {
	dir := filepath.Join(project.Dir, "advice")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(advice, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, advice.ID+".json"), append(encoded, '\n'), 0o644)
}

// Load reads stored advice.
func Load(project store.Project, id string) (Advice, error) {
	raw, err := os.ReadFile(filepath.Join(project.Dir, "advice", id+".json"))
	if err != nil {
		return Advice{}, fmt.Errorf("No advice %s. Run `lintel ask` first.", id)
	}
	var advice Advice
	if err := json.Unmarshal(raw, &advice); err != nil {
		return Advice{}, err
	}
	return advice, nil
}

// Option returns the option with this id.
func (a Advice) Option(id string) Option {
	for _, option := range a.Options {
		if option.ID == id {
			return option
		}
	}
	return Option{ID: id, Description: id}
}

// Format renders advice for the agent and the human.
func Format(advice Advice) string {
	type ranked struct {
		id string
		p  float64
	}
	var ranking []ranked
	for _, option := range advice.Options {
		p, _ := advice.Probabilities[option.ID].(float64)
		ranking = append(ranking, ranked{option.ID, p})
	}
	sort.SliceStable(ranking, func(i, j int) bool { return ranking[i].p > ranking[j].p })
	var parts []string
	for _, item := range ranking {
		parts = append(parts, fmt.Sprintf("%s %.2f", item.id, item.p))
	}
	chosen := advice.Option(advice.Recommendation)
	return strings.Join([]string{
		"Advice    " + advice.ID,
		"Question  " + advice.Question,
		"Jev recommends  " + chosen.ID + ": " + chosen.Description,
		"Ranking   " + strings.Join(parts, ", "),
		"At risk   " + advice.AtRisk,
		"",
		"Jev advises. To go ahead, propose it: `lintel propose --from-advice " + advice.ID +
			" --kind change --title \"...\" --path ... --package ...`, then run `lintel wait`.",
	}, "\n")
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func uniqueID(dir, question, day string) string {
	text := strings.Trim(idRE.ReplaceAllString(strings.ToLower(question), "-"), "-")
	if len(text) > 40 {
		text = strings.Trim(text[:40], "-")
	}
	if text == "" {
		text = "advice"
	}
	base := day + "-" + text
	candidate := base
	for suffix := 2; ; suffix++ {
		if _, err := os.Stat(filepath.Join(dir, "advice", candidate+".json")); err != nil {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", base, suffix)
	}
}

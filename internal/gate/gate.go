// Package gate decides whether an agent action may change the project.
package gate

import (
	"fmt"
	"sort"
	"strings"

	"lintel/internal/model"
	"lintel/internal/paths"
	"lintel/internal/scan"
	"lintel/internal/store"
)

// Evaluate allows or denies one action.
func Evaluate(root string, action model.Action, decisions []store.Decision, open *store.Decision) model.GateResult {
	var approved []store.Decision
	for _, item := range decisions {
		if model.IsGateOpen(item.Status) {
			approved = append(approved, item)
		}
	}
	var kickoffs []store.Decision
	for _, item := range approved {
		if item.Kind == model.KindKickoff {
			kickoffs = append(kickoffs, item)
		}
	}
	if action.Kind == "read" {
		return model.Allow()
	}
	if action.Kind == "shell" {
		return shell(root, action, approved, kickoffs, open)
	}
	if action.Kind != "write" {
		return deny(open, "Unknown action "+action.Kind+".")
	}
	if len(action.Paths) == 0 {
		return deny(open, "Write has no path to check.")
	}
	type pair struct{ raw, rel string }
	var relative []pair
	for _, raw := range action.Paths {
		rel, err := paths.Relativize(root, raw)
		if err != nil {
			if paths.IsScratch(paths.Resolve(root, raw)) {
				continue
			}
			return deny(open, err.Error())
		}
		if paths.IsProtected(rel) {
			return deny(open, protected(rel))
		}
		if paths.IsGateConfig(rel) {
			continue
		}
		relative = append(relative, pair{raw, rel})
	}
	if len(relative) == 0 {
		return model.Allow()
	}
	if len(kickoffs) == 0 {
		return deny(open, "No approved kickoff decision. Propose the stack and wait for approval before writing project files.")
	}
	for _, item := range relative {
		if item.rel == "" || item.rel == "." {
			return deny(open, "Write has no path to check.")
		}
		content, ok := action.Contents[item.raw]
		var contentPtr *string
		if ok {
			contentPtr = &content
		} else if alt, ok := action.Contents[item.rel]; ok {
			contentPtr = &alt
		}
		if reason := checkWrite(item.rel, contentPtr, approved); reason != "" {
			return deny(open, reason)
		}
	}
	return model.Allow()
}

func shell(root string, action model.Action, approved, kickoffs []store.Decision, open *store.Decision) model.GateResult {
	segments := scan.Parse(action.Command)
	if len(segments) == 0 {
		return deny(open, "Empty command.")
	}
	safe := true
	var writes []string
	for _, segment := range segments {
		if !scan.IsLintel(segment) && !scan.IsReadOnly(segment) {
			safe = false
		}
		for _, target := range segment.Writes {
			rel, err := paths.Relativize(root, paths.Resolve(root, target))
			if err != nil {
				if paths.IsScratch(paths.Resolve(root, target)) {
					continue
				}
				return deny(open, err.Error())
			}
			if paths.IsProtected(rel) {
				return deny(open, protected(rel))
			}
			writes = append(writes, rel)
		}
	}
	if safe && len(writes) == 0 {
		return model.Allow()
	}
	if len(kickoffs) == 0 {
		return deny(open, "No approved kickoff decision. Read the repo and propose a stack before running commands that change it.")
	}
	if added, ok := scan.PackagesFromShell(action.Command); ok {
		unknown := difference(added, approvedPackages(approved))
		if len(unknown) > 0 {
			return deny(open, "Command introduces packages that are not on an approved decision: "+strings.Join(unknown, ", "))
		}
	}
	for _, rel := range writes {
		if reason := checkWrite(rel, nil, approved); reason != "" {
			return deny(open, reason)
		}
	}
	return model.Allow()
}

func protected(relative string) string {
	if paths.IsGatePath(relative) {
		return relative + " is Lintel's decision record. Only lintel commands write it, and only the human approves."
	}
	return relative + " holds the Lintel hooks. Run `lintel init` to change them."
}

func checkWrite(relative string, content *string, approved []store.Decision) string {
	if paths.IsGatePath(relative) {
		return ""
	}
	if paths.IsLockfile(relative) {
		return relative + " is a lockfile. Add the package in an approved decision, then install it with the package manager instead of editing the lockfile."
	}
	if paths.IsManifest(relative) {
		if content == nil {
			return "Cannot verify packages in " + relative + ", so the edit stays blocked."
		}
		packages, err := scan.ManifestPackages(relative, *content)
		if err != nil {
			return fmt.Sprintf("Cannot parse %s to verify packages: %s", relative, err.Error())
		}
		unknown := difference(packages, approvedPackages(approved))
		if len(unknown) > 0 {
			return relative + " introduces unapproved packages: " + strings.Join(unknown, ", ")
		}
		return ""
	}
	if paths.IsSensitive(relative) {
		if !specificallyCovered(relative, approved) {
			return relative + " is schema, CI, Docker, or framework config. Approve a decision that lists that path explicitly."
		}
		return ""
	}
	if !covered(relative, approved) {
		return relative + " is outside the approved path scope."
	}
	if content != nil && paths.IsSource(relative) {
		unknown := difference(scan.SourceImports(relative, *content), approvedPackages(approved))
		if len(unknown) > 0 {
			return relative + " imports unapproved packages: " + strings.Join(unknown, ", ")
		}
	}
	return ""
}

func approvedPackages(decisions []store.Decision) map[string]bool {
	names := map[string]bool{}
	for _, decision := range decisions {
		for _, pkg := range decision.Proposal.Scope.Packages {
			names[pkg] = true
		}
	}
	return names
}

func patterns(decisions []store.Decision) []string {
	var out []string
	for _, decision := range decisions {
		out = append(out, decision.Proposal.Scope.Paths...)
	}
	return out
}

func covered(relative string, decisions []store.Decision) bool {
	for _, pattern := range patterns(decisions) {
		if paths.GlobMatch(pattern, relative) {
			return true
		}
	}
	return false
}

func specificallyCovered(relative string, decisions []store.Decision) bool {
	for _, pattern := range patterns(decisions) {
		if !paths.IsCatchAll(pattern) && paths.GlobMatch(pattern, relative) {
			return true
		}
	}
	return false
}

func deny(open *store.Decision, reason string) model.GateResult {
	advice := ""
	if open != nil {
		advice = store.HumanAdvice(open)
	}
	user := advice
	if user == "" {
		user = "Lintel blocked this action. " + reason
	} else if !strings.Contains(advice, reason) {
		user = advice + " " + reason
	}
	agent := reason + " " + hint(open)
	if advice != "" {
		agent = advice + " " + agent
	}
	return model.GateResult{Allow: false, UserMessage: user, AgentMessage: agent}
}

func hint(open *store.Decision) string {
	if open == nil {
		return "Do not work around this block. Propose the choice with `lintel propose`, then run `lintel wait` in this same turn."
	}
	verdict := ""
	if open.Jev != nil {
		verdict = open.Jev.Verdict
	}
	if verdict == model.VerdictRevise || verdict == model.VerdictBlock || verdict == model.VerdictAccept {
		return fmt.Sprintf("Run `lintel wait %s` in this same turn. Do not end the turn to ask the human to approve later. When wait returns authorized, implement only that scope. When it returns rejected, stop. Do not propose a replacement unless the human asks for one.", open.ID)
	}
	return fmt.Sprintf("Open decision %s is not approved yet. Wait for Jev's review and for the human to decide.", open.ID)
}

func difference(found, approved map[string]bool) []string {
	var unknown []string
	for name := range found {
		if !approved[name] {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(unknown)
	return unknown
}

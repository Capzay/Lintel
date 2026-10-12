package scan

import "strings"

func pyprojectPackages(content string) (map[string]bool, error) {
	data, err := parseTOML(content)
	if err != nil {
		return nil, &ManifestError{Msg: "pyproject.toml is not valid TOML"}
	}
	names := map[string]bool{}
	project, _ := data["project"].(map[string]any)
	for _, dep := range stringList(project["dependencies"]) {
		if name := pep508Name(dep); name != "" {
			names[name] = true
		}
	}
	optional, _ := project["optional-dependencies"].(map[string]any)
	for _, deps := range optional {
		for _, dep := range stringList(deps) {
			if name := pep508Name(dep); name != "" {
				names[name] = true
			}
		}
	}
	tool, _ := data["tool"].(map[string]any)
	poetry, _ := tool["poetry"].(map[string]any)
	deps, _ := poetry["dependencies"].(map[string]any)
	for name := range deps {
		if !strings.EqualFold(name, "python") {
			names[name] = true
		}
	}
	return names, nil
}

func pipfilePackages(content string) (map[string]bool, error) {
	data, err := parseTOML(content)
	if err != nil {
		return nil, &ManifestError{Msg: "Pipfile is not valid TOML"}
	}
	names := map[string]bool{}
	for _, section := range []string{"packages", "dev-packages"} {
		deps, _ := data[section].(map[string]any)
		for name := range deps {
			names[name] = true
		}
	}
	return names, nil
}

func stringList(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		var out []string
		for _, item := range typed {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

// parseTOML reads the small subset of TOML used by dependency manifests.
func parseTOML(content string) (map[string]any, error) {
	root := map[string]any{}
	current := root
	var table []string
	lines := strings.Split(content, "\n")
	for i := 0; i < len(lines); i++ {
		line := stripComment(lines[i])
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[[") {
			return nil, errUnbalanced
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			name := strings.TrimSpace(line[1 : len(line)-1])
			name = strings.Trim(name, `"`)
			table = strings.Split(name, ".")
			current = ensureTable(root, table)
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, errUnbalanced
		}
		key = strings.TrimSpace(strings.Trim(key, `"`))
		value = strings.TrimSpace(value)
		if strings.HasPrefix(value, "[") && !strings.HasSuffix(value, "]") {
			for i+1 < len(lines) && !strings.Contains(value, "]") {
				i++
				value += " " + strings.TrimSpace(stripComment(lines[i]))
			}
		}
		parsed, err := parseTOMLValue(value)
		if err != nil {
			return nil, err
		}
		current[key] = parsed
	}
	return root, nil
}

func ensureTable(root map[string]any, table []string) map[string]any {
	current := root
	for _, part := range table {
		next, ok := current[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[part] = next
		}
		current = next
	}
	return current
}

func parseTOMLValue(value string) (any, error) {
	if strings.HasPrefix(value, `"`) || strings.HasPrefix(value, `'`) {
		return unquote(value), nil
	}
	if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
		inner := strings.TrimSpace(value[1 : len(value)-1])
		if inner == "" {
			return []string{}, nil
		}
		var items []string
		for _, part := range splitCSV(inner) {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			items = append(items, unquote(part))
		}
		return items, nil
	}
	if strings.HasPrefix(value, "{") && strings.HasSuffix(value, "}") {
		return map[string]any{}, nil
	}
	return value, nil
}

func unquote(value string) string {
	if len(value) >= 2 {
		if value[0] == '"' && value[len(value)-1] == '"' {
			return value[1 : len(value)-1]
		}
		if value[0] == '\'' && value[len(value)-1] == '\'' {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func splitCSV(value string) []string {
	var parts []string
	var buf strings.Builder
	quote := byte(0)
	for i := 0; i < len(value); i++ {
		ch := value[i]
		if quote == 0 && (ch == '"' || ch == '\'') {
			quote = ch
			buf.WriteByte(ch)
			continue
		}
		if quote != 0 && ch == quote {
			quote = 0
			buf.WriteByte(ch)
			continue
		}
		if quote == 0 && ch == ',' {
			parts = append(parts, buf.String())
			buf.Reset()
			continue
		}
		buf.WriteByte(ch)
	}
	parts = append(parts, buf.String())
	return parts
}

func stripComment(line string) string {
	quote := byte(0)
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if quote == 0 && (ch == '"' || ch == '\'') {
			quote = ch
			continue
		}
		if quote != 0 && ch == quote {
			quote = 0
			continue
		}
		if quote == 0 && ch == '#' {
			return line[:i]
		}
	}
	return line
}

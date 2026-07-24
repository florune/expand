package template

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/florune/expand/internal/model"
)

var placeholder = regexp.MustCompile(`\{\{\s*([a-zA-Z][a-zA-Z0-9_-]*)\s*\}\}`)

// Variables returns template variables in first-appearance order. Declared
// metadata is preserved; undeclared placeholders become editable text fields
// whose visible fallback is the variable name itself.
func Variables(template string, declared []model.Variable) []model.Variable {
	metadata := make(map[string]model.Variable, len(declared))
	for _, variable := range declared {
		variable.Name = strings.TrimSpace(variable.Name)
		if variable.Name == "" {
			continue
		}
		metadata[variable.Name] = variable
	}
	seen := make(map[string]struct{})
	variables := make([]model.Variable, 0)
	for _, match := range placeholder.FindAllStringSubmatch(template, -1) {
		name := match[1]
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		variable, ok := metadata[name]
		if !ok {
			variable = model.Variable{Name: name, Label: name, Type: "text", Default: name}
		}
		if strings.TrimSpace(variable.Label) == "" {
			variable.Label = name
		}
		if strings.TrimSpace(variable.Type) == "" {
			variable.Type = "text"
		}
		variables = append(variables, variable)
	}
	return variables
}

func Render(entry model.Entry, values map[string]string) (string, error) {
	return RenderAt(entry, values, time.Now())
}

func RenderAt(entry model.Entry, values map[string]string, now time.Time) (string, error) {
	variables := Variables(entry.Template, entry.Variables)
	allowed := make(map[string]model.Variable, len(variables))
	for _, variable := range variables {
		allowed[variable.Name] = variable
	}

	missing := make([]string, 0)
	for _, variable := range variables {
		value := resolvedValue(variable, values[variable.Name], now)
		if value == "" {
			value = variable.Default
		}
		if variable.Required && strings.TrimSpace(value) == "" {
			missing = append(missing, variable.Label)
		}
	}
	if len(missing) > 0 {
		return "", fmt.Errorf("missing required values: %s", strings.Join(missing, ", "))
	}

	var renderErr error
	result := placeholder.ReplaceAllStringFunc(entry.Template, func(match string) string {
		parts := placeholder.FindStringSubmatch(match)
		name := parts[1]
		variable, ok := allowed[name]
		if !ok {
			renderErr = fmt.Errorf("unknown template variable %q", name)
			return match
		}
		return resolvedValue(variable, values[name], now)
	})
	if renderErr != nil {
		return "", renderErr
	}
	if placeholder.MatchString(result) {
		return "", errors.New("template contains unresolved variables")
	}
	return result, nil
}

func resolvedValue(variable model.Variable, supplied string, now time.Time) string {
	if strings.TrimSpace(supplied) != "" {
		return supplied
	}
	if variable.Type == "date" {
		format := variable.Format
		if format == "" {
			format = "2006-01-02"
		}
		return now.Format(format)
	}
	return variable.Default
}

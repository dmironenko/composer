package gitops

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"

	"github.com/dmironenko/composer/internal/app/composer/domain"

	"go.yaml.in/yaml/v3"

	"github.com/dmironenko/composer/internal/app/composer/adapters/gitops/internal/vars"
)

//go:embed compose.yaml.tmpl
var composeTemplate string

// DefaultTemplate returns the exact built-in template for repository overrides.
func DefaultTemplate() string { return composeTemplate }

type templateData struct {
	Installation string
	Environments []string
	Env          string
	Namespace    string
	App          string
	Vars         map[string]any
	Secrets      map[string]any
	Runtime      runtimeData
	networks     map[string]bool
}

type runtimeData struct{ ProjectName, NetworkName, Image string }

func functions(data templateData) template.FuncMap {
	return template.FuncMap{
		"network": data.network,
		"toJSON": func(value any) (string, error) {
			result, err := json.Marshal(value)

			return string(result), err
		},
		"toYAML": func(value any) (string, error) {
			result, err := yaml.Marshal(value)

			return strings.TrimSuffix(string(result), "\n"), err
		},
		"quote": func(value any) (string, error) {
			result, err := json.Marshal(fmt.Sprint(value))

			return string(result), err
		},
		"indent": indent,
		"get":    func(values map[string]any, key string) any { return values[key] },
		"default": func(fallback, value any) any {
			if value == nil {
				return fallback
			}

			return value
		},
		"dict":  dictionary,
		"list":  func(values ...any) []any { return values },
		"merge": vars.Merge,
	}
}

func execute(name, source string, data templateData) ([]byte, error) {
	tmpl, err := template.New(name).Option("missingkey=error").Funcs(functions(data)).Parse(source)
	if err != nil {
		return nil, fmt.Errorf("%w: parse template %s", domain.ErrInvalid, name)
	}

	var output bytes.Buffer
	if err := tmpl.Execute(&output, data); err != nil {
		// text/template errors can include values passed to a helper. Do not
		// expose them: templates receive plaintext secrets after SOPS decryption.
		return nil, fmt.Errorf(
			"%w: execute template %s (check required keys and helper argument types)", domain.ErrInvalid, name,
		)
	}

	return output.Bytes(), nil
}

func dictionary(pairs ...any) (map[string]any, error) {
	if len(pairs)%2 != 0 {
		return nil, fmt.Errorf("%w: dict requires key/value pairs", domain.ErrInvalid)
	}

	result := make(map[string]any, len(pairs)/2)
	for index := 0; index < len(pairs); index += 2 {
		key, ok := pairs[index].(string)
		if !ok {
			return nil, fmt.Errorf("%w: dict keys must be strings", domain.ErrInvalid)
		}

		result[key] = pairs[index+1]
	}

	return result, nil
}

func indent(width int, value string) (string, error) {
	if width < 0 || width > 128 {
		return "", fmt.Errorf("%w: invalid indent width", domain.ErrInvalid)
	}

	prefix := strings.Repeat(" ", width)

	return prefix + strings.ReplaceAll(value, "\n", "\n"+prefix), nil
}

func (data templateData) network(environment, namespace string) string {
	name := identity(data.Installation, environment, namespace)
	data.networks[name] = true

	return name
}

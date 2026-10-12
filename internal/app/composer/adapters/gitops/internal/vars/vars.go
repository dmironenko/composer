// Package vars decodes arbitrary app values and applies environment overrides.
package vars

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"

	"go.yaml.in/yaml/v3"
)

var (
	ErrInvalid   = errors.New("invalid vars yaml")
	ErrEncrypted = errors.New("cannot decrypt secrets: check age_key_file and value integrity")
)

// Read reports absence separately from an empty document. Only absence disables
// a stage domain. Other failures must never be interpreted as deletion.
func Read(fsys fs.FS, path, ageKeyFile string) (map[string]any, bool, error) {
	data, err := fs.ReadFile(fsys, path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}

	if err != nil {
		return nil, true, fmt.Errorf("read %s: %w", path, err)
	}

	values, err := Decode(data)
	if err == nil {
		err = decryptValues(values, ageKeyFile)
	}

	if err != nil {
		return nil, true, fmt.Errorf("%s: %w", path, err)
	}

	return values, true, nil
}

func Decode(data []byte) (map[string]any, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))

	var values map[string]any
	if err := decoder.Decode(&values); err != nil && !errors.Is(err, io.EOF) {
		return nil, ErrInvalid
	}

	var extra yaml.Node
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, ErrInvalid
	}

	if values == nil {
		values = map[string]any{}
	}

	if _, encrypted := values["sops"]; encrypted {
		return nil, fmt.Errorf("%w: SOPS files are no longer supported; migrate to encrypted secrets", ErrInvalid)
	}

	if !stringKeys(values) {
		return nil, ErrInvalid
	}

	return values, nil
}

// Merge owns the returned tree; neither input is modified. Null deletes a key,
// maps merge recursively and lists/scalars replace, including false/zero/empty.
func Merge(base, override map[string]any) map[string]any {
	result := make(map[string]any, len(base)+len(override))
	for key, value := range base {
		result[key] = clone(value)
	}

	for key, value := range override {
		if value == nil {
			delete(result, key)

			continue
		}

		if next, ok := value.(map[string]any); ok {
			previous, _ := result[key].(map[string]any)
			result[key] = Merge(previous, next)

			continue
		}

		result[key] = clone(value)
	}

	return result
}

func clone(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return Merge(value, nil)
	case []any:
		result := make([]any, len(value))
		for index, child := range value {
			result[index] = clone(child)
		}

		return result
	default:
		return value
	}
}

func stringKeys(value any) bool {
	switch value := value.(type) {
	case map[string]any:
		return stringMapKeys(value)
	case map[any]any:
		return false
	case []any:
		for _, child := range value {
			if !stringKeys(child) {
				return false
			}
		}
	}

	return true
}

func stringMapKeys(values map[string]any) bool {
	for _, child := range values {
		if !stringKeys(child) {
			return false
		}
	}

	return true
}

func decryptValues(values map[string]any, keyFile string) error {
	for key, value := range values {
		if key == "secrets" {
			continue
		}

		decrypted, err := decryptTree(value, keyFile)
		if err != nil {
			return err
		}

		values[key] = decrypted
	}

	return decryptLegacySecrets(values, keyFile)
}

func decryptLegacySecrets(values map[string]any, keyFile string) error {
	switch value := values["secrets"].(type) {
	case nil:
		return nil
	case map[string]any:
		return decryptMap(value, keyFile)
	case string:
		// read the earlier whole-block format so existing commits can still be reverted.
		plaintext, err := Decrypt([]byte(value), keyFile)
		if err != nil {
			return err
		}

		secrets, err := Decode(plaintext)
		if err != nil {
			return ErrEncrypted
		}

		values["secrets"] = secrets

		return nil
	default:
		return ErrEncrypted
	}
}

// decryptTree walks vars; decrypted strings are never parsed or visited again.
func decryptTree(value any, keyFile string) (any, error) {
	switch value := value.(type) {
	case map[string]any:
		return value, decryptMap(value, keyFile)
	case []any:
		for index, child := range value {
			decrypted, err := decryptTree(child, keyFile)
			if err != nil {
				return nil, err
			}

			value[index] = decrypted
		}
	case string:
		return decryptString(value, keyFile)
	}

	return value, nil
}

func decryptMap(values map[string]any, keyFile string) error {
	for key, child := range values {
		decrypted, err := decryptTree(child, keyFile)
		if err != nil {
			return err
		}

		values[key] = decrypted
	}

	return nil
}

package vars_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/dmironenko/composer/internal/app/composer/adapters/gitops/internal/vars"
)

//nolint:maintidx // Keep malformed YAML cases and value assertions in one table.
func TestDecode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, input string
		expected    map[string]any
		expectedErr error
	}{
		{name: "empty", input: "", expected: map[string]any{}},
		{
			name:     "false and zero",
			input:    "enabled: false\nreplicas: 0",
			expected: map[string]any{"enabled": false, "replicas": 0},
		},
		{name: "duplicate nested key", input: "a:\n  b: 1\n  b: 2", expectedErr: vars.ErrInvalid},
		{name: "list root", input: "[one, two]", expectedErr: vars.ErrInvalid},
		{name: "invalid syntax", input: "a: [", expectedErr: vars.ErrInvalid},
		{name: "extra document", input: "a: 1\n---\na: 2", expectedErr: vars.ErrInvalid},
		{name: "encrypted", input: "sops: {}\nsecret: encrypted", expectedErr: vars.ErrEncrypted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			actual, err := vars.Decode([]byte(tc.input))
			if !errors.Is(err, tc.expectedErr) {
				t.Fatalf("decode: actual=%v expected=%v", err, tc.expectedErr)
			}

			if !reflect.DeepEqual(actual, tc.expected) {
				t.Fatalf("values: actual=%#v expected=%#v", actual, tc.expected)
			}
		})
	}
}

//nolint:maintidx // Keep the merge cases and immutability assertions in one table.
func TestMerge(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, base, override, expected string }{
		{name: "recursive", base: "a: {one: 1, two: 2}", override: "a: {two: 3}", expected: "a: {one: 1, two: 3}"},
		{name: "list replaces", base: "a: [1, 2]", override: "a: [3]", expected: "a: [3]"},
		{name: "null deletes", base: "a: {one: 1, two: 2}", override: "a: {two: null}", expected: "a: {one: 1}"},
		{
			name:     "zero values replace",
			base:     "a: true\nb: 2\nc: text\nd: [1]",
			override: "a: false\nb: 0\nc: ''\nd: []",
			expected: "a: false\nb: 0\nc: ''\nd: []",
		},
		{
			name:     "empty build retained",
			base:     "compose: {services: {app: {build: {}}}}",
			override: "{}",
			expected: "compose: {services: {app: {build: {}}}}",
		},
		{
			name:     "nested base nulls retained",
			base:     "a: {keep: null, list: [{value: null}]}",
			override: "{}",
			expected: "a: {keep: null, list: [{value: null}]}",
		},
		{name: "mapping replaces scalar", base: "a: false", override: "a: {b: 1}", expected: "a: {b: 1}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			base := decode(t, tc.base)
			override := decode(t, tc.override)

			actual := vars.Merge(base, override)
			if expected := decode(t, tc.expected); !reflect.DeepEqual(actual, expected) {
				t.Fatalf("merge: actual=%#v expected=%#v", actual, expected)
			}

			if !reflect.DeepEqual(base, decode(t, tc.base)) || !reflect.DeepEqual(override, decode(t, tc.override)) {
				t.Fatal("merge mutated input")
			}
		})
	}
}

func decode(t *testing.T, input string) map[string]any {
	t.Helper()

	values, err := vars.Decode([]byte(input))
	if err != nil {
		t.Fatal(err)
	}

	return values
}

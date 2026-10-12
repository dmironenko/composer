package vars_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dmironenko/composer/internal/app/composer/adapters/gitops/internal/vars"
)

//nolint:gocognit,gocyclo,cyclop,maintidx // Keep each table case and its related assertions together.
func TestReadSecrets(t *testing.T) {
	cases := []struct {
		name, source, key string
		expectedErr       error
	}{
		{name: "encrypted subtree", source: "base.yaml", key: "age-key.txt"},
		{name: "no configured key", source: "base.yaml", expectedErr: vars.ErrEncrypted},
		{name: "wrong key", source: "base.yaml", key: "other-key.txt", expectedErr: vars.ErrEncrypted},
		{name: "unreadable key", source: "base.yaml", key: "missing.txt", expectedErr: vars.ErrEncrypted},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PATH", t.TempDir())

			data, err := os.ReadFile(filepath.Join("testdata", tc.source))
			if err != nil {
				t.Fatal(err)
			}

			root := t.TempDir()
			if err = os.WriteFile(filepath.Join(root, "base.yaml"), data, 0o600); err != nil {
				t.Fatal(err)
			}

			keyFile := ""
			if tc.key != "" {
				keyFile = filepath.Join("testdata", tc.key)
			}

			actual, exists, err := vars.Read(os.DirFS(root), "base.yaml", keyFile)
			if !exists || !errors.Is(err, tc.expectedErr) {
				t.Fatalf("read: exists=%v error=%v expected=%v", exists, err, tc.expectedErr)
			}

			if tc.expectedErr != nil {
				if actual != nil {
					t.Fatal("decryption failure returned values")
				}

				return
			}

			expected := decode(t, `
database: {host: db, password: base-test-secret}
logging: {level: info}
retries: 3
debug: false
`)
			if !reflect.DeepEqual(actual["app"], expected) {
				t.Fatal("decryption changed app values or YAML types")
			}

			original, err := os.ReadFile(filepath.Join(root, "base.yaml"))
			if err != nil || string(original) != string(data) {
				t.Fatal("encrypted source was modified")
			}
		})
	}
}

//nolint:gocognit,gocyclo,cyclop,maintidx // Keep each failure case and its disclosure checks together.
func TestReadFailuresDoNotExposeSecrets(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, key, document string
		expectedErr         error
	}{
		{name: "invalid identity", key: "private-key-marker", expectedErr: vars.ErrEncrypted},
		{name: "empty identity", key: "# no identities\n", expectedErr: vars.ErrEncrypted},
		{
			name:        "invalid encrypted value",
			document:    "app: {password: 'ENC[private-value-marker]'}\n",
			expectedErr: vars.ErrEncrypted,
		},
		{name: "malformed yaml", document: "app: [private-value-marker", expectedErr: vars.ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()

			data, err := os.ReadFile("testdata/base.yaml")
			if err != nil {
				t.Fatal(err)
			}

			if tc.document != "" {
				data = []byte(tc.document)
			}

			key, err := os.ReadFile("testdata/age-key.txt")
			if err != nil {
				t.Fatal(err)
			}

			if tc.key != "" {
				key = []byte(tc.key)
			}

			if err = os.WriteFile(filepath.Join(root, "key"), key, 0o600); err != nil {
				t.Fatal(err)
			}

			if err = os.WriteFile(filepath.Join(root, "vars.yaml"), data, 0o600); err != nil {
				t.Fatal(err)
			}

			_, exists, err := vars.Read(os.DirFS(root), "vars.yaml", filepath.Join(root, "key"))
			if !exists || !errors.Is(err, tc.expectedErr) {
				t.Fatalf("actual=%v expected=%v", err, tc.expectedErr)
			}

			if strings.Contains(err.Error(), "private-") {
				t.Fatal("error exposes input")
			}
		})
	}
}

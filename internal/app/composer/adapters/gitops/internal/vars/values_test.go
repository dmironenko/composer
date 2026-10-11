package vars_test

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/dmironenko/composer/internal/app/composer/adapters/gitops/internal/vars"
)

func TestReadEncryptedValues(t *testing.T) {
	t.Parallel()

	cases := []string{"some-token", "true", "123", "null", "", "age:literal", "ENC[literal]", " x\ny\n\n"}
	for _, plaintext := range cases {
		t.Run(plaintext, func(t *testing.T) {
			t.Parallel()
			checkEncryptedValue(t, plaintext)
		})
	}
}

func checkEncryptedValue(t *testing.T, plaintext string) {
	t.Helper()

	const keyFile = "testdata/age-key.txt"

	ciphertext, err := vars.Encrypt([]byte(plaintext), keyFile)
	if err != nil {
		t.Fatal(err)
	}

	encoded := strconv.Quote(string(ciphertext))
	legacy := "age:" + strings.TrimSuffix(strings.TrimPrefix(string(ciphertext), "ENC["), "]")

	document := "app: {literal: 'ENC[not-encrypted]'}\nsecrets:\n  token: " + encoded +
		"\n  legacy: " + legacy + "\n  database:\n    password: " + encoded + "\n  list: [" + encoded +
		", {nested: " + encoded + "}]\n  plain: readable\n  flag: false\n  absent: null\n"
	files := fstest.MapFS{"base.yaml": {Data: []byte(document)}}

	actual, _, err := vars.Read(files, "base.yaml", keyFile)
	if err != nil {
		t.Fatalf("read encrypted values: %v", err)
	}

	expected := decryptedValues(plaintext)
	if !reflect.DeepEqual(actual, expected) {
		t.Fatal("decryption changed secret bytes, types, or unrelated vars")
	}
}

func decryptedValues(plaintext string) map[string]any {
	return map[string]any{
		"app": map[string]any{"literal": "ENC[not-encrypted]"},
		"secrets": map[string]any{
			"token": plaintext, "legacy": plaintext, "database": map[string]any{"password": plaintext},
			"list":  []any{plaintext, map[string]any{"nested": plaintext}},
			"plain": "readable", "flag": false, "absent": nil,
		},
	}
}

func TestReadLegacyBlock(t *testing.T) {
	t.Parallel()

	const keyFile = "testdata/age-key.txt"

	ciphertext, err := vars.Encrypt([]byte("stage_token: demo\n"), keyFile)
	if err != nil {
		t.Fatal(err)
	}

	legacy := "age:" + strings.TrimSuffix(strings.TrimPrefix(string(ciphertext), "ENC["), "]")
	files := fstest.MapFS{"base.yaml": {Data: []byte("secrets: " + legacy)}}

	actual, exists, err := vars.Read(files, "base.yaml", keyFile)
	if err != nil || !exists || !reflect.DeepEqual(actual, decode(t, "secrets: {stage_token: demo}")) {
		t.Fatalf("legacy block is no longer readable: %v", err)
	}
}

func TestReadNestedDecryptionFailure(t *testing.T) {
	t.Parallel()

	for _, document := range []string{
		"secrets: {database: {password: 'age:invalid'}}",
		"secrets: {database: {password: 'ENC[invalid]'}}",
		"secrets: {tokens: ['ENC[invalid']}",
		"secrets: {tokens: ['age:invalid']}",
	} {
		files := fstest.MapFS{"base.yaml": {Data: []byte(document)}}

		actual, exists, err := vars.Read(files, "base.yaml", "testdata/age-key.txt")
		if !errors.Is(err, vars.ErrEncrypted) || !exists || actual != nil {
			t.Fatal("failed nested decryption must return no values")
		}
	}
}

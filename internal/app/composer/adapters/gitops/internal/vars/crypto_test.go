package vars_test

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/dmironenko/composer/internal/app/composer/adapters/gitops/internal/vars"
)

func TestEncryptRoundTrip(t *testing.T) {
	t.Parallel()

	input := []byte("database: {password: '$private-marker', retries: 3}\nenabled: false\n")

	first, err := vars.Encrypt(input, "testdata/age-key.txt")
	if err != nil {
		t.Fatal(err)
	}

	second, err := vars.Encrypt(input, "testdata/age-key.txt")
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Equal(first, second) || bytes.Contains(first, []byte("private-marker")) {
		t.Fatal("encryption must be randomized and hide plaintext")
	}

	actual, err := vars.Decrypt(first, "testdata/age-key.txt")
	if err != nil || !bytes.Equal(actual, input) {
		t.Fatalf("round trip failed: %v", err)
	}
}

func TestDecryptRejectsDamagedValues(t *testing.T) {
	t.Parallel()

	// More than one age chunk: a late authentication error must not expose early plaintext.
	input := []byte("password: " + strings.Repeat("private-marker", 10000))

	value, err := vars.Encrypt(input, "testdata/age-key.txt")
	if err != nil {
		t.Fatal(err)
	}

	truncated, tampered := damageValue(t, value)

	cases := []struct{ name, value, key string }{
		{name: "prefix", value: "private-marker", key: "age-key.txt"},
		{name: "base64", value: "ENC[!private-marker]", key: "age-key.txt"},
		{name: "missing closing bracket", value: strings.TrimSuffix(string(value), "]"), key: "age-key.txt"},
		{name: "wrong key", value: string(value), key: "other-key.txt"},
		{name: "truncated", value: truncated, key: "age-key.txt"},
		{name: "tampered", value: tampered, key: "age-key.txt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			actual, decryptErr := vars.Decrypt([]byte(tc.value), "testdata/"+tc.key)
			if !errors.Is(decryptErr, vars.ErrEncrypted) || actual != nil {
				t.Fatal("invalid ciphertext must fail without plaintext")
			}
		})
	}
}

func damageValue(t *testing.T, value []byte) (string, string) {
	t.Helper()

	ciphertext, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(strings.TrimPrefix(string(value), "ENC["), "]"))
	if err != nil {
		t.Fatal(err)
	}

	truncated := "ENC[" + base64.StdEncoding.EncodeToString(ciphertext[:len(ciphertext)-1]) + "]"
	ciphertext[len(ciphertext)-1] ^= 1
	tampered := "ENC[" + base64.StdEncoding.EncodeToString(ciphertext) + "]"

	return truncated, tampered
}

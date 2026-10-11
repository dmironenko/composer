package vars_test

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"

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
		{name: "base64", value: "age:!private-marker", key: "age-key.txt"},
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

	ciphertext, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(string(value), "age:"))
	if err != nil {
		t.Fatal(err)
	}

	truncated := "age:" + base64.StdEncoding.EncodeToString(ciphertext[:len(ciphertext)-1])
	ciphertext[len(ciphertext)-1] ^= 1
	tampered := "age:" + base64.StdEncoding.EncodeToString(ciphertext)

	return truncated, tampered
}

func TestEncryptRequiresMapping(t *testing.T) {
	t.Parallel()

	input := []byte("- private-marker\n")
	if output, err := vars.Encrypt(input, "testdata/age-key.txt"); !errors.Is(err, vars.ErrInvalid) || output != nil {
		t.Fatal("encryption must reject non-mapping input")
	}
}

func TestDecryptRequiresMapping(t *testing.T) {
	t.Parallel()

	value, keyFile := encryptRaw(t, []byte("- private-marker\n"))

	output, err := vars.Decrypt(value, keyFile)
	if !errors.Is(err, vars.ErrEncrypted) || output != nil {
		t.Fatal("decryption must reject non-mapping input")
	}
}

func encryptRaw(t *testing.T, input []byte) ([]byte, string) {
	t.Helper()

	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}

	var ciphertext bytes.Buffer

	writer, err := age.Encrypt(&ciphertext, identity.Recipient())
	if err != nil {
		t.Fatal(err)
	}

	if _, err = writer.Write(input); err != nil {
		t.Fatal(err)
	}

	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	if err = os.WriteFile(filepath.Join(root, "key"), []byte(identity.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	value := []byte("age:" + base64.StdEncoding.EncodeToString(ciphertext.Bytes()))

	return value, filepath.Join(root, "key")
}

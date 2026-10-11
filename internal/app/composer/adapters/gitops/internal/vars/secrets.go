package vars

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"strings"

	"filippo.io/age"
)

const secretPrefix = "age:"

var errKey = errors.New("cannot read age identity: check key file")

// Encrypt seals raw bytes as an age-encrypted, base64-encoded value.
func Encrypt(input []byte, keyFile string) ([]byte, error) {
	identity, err := readIdentity(keyFile)
	if err != nil {
		return nil, err
	}

	var output bytes.Buffer

	writer, err := age.Encrypt(&output, identity.Recipient())
	if err != nil {
		return nil, errKey
	}

	if _, err = writer.Write(input); err != nil {
		return nil, errKey
	}

	if err = writer.Close(); err != nil {
		return nil, errKey
	}

	return []byte(secretPrefix + base64.StdEncoding.EncodeToString(output.Bytes())), nil
}

// Decrypt authenticates the entire value before returning its original bytes.
func Decrypt(input []byte, keyFile string) ([]byte, error) {
	encoded, ok := strings.CutPrefix(strings.TrimSpace(string(input)), secretPrefix)
	if !ok {
		return nil, ErrEncrypted
	}

	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, ErrEncrypted
	}

	identity, err := readIdentity(keyFile)
	if err != nil {
		return nil, ErrEncrypted
	}

	reader, err := age.Decrypt(bytes.NewReader(ciphertext), identity)
	if err != nil {
		return nil, ErrEncrypted
	}

	plaintext, err := io.ReadAll(reader)
	if err != nil {
		return nil, ErrEncrypted
	}

	return plaintext, nil
}

func readIdentity(keyFile string) (*age.X25519Identity, error) {
	data, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, errKey
	}

	identities, err := age.ParseIdentities(bytes.NewReader(data))
	if err != nil || len(identities) != 1 {
		return nil, errKey
	}

	identity, ok := identities[0].(*age.X25519Identity)
	if !ok {
		return nil, errKey
	}

	return identity, nil
}

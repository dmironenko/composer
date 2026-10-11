package gitops

import (
	"fmt"

	"github.com/dmironenko/composer/internal/app/composer/adapters/gitops/internal/vars"
)

// EncryptSecrets encodes a YAML mapping for use as the secrets value in vars.
func EncryptSecrets(input []byte, keyFile string) ([]byte, error) {
	output, err := vars.Encrypt(input, keyFile)
	if err != nil {
		return nil, fmt.Errorf("encode secrets: %w", err)
	}

	return output, nil
}

// DecryptSecrets decodes an encrypted secrets value into a YAML mapping.
func DecryptSecrets(input []byte, keyFile string) ([]byte, error) {
	output, err := vars.Decrypt(input, keyFile)
	if err != nil {
		return nil, fmt.Errorf("decode secrets: %w", err)
	}

	return output, nil
}

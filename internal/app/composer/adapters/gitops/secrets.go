package gitops

import (
	"fmt"

	"github.com/dmironenko/composer/v2/internal/app/composer/adapters/gitops/internal/vars"
)

// EncryptSecrets encrypts one secret value for use in vars.
func EncryptSecrets(input []byte, keyFile string) ([]byte, error) {
	output, err := vars.Encrypt(input, keyFile)
	if err != nil {
		return nil, fmt.Errorf("encode secrets: %w", err)
	}

	return output, nil
}

// DecryptSecrets decodes an encrypted secrets value into its original bytes.
func DecryptSecrets(input []byte, keyFile string) ([]byte, error) {
	output, err := vars.Decrypt(input, keyFile)
	if err != nil {
		return nil, fmt.Errorf("decode secrets: %w", err)
	}

	return output, nil
}

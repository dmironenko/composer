package main

import (
	"fmt"

	"github.com/dmironenko/composer/v2/internal/app/composer/adapters/gitops"
)

func (bootstrap) EncryptSecrets(input []byte, keyFile string) ([]byte, error) {
	output, err := gitops.EncryptSecrets(input, keyFile)
	if err != nil {
		return nil, fmt.Errorf("encrypt block: %w", err)
	}

	return output, nil
}

func (bootstrap) DecryptSecrets(input []byte, keyFile string) ([]byte, error) {
	output, err := gitops.DecryptSecrets(input, keyFile)
	if err != nil {
		return nil, fmt.Errorf("decrypt block: %w", err)
	}

	return output, nil
}

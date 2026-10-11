package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

var errSecretKey = errors.New("--key-file is required")

func secretsCommand(bootstrap Bootstrap) *cobra.Command {
	cmd := &cobra.Command{Use: "secrets", Short: "Encrypt or decrypt a secrets block through stdin/stdout"}
	cmd.AddCommand(
		secretCommand("encrypt", "Read a YAML mapping and print an encrypted value", bootstrap.EncryptSecrets),
		secretCommand("decrypt", "Read an encrypted value and print its YAML mapping", bootstrap.DecryptSecrets),
	)

	return cmd
}

func secretCommand(action, description string, transform func([]byte, string) ([]byte, error)) *cobra.Command {
	var keyFile string

	cmd := &cobra.Command{
		Use: action, Short: description, Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if keyFile == "" {
				return errSecretKey
			}

			input, err := io.ReadAll(cmd.InOrStdin())
			if err != nil {
				return fmt.Errorf("read secrets: %w", err)
			}

			output, err := transform(input, keyFile)
			if err != nil {
				return fmt.Errorf("%s secrets: %w", action, err)
			}

			if _, err = fmt.Fprintln(cmd.OutOrStdout(), strings.TrimSuffix(string(output), "\n")); err != nil {
				return fmt.Errorf("write secrets: %w", err)
			}

			return nil
		},
	}
	cmd.Flags().StringVar(&keyFile, "key-file", "", "Path to one native age identity (required)")

	return cmd
}

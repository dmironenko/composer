package vars

import (
	"bytes"
	"io"
	"os"

	"filippo.io/age"
	"github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/aes"
	sopsage "github.com/getsops/sops/v3/age"
	"github.com/getsops/sops/v3/cmd/sops/common"
	"github.com/getsops/sops/v3/config"
	"github.com/getsops/sops/v3/logging"
	sopsyaml "github.com/getsops/sops/v3/stores/yaml"
)

// decrypt accepts standard SOPS YAML with one age recipient group. Keys come
// only from the configured file; no ambient credentials, commands or plugins.
func decrypt(data []byte, keyFile string) ([]byte, error) {
	identities, err := readIdentities(keyFile)
	if err != nil {
		return nil, err
	}

	store := sopsyaml.NewStore(&config.NewStoresConfig().YAML)

	tree, err := store.LoadEncryptedFile(data)
	if err != nil || len(tree.Metadata.KeyGroups) != 1 {
		return nil, ErrEncrypted
	}

	tree.Metadata.DataKey = decryptDataKey(tree.Metadata.KeyGroups[0], identities)

	if tree.Metadata.DataKey == nil {
		return nil, ErrEncrypted
	}
	// SOPS can log file comments. Its dedicated logger must not bypass our
	// sanitized errors. SetOutput is synchronized by logrus; process env is untouched.
	logging.Loggers["SOPS"].SetOutput(io.Discard)

	_, err = common.DecryptTree(common.DecryptTreeOpts{Tree: &tree, Cipher: aes.NewCipher()})
	if err != nil {
		return nil, ErrEncrypted
	}

	plaintext, err := store.EmitPlainFile(tree.Branches)
	if err != nil {
		return nil, ErrEncrypted
	}

	return plaintext, nil
}

func decryptDataKey(group sops.KeyGroup, identities []age.Identity) []byte {
	for _, master := range group {
		key, ok := master.(*sopsage.MasterKey)
		if !ok {
			continue
		}

		sopsage.ParsedIdentities(identities).ApplyToMasterKey(key)

		if plaintext, err := key.Decrypt(); err == nil {
			return plaintext
		}
	}

	return nil
}

func readIdentities(keyFile string) ([]age.Identity, error) {
	keyBytes, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, ErrEncrypted
	}

	identities, err := age.ParseIdentities(bytes.NewReader(keyBytes))
	if err != nil || len(identities) == 0 {
		return nil, ErrEncrypted
	}

	return identities, nil
}

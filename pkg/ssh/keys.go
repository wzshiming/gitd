package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"

	gossh "golang.org/x/crypto/ssh"
)

// Signer is an alias for ssh.Signer to avoid requiring callers to import golang.org/x/crypto/ssh.
type Signer = gossh.Signer

// PublicKey is an alias for ssh.PublicKey to avoid requiring callers to import golang.org/x/crypto/ssh.
type PublicKey = gossh.PublicKey

// KeyType specifies the type of SSH host key to generate.
type KeyType string

const (
	// KeyTypeEd25519 generates an Ed25519 host key.
	KeyTypeEd25519 KeyType = "ed25519"
	// KeyTypeRSA generates an RSA host key (4096-bit).
	KeyTypeRSA KeyType = "rsa"
)

// ParseAuthorizedKeys parses an OpenSSH authorized_keys file and returns
// the parsed public keys. Lines that are empty or start with '#' are skipped.
func ParseAuthorizedKeys(data []byte) ([]gossh.PublicKey, error) {
	var keys []gossh.PublicKey
	rest := data
	for len(rest) > 0 {
		var key gossh.PublicKey
		var err error
		key, _, _, rest, err = gossh.ParseAuthorizedKey(rest)
		if err != nil {
			return nil, fmt.Errorf("parsing authorized key: %w", err)
		}
		keys = append(keys, key)
	}
	return keys, nil
}

// ParseHostKeyFile reads a PEM-encoded private key and returns an SSH signer.
func ParseHostKeyFile(data []byte) (gossh.Signer, error) {
	signer, err := gossh.ParsePrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("parsing host key: %w", err)
	}
	return signer, nil
}

// GenerateAndSaveHostKey generates an SSH host key of the specified type and
// saves it to path with 0600 permissions, then returns a signer for the key.
func GenerateAndSaveHostKey(path string, keyType KeyType) (gossh.Signer, error) {
	var privateKey interface{}
	var err error

	switch keyType {
	case KeyTypeRSA:
		privateKey, err = rsa.GenerateKey(rand.Reader, 4096)
		if err != nil {
			return nil, fmt.Errorf("generating RSA host key: %w", err)
		}
	default:
		// Default to Ed25519.
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("generating Ed25519 host key: %w", err)
		}
		privateKey = priv
	}

	derBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("marshaling host key: %w", err)
	}

	pemBlock := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: derBytes,
	}

	if err := os.WriteFile(path, pem.EncodeToMemory(pemBlock), 0600); err != nil {
		return nil, fmt.Errorf("writing host key to %s: %w", path, err)
	}

	return gossh.NewSignerFromKey(privateKey)
}

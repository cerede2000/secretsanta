package maildkim

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"os"

	"secretsanta/internal/config"
	"secretsanta/internal/shared/errs"
)

// Causes stables, exposées pour errors.Is. Leur texte ne contient jamais
// ni le chemin ni le contenu de la clé.
var (
	ErrKeyUnreadable        = errors.New("maildkim: fichier de clé illisible")
	ErrKeyInvalid           = errors.New("maildkim: clé privée invalide")
	ErrKeyAlgorithmMismatch = errors.New("maildkim: type de clé incompatible avec l'algorithme")
	ErrUnknownAlgorithm     = errors.New("maildkim: algorithme inconnu")
)

// maxKeyFileSize borne la lecture : une clé PEM tient largement en 64 Kio.
const maxKeyFileSize = 64 << 10

// minRSABits est le minimum imposé aux signataires par la RFC 8301.
const minRSABits = 1024

// LoadKey lit la clé privée depuis le fichier dont le chemin est donné et
// vérifie qu'elle convient à l'algorithme (rsa-sha256 ou ed25519-sha256).
//
// Les erreurs renvoyées ne contiennent ni le chemin ni le contenu du fichier,
// et ne transportent aucune cause venue du système ou du décodeur.
func LoadKey(path config.Secret, algorithm string) (crypto.Signer, error) {
	if algorithm != config.AlgRSA && algorithm != config.AlgEd25519 {
		return nil, keyErr("Algorithme DKIM inconnu : rsa-sha256 ou ed25519-sha256 attendu.", ErrUnknownAlgorithm)
	}
	if !path.IsSet() {
		return nil, keyErr("Clé privée DKIM : aucun chemin de fichier n'est configuré.", ErrKeyUnreadable)
	}
	data, err := readBounded(path.Reveal())
	defer wipe(data)
	if err != nil {
		return nil, keyErr("Clé privée DKIM : le fichier est introuvable ou illisible.", ErrKeyUnreadable)
	}
	signer, err := parseKey(data)
	if err != nil {
		return nil, keyErr("Clé privée DKIM : le fichier ne contient pas une clé privée PEM valide.", ErrKeyInvalid)
	}
	switch k := signer.(type) {
	case *rsa.PrivateKey:
		if algorithm != config.AlgRSA {
			return nil, keyErr("Clé privée DKIM : une clé RSA ne convient pas à l'algorithme ed25519-sha256.", ErrKeyAlgorithmMismatch)
		}
		if k.N.BitLen() < minRSABits {
			return nil, keyErr("Clé privée DKIM : une clé RSA d'au moins 1024 bits est exigée (2048 recommandés).", ErrKeyInvalid)
		}
	case ed25519.PrivateKey:
		if algorithm != config.AlgEd25519 {
			return nil, keyErr("Clé privée DKIM : une clé Ed25519 ne convient pas à l'algorithme rsa-sha256.", ErrKeyAlgorithmMismatch)
		}
	default:
		return nil, keyErr("Clé privée DKIM : seules les clés RSA et Ed25519 sont acceptées.", ErrKeyAlgorithmMismatch)
	}
	return signer, nil
}

func keyErr(msg string, cause error) error {
	return errs.Wrap(errs.KindInternal, msg, cause)
}

func readBounded(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, errors.New("pas un fichier ordinaire")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxKeyFileSize+1))
	if err != nil {
		return data, err
	}
	if len(data) > maxKeyFileSize {
		return data, errors.New("fichier trop grand")
	}
	return data, nil
}

// parseKey décode le premier bloc PEM de clé privée (PKCS#8 ou PKCS#1).
// Un bloc chiffré, un autre type de bloc ou un contenu illisible est refusé.
func parseKey(data []byte) (crypto.Signer, error) {
	rest := data
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			return nil, ErrKeyInvalid
		}
		switch block.Type {
		case "PRIVATE KEY":
			defer wipe(block.Bytes)
			if _, enc := block.Headers["Proc-Type"]; enc {
				return nil, ErrKeyInvalid
			}
			k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
			if err != nil {
				return nil, ErrKeyInvalid
			}
			s, ok := k.(crypto.Signer)
			if !ok {
				return nil, ErrKeyInvalid
			}
			return s, nil
		case "RSA PRIVATE KEY":
			defer wipe(block.Bytes)
			if _, enc := block.Headers["Proc-Type"]; enc {
				return nil, ErrKeyInvalid
			}
			k, err := x509.ParsePKCS1PrivateKey(block.Bytes)
			if err != nil {
				return nil, ErrKeyInvalid
			}
			return k, nil
		}
		// Autre bloc (certificat, paramètres) : on passe au suivant.
	}
}

// wipe efface au mieux une copie en mémoire du contenu de la clé.
func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

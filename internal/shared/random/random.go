// Package random fournit de l'aléa cryptographique.
package random

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"math/big"
)

// TokenBytes est la taille d'un jeton : 128 bits.
const TokenBytes = 16

// Bytes renvoie n octets aléatoires.
func Bytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

// Token renvoie un jeton de 128 bits encodé en base64 URL sans remplissage
// (22 caractères).
func Token() (string, error) {
	b, err := Bytes(TokenBytes)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Int renvoie un entier uniforme dans [0, n).
func Int(n int64) (int64, error) {
	if n <= 0 {
		return 0, errors.New("random: borne invalide")
	}
	v, err := rand.Int(rand.Reader, big.NewInt(n))
	if err != nil {
		return 0, err
	}
	return v.Int64(), nil
}

// Shuffle mélange uniformément n éléments (Fisher-Yates) via swap.
func Shuffle(n int, swap func(i, j int)) error {
	for i := n - 1; i > 0; i-- {
		j, err := Int(int64(i + 1))
		if err != nil {
			return err
		}
		swap(i, int(j))
	}
	return nil
}

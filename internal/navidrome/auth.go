package navidrome

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"math/big"
	"net/url"
)

const charset = "abcdefghijklmnopqrstuvwxyz0123456789"

// generateSalt creates a random 6-character alphanumeric string.
func generateSalt() string {
	b := make([]byte, 6)
	for i := range b {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			// Fallback if random reader fails
			b[i] = charset[i%len(charset)]
		} else {
			b[i] = charset[num.Int64()]
		}
	}
	return string(b)
}

// BuildAuthParams constructs the query parameters required for Subsonic API authentication.
func BuildAuthParams(username, password, clientName, apiVersion string) url.Values {
	salt := generateSalt()

	hasher := md5.New()
	hasher.Write([]byte(password + salt))
	token := hex.EncodeToString(hasher.Sum(nil))

	params := url.Values{}
	params.Set("u", username)
	params.Set("t", token)
	params.Set("s", salt)
	params.Set("v", apiVersion)
	params.Set("c", clientName)
	params.Set("f", "json")

	return params
}

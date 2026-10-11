package identity

import (
	"crypto/rand"
	"math/big"
	"strings"
)

// A user code is the short secret a person carries between two screens: from a
// chat to the Portal when linking a chat account, and from a CLI or Desktop to
// the Portal when confirming a native sign-in. Both flows read, type, and
// compare it the same way, so they share this one format.

// userCodeAlphabet leaves out characters people confuse when retyping a code
// (0/O, 1/I/L), so a code read off a phone screen survives being typed.
const userCodeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// UserCodeLength gives about 40 bits: far beyond guessing within a code's
// ten-minute life, and still short enough to type.
const UserCodeLength = 8

// NewUserCode draws a code from the alphabet with a cryptographic source. It
// returns the bare form; DisplayUserCode adds the separator people see.
func NewUserCode() (string, error) {
	size := big.NewInt(int64(len(userCodeAlphabet)))
	var b strings.Builder
	for range UserCodeLength {
		n, err := rand.Int(rand.Reader, size)
		if err != nil {
			return "", err
		}
		b.WriteByte(userCodeAlphabet[n.Int64()])
	}
	return b.String(), nil
}

// NormalizeUserCode accepts a code the way people retype one: any case, with
// or without the separator it was shown with. Stores hash this form.
func NormalizeUserCode(code string) string {
	code = strings.ToUpper(code)
	return strings.NewReplacer("-", "", " ", "").Replace(code)
}

// DisplayUserCode is the XXXX-XXXX form a person reads.
func DisplayUserCode(code string) string {
	if len(code) != UserCodeLength {
		return code
	}
	return code[:4] + "-" + code[4:]
}

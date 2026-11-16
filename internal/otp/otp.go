// Package otp makes the one-time codes of RFC 4226 (HOTP) and RFC 6238 (TOTP)
// from the secret an authenticator app would be given: a bare base32 seed, or
// an otpauth:// address as a QR code carries.
package otp

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Kind is how the next code is chosen.
type Kind uint8

const (
	// TOTP codes follow the clock, a new one every period.
	TOTP Kind = iota
	// HOTP codes follow a counter that rises with every code used.
	HOTP
)

// Secret is what is needed to make codes. Its zero value is not usable; read one
// with Parse.
type Secret struct {
	Kind      Kind
	Key       []byte
	Digits    int    // 6 to 8
	Period    int    // seconds, TOTP only
	Algorithm string // "SHA1", "SHA256" or "SHA512"
	Counter   uint64 // the next HOTP counter
	Issuer    string
	Account   string
}

// Errors Parse returns.
var (
	ErrEmpty       = errors.New("otp: nothing to read")
	ErrSecret      = errors.New("otp: the secret is not valid base32")
	ErrScheme      = errors.New("otp: not an otpauth://totp or otpauth://hotp address")
	ErrParameter   = errors.New("otp: a parameter is out of range")
	ErrNoSecretKey = errors.New("otp: the address has no secret")
)

const (
	defaultDigits = 6
	defaultPeriod = 30
	// A key shorter than 10 bytes is below what RFC 4226 allows; one longer than
	// 1 KiB is not a seed.
	minKeyBytes = 10
	maxKeyBytes = 1024
)

// Parse reads a bare base32 seed (blanks and case ignored) or an otpauth:// address.
func Parse(input string) (Secret, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return Secret{}, ErrEmpty
	}
	if !strings.HasPrefix(strings.ToLower(input), "otpauth:") {
		key, err := decodeKey(input)
		if err != nil {
			return Secret{}, err
		}
		return Secret{Kind: TOTP, Key: key, Digits: defaultDigits, Period: defaultPeriod, Algorithm: "SHA1"}, nil
	}
	address, err := url.Parse(input)
	if err != nil || !strings.EqualFold(address.Scheme, "otpauth") {
		return Secret{}, ErrScheme
	}
	secret := Secret{Digits: defaultDigits, Period: defaultPeriod, Algorithm: "SHA1"}
	switch strings.ToLower(address.Host) {
	case "totp":
		secret.Kind = TOTP
	case "hotp":
		secret.Kind = HOTP
	default:
		return Secret{}, ErrScheme
	}
	query := address.Query()
	if query.Get("secret") == "" {
		return Secret{}, ErrNoSecretKey
	}
	if secret.Key, err = decodeKey(query.Get("secret")); err != nil {
		return Secret{}, err
	}
	label := strings.TrimPrefix(address.Path, "/")
	if unescaped, err := url.PathUnescape(label); err == nil {
		label = unescaped
	}
	if issuer, account, found := strings.Cut(label, ":"); found {
		secret.Issuer, secret.Account = strings.TrimSpace(issuer), strings.TrimSpace(account)
	} else {
		secret.Account = strings.TrimSpace(label)
	}
	if issuer := query.Get("issuer"); issuer != "" {
		secret.Issuer = issuer
	}
	if value := query.Get("digits"); value != "" {
		if secret.Digits, err = strconv.Atoi(value); err != nil || secret.Digits < 6 || secret.Digits > 8 {
			return Secret{}, ErrParameter
		}
	}
	if value := query.Get("period"); value != "" {
		if secret.Period, err = strconv.Atoi(value); err != nil || secret.Period < 1 || secret.Period > 3600 {
			return Secret{}, ErrParameter
		}
	}
	if value := query.Get("algorithm"); value != "" {
		switch strings.ToUpper(value) {
		case "SHA1", "SHA256", "SHA512":
			secret.Algorithm = strings.ToUpper(value)
		default:
			return Secret{}, ErrParameter
		}
	}
	if secret.Kind == HOTP {
		if value := query.Get("counter"); value != "" {
			if secret.Counter, err = strconv.ParseUint(value, 10, 64); err != nil {
				return Secret{}, ErrParameter
			}
		}
	}
	return secret, nil
}

func decodeKey(text string) ([]byte, error) {
	text = strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '\t' || r == '\n' || r == '=' {
			return -1
		}
		return r
	}, strings.ToUpper(text))
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(text)
	if err != nil {
		return nil, ErrSecret
	}
	if len(key) < minKeyBytes || len(key) > maxKeyBytes {
		return nil, ErrSecret
	}
	return key, nil
}

func (secret Secret) hash() func() hash.Hash {
	switch secret.Algorithm {
	case "SHA256":
		return sha256.New
	case "SHA512":
		return sha512.New
	}
	return sha1.New
}

// hotp is RFC 4226's code for one counter value.
func (secret Secret) hotp(counter uint64) string {
	var message [8]byte
	binary.BigEndian.PutUint64(message[:], counter)
	mac := hmac.New(secret.hash(), secret.Key)
	mac.Write(message[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	truncated := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	modulus := uint32(1)
	for range secret.Digits {
		modulus *= 10
	}
	return fmt.Sprintf("%0*d", secret.Digits, truncated%modulus)
}

// Code is the code to use now. For HOTP it is the one for the current counter;
// call Next to move on once it has been used.
func (secret Secret) Code(now time.Time) string {
	if secret.Kind == HOTP {
		return secret.hotp(secret.Counter)
	}
	return secret.hotp(uint64(now.Unix()) / uint64(secret.Period))
}

// Next is the secret after one HOTP code was used; a TOTP secret is unchanged.
func (secret Secret) Next() Secret {
	if secret.Kind == HOTP {
		secret.Counter++
	}
	return secret
}

// Remaining is how long the current TOTP code stays valid.
func (secret Secret) Remaining(now time.Time) time.Duration {
	if secret.Kind != TOTP {
		return 0
	}
	period := time.Duration(secret.Period) * time.Second
	return period - time.Duration(now.UnixNano())%period
}

// String is the secret as an otpauth:// address, with the counter a HOTP secret
// has reached, so storing it keeps the place.
func (secret Secret) String() string {
	kind := "totp"
	if secret.Kind == HOTP {
		kind = "hotp"
	}
	query := url.Values{}
	query.Set("secret", base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret.Key))
	if secret.Issuer != "" {
		query.Set("issuer", secret.Issuer)
	}
	if secret.Algorithm != "SHA1" {
		query.Set("algorithm", secret.Algorithm)
	}
	if secret.Digits != defaultDigits {
		query.Set("digits", strconv.Itoa(secret.Digits))
	}
	if secret.Kind == TOTP && secret.Period != defaultPeriod {
		query.Set("period", strconv.Itoa(secret.Period))
	}
	if secret.Kind == HOTP {
		query.Set("counter", strconv.FormatUint(secret.Counter, 10))
	}
	label := url.PathEscape(secret.Account)
	if secret.Issuer != "" {
		label = url.PathEscape(secret.Issuer) + ":" + label
	}
	return "otpauth://" + kind + "/" + label + "?" + query.Encode()
}

// LooksLikeQuestion reports whether a server's prompt asks for a one-time code
// rather than a password, by its wording.
func LooksLikeQuestion(question string) bool {
	lower := strings.ToLower(question)
	for _, word := range []string{
		"verification", "one-time", "one time", "otp", "authenticator", "passcode", "token", "2fa", "mfa",
		"two-factor", "two factor", "code", "验证码", "动态", "动态口令", "一次性口令", "令牌", "校验",
		"驗證碼", "動態", "一次性密碼", "權杖", "校驗",
	} {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

package otp

import (
	"strings"
	"testing"
	"time"
)

// RFC 4226 appendix D: the key is the ASCII "12345678901234567890".
func TestHOTPMatchesRFC4226(t *testing.T) {
	secret := Secret{Kind: HOTP, Key: []byte("12345678901234567890"), Digits: 6, Algorithm: "SHA1"}
	want := []string{"755224", "287082", "359152", "969429", "338314", "254676", "287922", "162583", "399871", "520489"}
	for counter, code := range want {
		secret.Counter = uint64(counter)
		if got := secret.Code(time.Time{}); got != code {
			t.Errorf("counter %d: %s, want %s", counter, got, code)
		}
	}
}

// RFC 6238 appendix B: 8 digits, and the three hashes' own keys.
func TestTOTPMatchesRFC6238(t *testing.T) {
	keys := map[string]string{
		"SHA1":   "12345678901234567890",
		"SHA256": "12345678901234567890123456789012",
		"SHA512": "1234567890123456789012345678901234567890123456789012345678901234",
	}
	cases := []struct {
		unix      int64
		algorithm string
		code      string
	}{
		{59, "SHA1", "94287082"}, {59, "SHA256", "46119246"}, {59, "SHA512", "90693936"},
		{1111111109, "SHA1", "07081804"}, {1111111109, "SHA256", "68084774"}, {1111111109, "SHA512", "25091201"},
		{20000000000, "SHA1", "65353130"}, {20000000000, "SHA256", "77737706"}, {20000000000, "SHA512", "47863826"},
	}
	for _, test := range cases {
		secret := Secret{Kind: TOTP, Key: []byte(keys[test.algorithm]), Digits: 8, Period: 30, Algorithm: test.algorithm}
		if got := secret.Code(time.Unix(test.unix, 0)); got != test.code {
			t.Errorf("%s at %d: %s, want %s", test.algorithm, test.unix, got, test.code)
		}
	}
}

func TestParseReadsABareSeedWithBlanksAndAnyCase(t *testing.T) {
	// "12345678901234567890" in base32.
	secret, err := Parse("gezd gnbv gy3t qojq gezd gnbv gy3t qojq")
	if err != nil {
		t.Fatal(err)
	}
	if string(secret.Key) != "12345678901234567890" || secret.Kind != TOTP || secret.Digits != 6 || secret.Period != 30 {
		t.Fatalf("%+v", secret)
	}
	if code := secret.Code(time.Unix(59, 0)); code != "287082" {
		t.Fatalf("code %s", code)
	}
}

func TestParseReadsAnOtpauthAddress(t *testing.T) {
	secret, err := Parse("otpauth://totp/Example%20Co:alice@example.com?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ&issuer=Example%20Co&digits=8&period=60&algorithm=sha256")
	if err != nil {
		t.Fatal(err)
	}
	if secret.Issuer != "Example Co" || secret.Account != "alice@example.com" || secret.Digits != 8 || secret.Period != 60 || secret.Algorithm != "SHA256" {
		t.Fatalf("%+v", secret)
	}
	hotp, err := Parse("otpauth://hotp/me?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ&counter=5")
	if err != nil || hotp.Kind != HOTP || hotp.Counter != 5 {
		t.Fatalf("%+v, %v", hotp, err)
	}
	if hotp.Code(time.Now()) != "254676" || hotp.Next().Counter != 6 || hotp.Counter != 5 {
		t.Fatalf("code %s next %d", hotp.Code(time.Now()), hotp.Next().Counter)
	}
}

func TestParseRefusesWhatIsNotASecret(t *testing.T) {
	for name, input := range map[string]string{
		"empty": " ", "not base32": "hello world!", "too short": "GEZDGNBV", "wrong host": "otpauth://steam/x?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ",
		"no secret": "otpauth://totp/x", "digits": "otpauth://totp/x?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ&digits=12",
		"period": "otpauth://totp/x?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ&period=0", "algorithm": "otpauth://totp/x?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ&algorithm=md5",
		"counter": "otpauth://hotp/x?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ&counter=-1",
	} {
		if _, err := Parse(input); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestStringRoundTripsAndKeepsTheHOTPPlace(t *testing.T) {
	original, err := Parse("otpauth://hotp/Corp:bob?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ&issuer=Corp&digits=7&algorithm=SHA512&counter=41")
	if err != nil {
		t.Fatal(err)
	}
	again, err := Parse(original.Next().String())
	if err != nil {
		t.Fatal(err)
	}
	if again.Counter != 42 || again.Digits != 7 || again.Algorithm != "SHA512" || again.Issuer != "Corp" || again.Account != "bob" || string(again.Key) != string(original.Key) {
		t.Fatalf("%+v", again)
	}
	if strings.Contains(original.String(), "period") {
		t.Fatalf("a HOTP address carries no period: %s", original)
	}
}

func TestRemainingCountsDownWithinThePeriod(t *testing.T) {
	secret := Secret{Kind: TOTP, Period: 30}
	if got := secret.Remaining(time.Unix(59, 0)); got != time.Second {
		t.Fatalf("remaining %v", got)
	}
	if got := secret.Remaining(time.Unix(60, 0)); got != 30*time.Second {
		t.Fatalf("remaining %v", got)
	}
	if (Secret{Kind: HOTP}).Remaining(time.Now()) != 0 {
		t.Fatal("a counter has no clock")
	}
}

func TestQuestionWordingIsRecognised(t *testing.T) {
	for question, want := range map[string]bool{
		"Verification code: ": true, "Enter OTP:": true, "Authenticator code": true, "Duo passcode or option (1-3):": true,
		"请输入动态口令:": true, "请输入登录口令:": false, "Password: ": false, "Password for alice@host:": false, "": false,
		"請輸入驗證碼：": true, "請輸入一次性密碼：": true, "請輸入登入密碼：": false,
	} {
		if got := LooksLikeQuestion(question); got != want {
			t.Errorf("%q: %v", question, got)
		}
	}
}

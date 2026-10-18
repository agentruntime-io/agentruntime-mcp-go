package agentruntimemcp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

// verifyModeBSignature is a local copy of BFF VerifyAgentruntimeSignature logic
// so this package does not depend on the BFF. The algorithm must stay identical.
func verifyModeBSignature(secret, headerValue string, body []byte) bool {
	var ts int64
	var sigHex string
	for _, p := range strings.Split(headerValue, ",") {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(p, "t=") {
			v, err := strconv.ParseInt(strings.TrimPrefix(p, "t="), 10, 64)
			if err != nil {
				return false
			}
			ts = v
		}
		if strings.HasPrefix(p, "v1=") {
			sigHex = strings.TrimPrefix(p, "v1=")
		}
	}
	if ts == 0 || sigHex == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(fmt.Sprintf("%d.", ts)))
	_, _ = mac.Write(body)
	expected := mac.Sum(nil)
	got, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}
	return hmac.Equal(expected, got)
}

func TestSignModeB_VerifiesCorrectly(t *testing.T) {
	secret := "whsec_test_signing_secret"
	body := []byte(`{"event":"push","ref":"refs/heads/main"}`)
	unix := time.Now().Unix()

	hdr := SignModeB(secret, body, unix)

	if !strings.HasPrefix(hdr, fmt.Sprintf("t=%d,v1=", unix)) {
		t.Fatalf("unexpected header format: %s", hdr)
	}
	if !verifyModeBSignature(secret, hdr, body) {
		t.Fatalf("SignModeB output failed BFF-compatible verification: %s", hdr)
	}
}

func TestSignModeB_WrongBodyFails(t *testing.T) {
	secret := "whsec_test"
	body := []byte(`{"a":1}`)
	unix := time.Now().Unix()

	hdr := SignModeB(secret, body, unix)
	if verifyModeBSignature(secret, hdr, []byte(`{"a":2}`)) {
		t.Fatal("different body should not verify")
	}
}

func TestSignModeB_WrongSecretFails(t *testing.T) {
	body := []byte(`{}`)
	unix := time.Now().Unix()

	hdr := SignModeB("secret-a", body, unix)
	if verifyModeBSignature("secret-b", hdr, body) {
		t.Fatal("different secret should not verify")
	}
}

func TestSignModeB_HeaderFormat(t *testing.T) {
	hdr := SignModeB("s", []byte("body"), 1700000000)
	if !strings.HasPrefix(hdr, "t=1700000000,v1=") {
		t.Fatalf("unexpected format: %s", hdr)
	}
	parts := strings.SplitN(hdr, ",v1=", 2)
	if len(parts) != 2 || len(parts[1]) != 64 {
		t.Fatalf("expected 64-char hex v1 digest, got: %s", hdr)
	}
}

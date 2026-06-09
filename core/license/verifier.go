package license

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math"
	"strings"
	"time"
)

const (
	STATUS_INVALID       = "INVALID"
	STATUS_NOT_YET_VALID = "NOT_YET_VALID"
	STATUS_VALID         = "VALID"
	STATUS_EXPIRING_SOON = "EXPIRING_SOON"
	STATUS_GRACE         = "GRACE"
	STATUS_EXPIRED       = "EXPIRED"
)

type State struct {
	Status   string   `json:"status"`
	Reason   string   `json:"reason,omitempty"`
	DaysLeft int      `json:"days_left,omitempty"`
	Payload  *Payload `json:"-"`
}

type Payload struct {
	DeploymentId string `json:"deployment_id"`
	IssuedAt     string `json:"issued_at"`
	NotBefore    string `json:"not_before"`
	ExpiresAt    string `json:"expires_at"`
	GraceDays    int    `json:"grace_days"`
	TokenVersion int    `json:"token_version"`
}

// converts a PEM (SPKI) string into an ed25519 public key
func parsePublicKey(pemStr string) (ed25519.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, errors.New("public key: invalid PEM")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	ed, ok := pub.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("public key is not ed25519")
	}

	return ed, nil
}

// checks a token. Pure: no I/O, no panic.
// now should already be passed through the clock guard.
func verify(
	token string,
	pub ed25519.PublicKey,
	deploymentId string,
	now time.Time,
	expiringSoonDays int,
) State {
	// check token
	if token == "" {
		return State{
			Status: STATUS_INVALID,
			Reason: "TOKEN_EMPTY",
		}
	}

	parts := strings.Split(token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return State{
			Status: STATUS_INVALID,
			Reason: "TOKEN_MALFORMED",
		}
	}

	// check signature
	payloadB64, sigB64 := parts[0], parts[1]

	sig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return State{
			Status: STATUS_INVALID,
			Reason: "SIGNATURE_DECODE",
		}
	}

	// verify with the public key
	if len(pub) != ed25519.PublicKeySize {
		return State{
			Status: STATUS_INVALID,
			Reason: "PUBLIC_KEY_INVALID",
		}
	}

	if !ed25519.Verify(pub, []byte(payloadB64), sig) {
		return State{
			Status: STATUS_INVALID,
			Reason: "SIGNATURE_INVALID",
		}
	}

	// check payload
	raw, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return State{
			Status: STATUS_INVALID,
			Reason: "PAYLOAD_DECODE",
		}
	}

	var p Payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return State{
			Status: STATUS_INVALID,
			Reason: "PAYLOAD_UNREADABLE",
		}
	}

	// check deployment id
	if p.DeploymentId != deploymentId {
		return State{
			Status:  STATUS_INVALID,
			Reason:  "DEPLOYMENT_MISMATCH",
			Payload: &p,
		}
	}

	// check expired time
	exp, err := time.Parse(time.RFC3339, p.ExpiresAt)
	if err != nil {
		return State{
			Status:  STATUS_INVALID,
			Reason:  "BAD_EXPIRED_TIME",
			Payload: &p,
		}
	}

	nb, err := time.Parse(time.RFC3339, p.NotBefore)
	if err != nil {
		nb = time.Time{}
	}

	graceEnd := exp.Add(time.Duration(p.GraceDays) * 24 * time.Hour)

	switch {
	case now.Before(nb): // now < not-before
		return State{
			Status:  STATUS_NOT_YET_VALID,
			Payload: &p,
		}

	case !now.After(exp): // now <= expiry
		daysLeft := int(math.Ceil(exp.Sub(now).Hours() / 24))
		status := STATUS_VALID

		if daysLeft <= expiringSoonDays {
			status = STATUS_EXPIRING_SOON
		}

		return State{
			Status:   status,
			DaysLeft: daysLeft,
			Payload:  &p,
		}

	case !now.After(graceEnd): // now <= grace end
		return State{
			Status:  STATUS_GRACE,
			Payload: &p,
		}

	default:
		return State{
			Status:  STATUS_EXPIRED,
			Payload: &p,
		}
	}
}

// reports whether the status can still serve traffic
func isServingState(status string) bool {
	return status == STATUS_VALID || status == STATUS_EXPIRING_SOON || status == STATUS_GRACE
}

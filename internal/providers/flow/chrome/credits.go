package chrome

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type Tier string

const (
	TierUnknown      Tier = "UNSPECIFIED"
	TierPro          Tier = "PRO"
	TierUltra        Tier = "ULTRA"
	TierFree         Tier = "FREE"
	TierUnsubscribed Tier = "UNSUBSCRIBED_WITH_CREDITS"
	TierZero         Tier = "ZERO"
	TierExempt       Tier = "EXEMPT"
	TierGemnova      Tier = "GEMNOVA"
	TierUltra1P5     Tier = "ULTRA1P5"
)

var AllTiers = []struct {
	Value  Tier
	TSName string
}{
	{TierUnknown, "Unknown"}, {TierPro, "Pro"}, {TierUltra, "Ultra"}, {TierFree, "Free"},
	{TierUnsubscribed, "Unsubscribed"}, {TierZero, "Zero"}, {TierExempt, "Exempt"}, {TierGemnova, "Gemnova"}, {TierUltra1P5, "Ultra1P5"},
}

type FlowCredits struct {
	AvailabilityCode *int   `json:"availabilityCode"`
	Credits          int    `json:"credits"`
	TierCode         int    `json:"tierCode"`
	Tier             Tier   `json:"tier"`
	ResetStatus      int    `json:"resetStatus"`
	QuotaFlag        int    `json:"quotaFlag"`
	DailyLimit       int    `json:"dailyLimit"`
	ResetAt          string `json:"resetAt,omitempty"`
}

// ParseCreditsFromText selects only matching wrb.fr envelopes, after decoding
// XSSI and byte-delimited frames. A zero balance is a valid result.
func ParseCreditsFromText(text, rpcID string) (FlowCredits, error) {
	payload, err := parseRPCPayload(text, rpcID)
	if err != nil {
		return FlowCredits{}, err
	}
	return parseCreditsPayload(payload)
}

// ParseRPCPayload returns the JSON payload of the single wrb.fr envelope for
// rpcID, or an error when the envelope is missing, duplicated or carries a
// provider error status.
func ParseRPCPayload(text, rpcID string) (string, error) {
	return parseRPCPayload(text, rpcID)
}

func parseRPCPayload(text, rpcID string) (string, error) {
	if !rpcIDPattern.MatchString(rpcID) {
		return "", errors.New("invalid runtime RPC ID")
	}
	data := []byte(text)
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte(")]}'")) {
		if idx := bytes.IndexByte(data, '\n'); idx != -1 {
			data = data[idx+1:]
		}
	}
	var found *string
	for len(data) > 0 {
		start := bytes.IndexByte(data, '[')
		if start == -1 {
			break
		}
		data = data[start:]

		dec := json.NewDecoder(bytes.NewReader(data))
		var envelopes []json.RawMessage
		if err := dec.Decode(&envelopes); err != nil {
			data = data[1:]
			continue
		}
		data = data[dec.InputOffset():]

		for _, envBytes := range envelopes {
			var env []json.RawMessage
			if err := json.Unmarshal(envBytes, &env); err != nil || len(env) < 3 {
				continue
			}
			var kind, id string
			_ = json.Unmarshal(env[0], &kind)
			_ = json.Unmarshal(env[1], &id)
			if kind != "wrb.fr" || id != rpcID {
				continue
			}
			if found != nil {
				return "", fmt.Errorf("duplicate %s RPC response", rpcID)
			}
			if len(env) > 5 && !bytes.Equal(env[5], []byte("null")) {
				return "", fmt.Errorf("%s RPC returned an error: %s", rpcID, env[5])
			}
			var payload string
			if err := json.Unmarshal(env[2], &payload); err != nil || payload == "" {
				return "", fmt.Errorf("missing %s RPC payload", rpcID)
			}
			found = &payload
		}
	}
	if found == nil {
		return "", fmt.Errorf("missing %s RPC response", rpcID)
	}
	return *found, nil
}

// The bundle exposes field 1 as an integer. Preserve its server code rather than
// inferring region/age eligibility from undocumented numeric meanings.
func parseAvailability(payload string) (*int, error) {
	var fields []json.RawMessage
	if err := json.Unmarshal([]byte(payload), &fields); err != nil {
		return nil, errors.New("invalid availability schema")
	}
	if len(fields) == 0 || bytes.Equal(fields[0], []byte("null")) {
		return nil, nil
	}
	var code int
	if err := json.Unmarshal(fields[0], &code); err != nil || code < 0 {
		return nil, errors.New("invalid availability code")
	}
	return &code, nil
}
func parseCreditsPayload(payload string) (FlowCredits, error) {
	var fields []json.RawMessage
	if err := json.Unmarshal([]byte(payload), &fields); err != nil || len(fields) < 7 {
		return FlowCredits{}, errors.New("invalid credits schema")
	}
	var c FlowCredits
	for _, f := range []struct {
		index int
		value *int
	}{{0, &c.Credits}, {1, &c.TierCode}, {2, &c.ResetStatus}, {3, &c.QuotaFlag}, {5, &c.DailyLimit}} {
		if bytes.Equal(fields[f.index], []byte("null")) && f.index > 1 {
			continue
		}
		if err := json.Unmarshal(fields[f.index], f.value); err != nil || *f.value < 0 {
			return FlowCredits{}, fmt.Errorf("invalid credits field %d", f.index)
		}
	}
	c.Tier = tierName(c.TierCode)
	if !bytes.Equal(fields[6], []byte("null")) {
		var stamp []int64
		if err := json.Unmarshal(fields[6], &stamp); err != nil || len(stamp) != 2 || stamp[0] < 0 || stamp[1] < 0 || stamp[1] >= int64(time.Second) {
			return FlowCredits{}, errors.New("invalid credits reset timestamp")
		}
		c.ResetAt = time.Unix(stamp[0], stamp[1]).UTC().Format(time.RFC3339Nano)
	}
	return c, nil
}

func tierName(code int) Tier {
	// Protocol enum names from the reference adapter; unknown codes stay unknown.
	tiers := []Tier{TierUnknown, TierPro, TierUltra, TierFree, TierUnsubscribed, TierZero, TierExempt, TierGemnova, TierUltra1P5}
	if code < 0 || code >= len(tiers) {
		return TierUnknown
	}
	return tiers[code]
}

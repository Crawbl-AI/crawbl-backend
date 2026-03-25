package server

import "time"

type legalResponse struct {
	TermsOfService        string `json:"termsOfService"`
	PrivacyPolicy         string `json:"privacyPolicy"`
	TermsOfServiceVersion string `json:"termsOfServiceVersion"`
	PrivacyPolicyVersion  string `json:"privacyPolicyVersion"`
}

type dateTime struct {
	time.Time
}

func (d *dateTime) UnmarshalJSON(value []byte) error {
	raw := string(value)
	if raw == "null" {
		return nil
	}
	if len(raw) < 2 {
		return nil
	}
	raw = raw[1 : len(raw)-1]
	if raw == "" {
		return nil
	}

	parsed, err := time.Parse(time.RFC3339, raw)
	if err == nil {
		d.Time = parsed
		return nil
	}

	parsed, err = time.Parse("2006-01-02T15:04:05.000", raw)
	if err == nil {
		d.Time = parsed
		return nil
	}

	return err
}

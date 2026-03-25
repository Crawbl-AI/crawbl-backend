// Package server provides HTTP handlers and types.
package server

import "time"

// minJSONQuotedStringLen is the minimum length for a JSON quoted string (must contain at least "").
const minJSONQuotedStringLen = 2

// legalResponse contains the platform's legal documents for unauthenticated access.
// This is used before user registration to display terms of service and privacy policy.
type legalResponse struct {
	// TermsOfService is the full text of the terms of service document.
	TermsOfService string `json:"terms_of_service"`

	// PrivacyPolicy is the full text of the privacy policy document.
	PrivacyPolicy string `json:"privacy_policy"`

	// TermsOfServiceVersion is the version identifier for the current terms of service.
	TermsOfServiceVersion string `json:"terms_of_service_version"`

	// PrivacyPolicyVersion is the version identifier for the current privacy policy.
	PrivacyPolicyVersion string `json:"privacy_policy_version"`
}

// dateTime is a custom time type that handles multiple date format parsing in JSON.
// It supports RFC3339 format and a custom milliseconds format for compatibility
// with various client date/time representations.
type dateTime struct {
	// Time is the underlying time value after successful parsing.
	time.Time
}

// UnmarshalJSON implements custom JSON unmarshaling for dateTime.
// It handles three cases:
//   - JSON null value: returns without error, leaving time as zero value
//   - Empty or short strings: returns without error, leaving time as zero value
//   - Valid date strings: parses using RFC3339 or custom milliseconds format
//
// This flexible parsing supports clients that may send dates in different formats.
func (d *dateTime) UnmarshalJSON(value []byte) error {
	raw := string(value)
	if raw == "null" {
		return nil
	}
	if len(raw) < minJSONQuotedStringLen {
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

package model

import "errors"

// ErrOpenAIAPIKeyMissing is returned when config.Validate has been skipped
// and the constructor is called with an empty APIKey. It's safe to return
// the zero Config from tests, but production construction always goes
// through config.Load + Validate which rejects a missing key earlier.
var ErrOpenAIAPIKeyMissing = errors.New("openai: APIKey is required (set OPENAI_API_KEY env var or config.OpenAI.APIKey)")

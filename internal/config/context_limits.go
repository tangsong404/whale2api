package config

import (
	"os"
	"strconv"
	"strings"
)

// Defaults: the upstream reverse proxy silently drops the tail beyond ~890K
// native tokens, so the user-facing value advertises 896K while the logical
// gate keeps the local estimate measured at that boundary (I=2.85M).
// Over-gate requests return the same 400 structure as the official DeepSeek
// API. Both values can still be overridden via env.
const (
	DefaultUserFacingContextLimitTokens = 896_000
	DefaultLogicalContextLimitTokens    = 2_850_000
)

const (
	envUserFacingContextLimit = "WHALE2API_USER_FACING_CONTEXT_LIMIT_TOKENS"
	envLogicalContextLimit    = "WHALE2API_LOGICAL_CONTEXT_LIMIT_TOKENS"
)

var (
	// EffectiveUserFacingContextLimitTokens is the limit described to clients
	// in /v1/models and in context-length errors.
	EffectiveUserFacingContextLimitTokens = DefaultUserFacingContextLimitTokens
	// EffectiveLogicalContextLimitTokens gates internal prompt token counts
	// before a request is forwarded upstream.
	EffectiveLogicalContextLimitTokens = DefaultLogicalContextLimitTokens
)

// LoadContextLimitOverrides reads the optional context-limit env overrides.
// Invalid values (non-numeric, <= 0, or logical < user-facing) fall back to
// the defaults as a pair and log a warning. Call once at startup; tests may
// call it after mutating the environment.
func LoadContextLimitOverrides() {
	user := DefaultUserFacingContextLimitTokens
	logical := DefaultLogicalContextLimitTokens
	valid := true

	userRaw := strings.TrimSpace(os.Getenv(envUserFacingContextLimit))
	logicalRaw := strings.TrimSpace(os.Getenv(envLogicalContextLimit))

	if userRaw != "" {
		parsed, err := strconv.Atoi(userRaw)
		if err != nil || parsed <= 0 {
			Logger.Warn("[context] invalid user-facing context limit override; using defaults",
				"env", envUserFacingContextLimit, "value", userRaw,
				"default", DefaultUserFacingContextLimitTokens)
			valid = false
		} else {
			user = parsed
		}
	}
	if logicalRaw != "" {
		parsed, err := strconv.Atoi(logicalRaw)
		if err != nil || parsed <= 0 {
			Logger.Warn("[context] invalid logical context limit override; using defaults",
				"env", envLogicalContextLimit, "value", logicalRaw,
				"default", DefaultLogicalContextLimitTokens)
			valid = false
		} else {
			logical = parsed
		}
	}
	if valid && logical < user {
		Logger.Warn("[context] logical context limit is below the user-facing limit; using defaults",
			"logical", logical, "user_facing", user)
		valid = false
	}
	if !valid {
		user = DefaultUserFacingContextLimitTokens
		logical = DefaultLogicalContextLimitTokens
	}

	EffectiveUserFacingContextLimitTokens = user
	EffectiveLogicalContextLimitTokens = logical
	AdvertisedMaxContextTokens = user
	refreshDeepSeekModelContextLengths()

	if user != DefaultUserFacingContextLimitTokens || logical != DefaultLogicalContextLimitTokens {
		Logger.Info("[context] context limit overrides applied",
			"user_facing", user, "logical", logical)
	}
}

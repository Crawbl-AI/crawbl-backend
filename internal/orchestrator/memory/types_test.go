package memory

import "testing"

func TestTokenBudgets(t *testing.T) {
	if TokenBudgetL0+TokenBudgetL1+TokenBudgetL2 > TokenBudgetTotal {
		t.Errorf("token budgets exceed total: %d + %d + %d > %d",
			TokenBudgetL0, TokenBudgetL1, TokenBudgetL2, TokenBudgetTotal)
	}
}

func TestWorkspaceLimits(t *testing.T) {
	if MaxDrawersPerWorkspace <= 0 {
		t.Error("MaxDrawersPerWorkspace must be positive")
	}
	if MaxEntitiesPerWorkspace <= 0 {
		t.Error("MaxEntitiesPerWorkspace must be positive")
	}
	if MaxTriplesPerWorkspace <= 0 {
		t.Error("MaxTriplesPerWorkspace must be positive")
	}
	if MaxContentLength <= 0 {
		t.Error("MaxContentLength must be positive")
	}
	if MaxIdentityLength <= 0 {
		t.Error("MaxIdentityLength must be positive")
	}
}

func TestMemoryTypeConstants(t *testing.T) {
	types := []MemoryType{
		MemoryTypeDecision,
		MemoryTypePreference,
		MemoryTypeMilestone,
		MemoryTypeProblem,
		MemoryTypeEmotional,
	}
	seen := make(map[MemoryType]bool)
	for _, mt := range types {
		if mt == "" {
			t.Error("empty memory type")
		}
		if seen[mt] {
			t.Errorf("duplicate memory type: %s", mt)
		}
		seen[mt] = true
	}
}

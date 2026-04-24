package protoconv

import memoryv1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/memory/v1"

// --- MemoryType ---

// MemoryTypeToProto converts a Go string to the proto enum value.
func MemoryTypeToProto(s string) memoryv1.MemoryType {
	if v, ok := memoryTypeToProto[s]; ok {
		return v
	}
	return memoryv1.MemoryType_MEMORY_TYPE_UNSPECIFIED
}

// MemoryTypeFromProto converts a proto enum value to the Go string.
func MemoryTypeFromProto(v memoryv1.MemoryType) string {
	return memoryTypeFromProto[v]
}

// --- DrawerState ---

// DrawerStateToProto converts a Go string to the proto enum value.
func DrawerStateToProto(s string) memoryv1.DrawerState {
	if v, ok := drawerStateToProto[s]; ok {
		return v
	}
	return memoryv1.DrawerState_DRAWER_STATE_UNSPECIFIED
}

// DrawerStateFromProto converts a proto enum value to the Go string.
func DrawerStateFromProto(v memoryv1.DrawerState) string {
	return drawerStateFromProto[v]
}

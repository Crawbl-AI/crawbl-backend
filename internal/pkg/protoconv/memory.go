package protoconv

import memoryv1 "github.com/Crawbl-AI/crawbl-backend/internal/generated/proto/memory/v1"

// --- MemoryType ---

var (
	memoryTypeToProto = map[string]memoryv1.MemoryType{
		"decision":   memoryv1.MemoryType_MEMORY_TYPE_DECISION,
		"preference": memoryv1.MemoryType_MEMORY_TYPE_PREFERENCE,
		"milestone":  memoryv1.MemoryType_MEMORY_TYPE_MILESTONE,
		"problem":    memoryv1.MemoryType_MEMORY_TYPE_PROBLEM,
		"emotional":  memoryv1.MemoryType_MEMORY_TYPE_EMOTIONAL,
		"fact":       memoryv1.MemoryType_MEMORY_TYPE_FACT,
		"task":       memoryv1.MemoryType_MEMORY_TYPE_TASK,
	}
	memoryTypeFromProto = inverseMapIS(memoryTypeToProto)
)

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

var (
	drawerStateToProto = map[string]memoryv1.DrawerState{
		"raw":         memoryv1.DrawerState_DRAWER_STATE_RAW,
		"classifying": memoryv1.DrawerState_DRAWER_STATE_CLASSIFYING,
		"processed":   memoryv1.DrawerState_DRAWER_STATE_PROCESSED,
		"merged":      memoryv1.DrawerState_DRAWER_STATE_MERGED,
		"failed":      memoryv1.DrawerState_DRAWER_STATE_FAILED,
	}
	drawerStateFromProto = inverseMapIS(drawerStateToProto)
)

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

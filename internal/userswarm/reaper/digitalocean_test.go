package reaper

import (
	"testing"
	"time"
)

func TestClassifyOrphanVolume(t *testing.T) {
	now := time.Date(2026, 3, 30, 12, 0, 0, 0, time.UTC)
	minAge := 30 * time.Minute

	testCases := []struct {
		name               string
		volume             doVolume
		livePVNames        map[string]struct{}
		currentClusterTags map[string]struct{}
		activeClusterTags  map[string]struct{}
		wantReason         string
		wantMatch          bool
	}{
		{
			name: "current cluster orphan",
			volume: doVolume{
				Name:      "pvc-deadbeef",
				CreatedAt: now.Add(-2 * time.Hour),
				Tags:      []string{"k8s:current"},
			},
			livePVNames:        map[string]struct{}{},
			currentClusterTags: map[string]struct{}{"k8s:current": {}},
			activeClusterTags:  map[string]struct{}{"k8s:current": {}},
			wantReason:         "no matching PV in current cluster",
			wantMatch:          true,
		},
		{
			name: "dead cluster orphan",
			volume: doVolume{
				Name:      "pvc-deadcluster",
				CreatedAt: now.Add(-2 * time.Hour),
				Tags:      []string{"k8s:deleted-cluster"},
			},
			livePVNames:        map[string]struct{}{},
			currentClusterTags: map[string]struct{}{"k8s:current": {}},
			activeClusterTags:  map[string]struct{}{"k8s:current": {}},
			wantReason:         "tagged for a deleted DOKS cluster",
			wantMatch:          true,
		},
		{
			name: "other active cluster is skipped",
			volume: doVolume{
				Name:      "pvc-othercluster",
				CreatedAt: now.Add(-2 * time.Hour),
				Tags:      []string{"k8s:other"},
			},
			livePVNames:        map[string]struct{}{},
			currentClusterTags: map[string]struct{}{"k8s:current": {}},
			activeClusterTags:  map[string]struct{}{"k8s:current": {}, "k8s:other": {}},
			wantMatch:          false,
		},
		{
			name: "live pv is skipped",
			volume: doVolume{
				Name:      "pvc-live",
				CreatedAt: now.Add(-2 * time.Hour),
				Tags:      []string{"k8s:current"},
			},
			livePVNames:        map[string]struct{}{"pvc-live": {}},
			currentClusterTags: map[string]struct{}{"k8s:current": {}},
			activeClusterTags:  map[string]struct{}{"k8s:current": {}},
			wantMatch:          false,
		},
		{
			name: "attached volume is skipped",
			volume: doVolume{
				Name:       "pvc-attached",
				CreatedAt:  now.Add(-2 * time.Hour),
				Tags:       []string{"k8s:current"},
				DropletIDs: []int{123},
			},
			livePVNames:        map[string]struct{}{},
			currentClusterTags: map[string]struct{}{"k8s:current": {}},
			activeClusterTags:  map[string]struct{}{"k8s:current": {}},
			wantMatch:          false,
		},
		{
			name: "recent volume is skipped",
			volume: doVolume{
				Name:      "pvc-fresh",
				CreatedAt: now.Add(-10 * time.Minute),
				Tags:      []string{"k8s:current"},
			},
			livePVNames:        map[string]struct{}{},
			currentClusterTags: map[string]struct{}{"k8s:current": {}},
			activeClusterTags:  map[string]struct{}{"k8s:current": {}},
			wantMatch:          false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gotReason, gotMatch := classifyOrphanVolume(tc.volume, tc.livePVNames, tc.currentClusterTags, tc.activeClusterTags, minAge, now)
			if gotMatch != tc.wantMatch {
				t.Fatalf("match = %v, want %v", gotMatch, tc.wantMatch)
			}
			if gotReason != tc.wantReason {
				t.Fatalf("reason = %q, want %q", gotReason, tc.wantReason)
			}
		})
	}
}

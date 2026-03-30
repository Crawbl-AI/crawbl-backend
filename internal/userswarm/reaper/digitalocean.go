package reaper

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// We only need a tiny slice of the DO API for this cleanup path:
	// list volumes, list Kubernetes clusters, and delete a volume by ID.
	doAPIBaseURL = "https://api.digitalocean.com/v2"
	// The account is small today, but we still page defensively so the sweeper
	// does not silently miss leaks if the number of CSI volumes grows later.
	doPageSize = 200
	// DOKS-created block volumes are named after the backing PV, which is why
	// every CSI volume we care about starts with "pvc-".
	doVolumeNamePrefix = "pvc-"
	// DOKS tags every CSI volume with a cluster tag like k8s:<cluster-id>.
	// We use that tag to distinguish current-cluster leaks from dead-cluster leaks.
	doKubernetesTagPrefix = "k8s:"
)

// doClient is intentionally tiny.
// We do not need the full DigitalOcean SDK here; a small HTTP wrapper keeps the
// reaper dependency surface narrow and makes the safety logic easy to read.
type doClient struct {
	httpClient *http.Client
	baseURL    string
}

// doVolume mirrors only the fields we use when deciding whether a block volume
// is a billing leak or a legitimate live resource.
type doVolume struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	DropletIDs  []int     `json:"droplet_ids"`
	CreatedAt   time.Time `json:"created_at"`
	Tags        []string  `json:"tags"`
}

// The list endpoints wrap items under top-level keys instead of returning bare arrays.
type doVolumesResponse struct {
	Volumes []doVolume `json:"volumes"`
}

// doCluster is trimmed to the identity and tag fields needed for leak detection.
// We only care whether a cluster still exists and which k8s:<cluster-id> tags
// should still be considered "live".
type doCluster struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
	Tags   []string `json:"tags"`
	Status struct {
		State string `json:"state"`
	} `json:"status"`
}

type doClustersResponse struct {
	KubernetesClusters []doCluster `json:"kubernetes_clusters"`
}

// newDOClient builds an OAuth-backed HTTP client for the DO API.
// The reaper calls this only when a token is present; local dry-runs can still
// execute the Kubernetes/database cleanup path without DO access.
func newDOClient(token string) *doClient {
	tokenSource := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	return &doClient{
		httpClient: oauth2.NewClient(context.Background(), tokenSource),
		baseURL:    doAPIBaseURL,
	}
}

// reapOrphanedVolumes is the external side of swarm cleanup.
//
// Why this exists:
//   - Kubernetes reclaim policy "Delete" is supposed to remove the DO block
//     volume when the PVC/PV disappears.
//   - In practice, CSI teardown can drift and leave unattached DO volumes
//     billing forever even though Kubernetes already forgot them.
//   - Recreated DOKS clusters make this worse because old k8s:<cluster-id>
//     volumes become invisible to the new cluster.
//
// Safety model:
//   - Never touch attached volumes.
//   - Never touch a DO volume that still has a matching live PV.
//   - Only touch k8s-tagged pvc-* volumes.
//   - Only touch volumes older than the configured grace period.
//   - Delete when the volume is clearly tied to this cluster but has no PV, or
//     when it is tied to a cluster tag that no longer exists at all.
func reapOrphanedVolumes(ctx context.Context, k8sClient client.Client, logger *slog.Logger, cfg *Config) (reaped, errors int) {
	// If the token is missing we deliberately skip external cleanup instead of
	// failing the entire reaper. That keeps the normal stale-user cleanup working
	// even in local environments where DO credentials are not configured.
	if cfg.DigitalOceanToken == "" {
		logger.Info("skipping orphan volume cleanup because DIGITALOCEAN_ACCESS_TOKEN is not configured")
		return 0, 0
	}

	// Snapshot the cluster's current PV names first.
	// DO CSI names the volume after the PV, so this gives us a direct join key
	// between Kubernetes state and DO state without needing any custom metadata.
	livePVNames, err := listPersistentVolumeNames(ctx, k8sClient)
	if err != nil {
		logger.Error("failed to list persistent volumes", "error", err)
		return 0, 1
	}

	client := newDOClient(cfg.DigitalOceanToken)

	// We need the full DO volume list because Kubernetes may already have lost
	// the corresponding PV objects for leaked assets.
	volumes, err := client.listVolumes(ctx)
	if err != nil {
		logger.Error("failed to list DigitalOcean volumes", "error", err)
		return 0, 1
	}

	// We also need the set of still-existing DOKS cluster tags.
	// That lets us catch two different leak classes:
	//   1. current-cluster volumes with no PV anymore
	//   2. old-cluster volumes whose entire cluster was recreated or deleted
	activeClusterTags, err := client.listActiveClusterTags(ctx)
	if err != nil {
		logger.Error("failed to list DigitalOcean Kubernetes clusters", "error", err)
		return 0, 1
	}

	// The most trustworthy way to identify "our" live cluster tag is to read it
	// off the volumes that still back real PVs in the current cluster.
	currentClusterTags := currentClusterTagsFromLivePVs(volumes, livePVNames)
	// On a very fresh cluster there may be zero PVs yet. If there is exactly one
	// active DOKS cluster in the account, we can still safely treat that tag as current.
	if len(currentClusterTags) == 0 && len(activeClusterTags) == 1 {
		currentClusterTags = cloneStringSet(activeClusterTags)
	}
	now := time.Now().UTC()

	// Evaluate every DO volume against the safety rules above.
	for _, vol := range volumes {
		reason, ok := classifyOrphanVolume(vol, livePVNames, currentClusterTags, activeClusterTags, cfg.OrphanVolumeMinAge, now)
		if !ok {
			continue
		}

		logger.Info("found orphaned DigitalOcean volume",
			"volume_id", vol.ID,
			"name", vol.Name,
			"reason", reason,
			"created_at", vol.CreatedAt.Format(time.RFC3339),
			"dry_run", cfg.DryRun,
		)

		if cfg.DryRun {
			reaped++
			continue
		}

		// deleteVolume is intentionally idempotent: a 404 means another cleanup
		// worker or a delayed CSI teardown already won the race.
		if err := client.deleteVolume(ctx, vol.ID); err != nil {
			logger.Error("failed to delete orphaned DigitalOcean volume", "volume_id", vol.ID, "name", vol.Name, "error", err)
			errors++
			continue
		}

		logger.Info("deleted orphaned DigitalOcean volume", "volume_id", vol.ID, "name", vol.Name, "reason", reason)
		reaped++
	}

	return reaped, errors
}

// listPersistentVolumeNames returns the set of live PV object names.
// In DOKS CSI those names match the DO volume names exactly, which is why the
// reconciler can compare the two systems without additional database state.
func listPersistentVolumeNames(ctx context.Context, k8sClient client.Client) (map[string]struct{}, error) {
	var pvList corev1.PersistentVolumeList
	if err := k8sClient.List(ctx, &pvList); err != nil {
		return nil, err
	}

	names := make(map[string]struct{}, len(pvList.Items))
	for i := range pvList.Items {
		names[pvList.Items[i].Name] = struct{}{}
	}
	return names, nil
}

// currentClusterTagsFromLivePVs discovers which k8s:<cluster-id> tags belong to
// the cluster we are currently talking to.
//
// Why infer instead of hard-coding:
//   - the cluster may be recreated
//   - the account may contain old deleted-cluster tags
//   - the reaper should not need a separate cluster-ID config knob
func currentClusterTagsFromLivePVs(volumes []doVolume, livePVNames map[string]struct{}) map[string]struct{} {
	tags := map[string]struct{}{}
	for _, vol := range volumes {
		if _, ok := livePVNames[vol.Name]; !ok {
			continue
		}
		for _, tag := range kubernetesTags(vol.Tags) {
			tags[tag] = struct{}{}
		}
	}
	return tags
}

// classifyOrphanVolume is the core policy decision.
//
// It answers one question:
// "Given what Kubernetes knows right now and what DigitalOcean knows right now,
//
//	is this volume definitely a leak we should delete?"
//
// Returning a human-readable reason helps logs explain *why* a volume matched,
// which matters because this code is deleting billable infrastructure.
func classifyOrphanVolume(vol doVolume, livePVNames, currentClusterTags, activeClusterTags map[string]struct{}, minAge time.Duration, now time.Time) (string, bool) {
	// Ignore anything that does not look like a CSI-created PV volume.
	if !strings.HasPrefix(vol.Name, doVolumeNamePrefix) {
		return "", false
	}
	// An attached volume is actively in use by some droplet, so we never touch it.
	if len(vol.DropletIDs) > 0 {
		return "", false
	}
	// Give the CSI driver time to finish normal cleanup before we treat the
	// resource as leaked.
	if minAge > 0 && now.Sub(vol.CreatedAt) < minAge {
		return "", false
	}
	// If Kubernetes still has the PV, the volume is still part of the desired state.
	if _, ok := livePVNames[vol.Name]; ok {
		return "", false
	}

	// Only DOKS-managed Kubernetes volumes are in scope for this sweeper.
	k8sTags := kubernetesTags(vol.Tags)
	if len(k8sTags) == 0 {
		return "", false
	}

	// Case 1: the volume carries the current cluster tag, but Kubernetes has no PV.
	// That means the external asset outlived the in-cluster storage objects.
	if overlapsWithSet(k8sTags, currentClusterTags) {
		return "no matching PV in current cluster", true
	}
	// Case 2: none of the volume's cluster tags correspond to any active DOKS cluster.
	// This is the recreated-cluster leak we observed in the dev account.
	if !overlapsWithSet(k8sTags, activeClusterTags) {
		return "tagged for a deleted DOKS cluster", true
	}
	// If the tag belongs to some other active cluster, leave it alone.
	return "", false
}

// kubernetesTags keeps only DOKS cluster tags from an arbitrary tag list.
func kubernetesTags(tags []string) []string {
	var result []string
	for _, tag := range tags {
		if strings.HasPrefix(tag, doKubernetesTagPrefix) {
			result = append(result, tag)
		}
	}
	return result
}

// overlapsWithSet is a small helper because we repeatedly compare a list of
// volume tags against a set of known-good cluster tags.
func overlapsWithSet(values []string, set map[string]struct{}) bool {
	for _, v := range values {
		if _, ok := set[v]; ok {
			return true
		}
	}
	return false
}

// cloneStringSet avoids aliasing when we use the active-cluster set as a fallback.
func cloneStringSet(in map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{}, len(in))
	for key := range in {
		out[key] = struct{}{}
	}
	return out
}

// listVolumes walks the DO volumes endpoint page-by-page until it exhausts results.
// We keep the loop explicit instead of hiding it behind a generic pager because the
// reaper only has two endpoints and the control flow is easier to audit this way.
func (c *doClient) listVolumes(ctx context.Context) ([]doVolume, error) {
	var all []doVolume
	for page := 1; ; page++ {
		var resp doVolumesResponse
		if err := c.get(ctx, "/volumes", map[string]string{
			"page":     fmt.Sprintf("%d", page),
			"per_page": fmt.Sprintf("%d", doPageSize),
		}, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.Volumes...)
		if len(resp.Volumes) < doPageSize {
			return all, nil
		}
	}
}

// listActiveClusterTags returns the set of k8s:<cluster-id> tags for clusters
// that still exist. We record both the explicit tag list and the cluster ID
// itself because DO exposes the same identity in both places depending on API shape.
func (c *doClient) listActiveClusterTags(ctx context.Context) (map[string]struct{}, error) {
	tags := map[string]struct{}{}
	for page := 1; ; page++ {
		var resp doClustersResponse
		if err := c.get(ctx, "/kubernetes/clusters", map[string]string{
			"page":     fmt.Sprintf("%d", page),
			"per_page": fmt.Sprintf("%d", doPageSize),
		}, &resp); err != nil {
			return nil, err
		}
		for _, cluster := range resp.KubernetesClusters {
			if strings.EqualFold(cluster.Status.State, "deleted") {
				continue
			}
			for _, tag := range kubernetesTags(cluster.Tags) {
				tags[tag] = struct{}{}
			}
			if cluster.ID != "" {
				tags[doKubernetesTagPrefix+cluster.ID] = struct{}{}
			}
		}
		if len(resp.KubernetesClusters) < doPageSize {
			return tags, nil
		}
	}
}

// deleteVolume performs the final destructive API call.
//
// The method is intentionally strict:
//   - 204 No Content is success
//   - 404 Not Found is treated as success for idempotency
//   - anything else is surfaced with the response body for debugging
func (c *doClient) deleteVolume(ctx context.Context, volumeID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+"/volumes/"+volumeID, nil)
	if err != nil {
		return err
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return nil
	}
	if res.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 8192))
		return fmt.Errorf("delete volume %s: unexpected status %s: %s", volumeID, res.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

// get is the shared GET helper for the tiny DO client.
// It centralizes query encoding, status checking, and JSON decoding so the
// higher-level sweep logic stays focused on reconciliation rather than HTTP plumbing.
func (c *doClient) get(ctx context.Context, path string, query map[string]string, out interface{}) error {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return err
	}
	params := u.Query()
	for key, value := range query {
		params.Set(key, value)
	}
	u.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}

	res, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 8192))
		return fmt.Errorf("GET %s: unexpected status %s: %s", u.String(), res.Status, strings.TrimSpace(string(body)))
	}

	return json.NewDecoder(res.Body).Decode(out)
}

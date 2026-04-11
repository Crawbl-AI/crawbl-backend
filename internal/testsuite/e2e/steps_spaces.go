// Package e2e — DO Spaces assertion step definitions.
//
// Steps are phrased as workspace-file-store concepts so .feature
// files never mention "Spaces", "S3", or "HeadObject":
//
//	a file named "trip.md" should exist in the workspace file store
//	the saved file "trip.md" should contain "Paris"
//
// When tc.spacesClient is nil the step is a silent no-op.
package e2e

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/cucumber/godog"
)

func registerSpacesSteps(sc *godog.ScenarioContext, tc *testContext) {
	sc.Step(`^a file named "([^"]*)" should exist in the workspace file store$`, tc.fileShouldExistInWorkspace)
	sc.Step(`^the saved file "([^"]*)" should contain "([^"]*)"$`, tc.savedFileShouldContain)
}

// workspaceObjectKey builds the full Spaces object key for a file
// inside the primary user's workspace, mirroring the prefix used by
// internal/agentruntime/storage/spaces.go.
func (tc *testContext) workspaceObjectKey(fileName string) (string, error) {
	state := tc.state["primary"]
	if state == nil || state.workspaceID == "" {
		return "", fmt.Errorf("primary user workspace not initialized")
	}
	return fmt.Sprintf("workspaces/%s/%s", state.workspaceID, fileName), nil
}

func (tc *testContext) fileShouldExistInWorkspace(fileName string) error {
	if tc.spacesClient == nil {
		return nil
	}
	key, err := tc.workspaceObjectKey(fileName)
	if err != nil {
		return err
	}

	return tc.pollDefault(func() error {
		_, err := tc.spacesClient.HeadObject(context.Background(), &s3.HeadObjectInput{
			Bucket: &tc.cfg.SpacesBucket,
			Key:    &key,
		})
		if err != nil {
			return fmt.Errorf("file %q not found in workspace file store: %w", fileName, err)
		}
		return nil
	})
}

func (tc *testContext) savedFileShouldContain(fileName, substring string) error {
	if tc.spacesClient == nil {
		return nil
	}
	key, err := tc.workspaceObjectKey(fileName)
	if err != nil {
		return err
	}

	return tc.pollDefault(func() error {
		resp, err := tc.spacesClient.GetObject(context.Background(), &s3.GetObjectInput{
			Bucket: &tc.cfg.SpacesBucket,
			Key:    &key,
		})
		if err != nil {
			return fmt.Errorf("failed to read file %q: %w", fileName, err)
		}
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read body of %q: %w", fileName, err)
		}
		if !strings.Contains(string(body), substring) {
			return fmt.Errorf("file %q does not contain %q; got %q", fileName, substring, string(body))
		}
		return nil
	})
}

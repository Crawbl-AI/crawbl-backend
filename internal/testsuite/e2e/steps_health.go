package e2e

import "github.com/cucumber/godog"

func registerHealthSteps(sc *godog.ScenarioContext, tc *testContext) {
	sc.Step(`^the guest checks the service health$`, tc.guestChecksServiceHealth)
	sc.Step(`^the service should report online$`, tc.serviceShouldReportOnline)
	sc.Step(`^the guest reads the public legal documents$`, tc.guestReadsPublicLegalDocuments)
	sc.Step(`^the public legal documents should be available$`, tc.publicLegalDocumentsShouldBeAvailable)
}

func (tc *testContext) guestChecksServiceHealth() error {
	_, err := tc.doRequest("GET", "/v1/health", "", nil)
	return err
}

func (tc *testContext) serviceShouldReportOnline() error {
	if err := tc.assertStatus(statusOK); err != nil {
		return err
	}
	return tc.assertJSONEquals("data.online", "true")
}

func (tc *testContext) guestReadsPublicLegalDocuments() error {
	_, err := tc.doRequest("GET", "/v1/legal", "", nil)
	return err
}

func (tc *testContext) publicLegalDocumentsShouldBeAvailable() error {
	if err := tc.assertStatus(statusOK); err != nil {
		return err
	}
	if err := tc.assertJSONNotEmpty("data.terms_of_service_version"); err != nil {
		return err
	}
	return tc.assertJSONNotEmpty("data.privacy_policy_version")
}

package e2e

import (
	"fmt"
	"net/http"
	"time"

	"github.com/cucumber/godog"
	"github.com/tidwall/gjson"
)

func registerAuthSteps(sc *godog.ScenarioContext, tc *testContext) {
	// Sign-up / sign-in
	sc.Step(`^user "([^"]*)" signs up$`, tc.userSignsUp)
	sc.Step(`^user "([^"]*)" signs in$`, tc.userSignsIn)
	sc.Step(`^user "([^"]*)" should exist in the database$`, tc.userShouldExistInDatabase)
	sc.Step(`^user "([^"]*)" should have one workspace in the database$`, tc.userShouldHaveOneWorkspaceInDatabase)

	// Profile
	sc.Step(`^user "([^"]*)" opens their profile$`, tc.userOpensProfile)
	sc.Step(`^user "([^"]*)" should see their default profile details$`, tc.userShouldSeeDefaultProfileDetails)
	sc.Step(`^user "([^"]*)" updates their profile details$`, tc.userUpdatesProfileDetails)
	sc.Step(`^user "([^"]*)" should see their updated profile details$`, tc.userShouldSeeUpdatedProfileDetails)
	sc.Step(`^user "([^"]*)" registers a push token$`, tc.userRegistersPushToken)
	sc.Step(`^the push token for user "([^"]*)" should be stored$`, tc.pushTokenShouldBeStored)

	// Legal acceptance
	sc.Step(`^user "([^"]*)" opens their legal status$`, tc.userOpensLegalStatus)
	sc.Step(`^user "([^"]*)" should see the current legal versions$`, tc.userShouldSeeCurrentLegalVersions)
	sc.Step(`^user "([^"]*)" accepts the current legal documents$`, tc.userAcceptsCurrentLegalDocuments)
	sc.Step(`^user "([^"]*)" should show accepted legal documents$`, tc.userShouldShowAcceptedLegalDocuments)

	// Account deletion
	sc.Step(`^user "([^"]*)" deletes their account$`, tc.userDeletesTheirAccount)
	sc.Step(`^user "([^"]*)" should be marked as deleted in the database$`, tc.userShouldBeMarkedAsDeletedInDatabase)
	sc.Step(`^the deleted account should no longer behave like an active user$`, tc.deletedAccountShouldNoLongerBehaveLikeActiveUser)

	// Edge-case guards
	sc.Step(`^a guest requests their profile$`, tc.guestRequestsProfile)
	sc.Step(`^the request should be unauthorized$`, tc.requestShouldBeUnauthorized)
	sc.Step(`^the request should be rejected as invalid$`, tc.requestShouldBeRejectedAsInvalid)
	sc.Step(`^the request should be rejected as not found$`, tc.requestShouldBeRejectedAsNotFound)
}

// --- Sign-up / sign-in -----------------------------------------------

func (tc *testContext) userSignsUp(alias string) error {
	if err := tc.userHasSignedUp(alias); err != nil {
		return err
	}
	return tc.assertStatus(http.StatusNoContent)
}

func (tc *testContext) userSignsIn(alias string) error {
	if _, err := tc.doRequest("POST", "/v1/auth/sign-in", alias, nil); err != nil {
		return err
	}
	return tc.assertStatus(http.StatusNoContent)
}

func (tc *testContext) userShouldExistInDatabase(alias string) error {
	return tc.dbHasUserWithSubject(alias)
}

func (tc *testContext) userShouldHaveOneWorkspaceInDatabase(alias string) error {
	return tc.dbWorkspaceCountForSubject(1, alias)
}

// --- Profile ---------------------------------------------------------

func (tc *testContext) userOpensProfile(alias string) error {
	if _, err := tc.doRequest("GET", "/v1/users/profile", alias, nil); err != nil {
		return err
	}
	return tc.assertStatus(http.StatusOK)
}

func (tc *testContext) userShouldSeeDefaultProfileDetails(alias string) error {
	if err := tc.userOpensProfile(alias); err != nil {
		return err
	}
	for path, expected := range map[string]string{
		"data.is_deleted":        "false",
		"data.is_banned":         "false",
		"data.subscription.code": "freemium",
	} {
		if err := tc.assertJSONEquals(path, expected); err != nil {
			return err
		}
	}
	if err := tc.assertJSONEqualsSubject("data.firebase_uid", alias); err != nil {
		return err
	}
	return tc.assertJSONEqualsEmail("data.email", alias)
}

func (tc *testContext) userUpdatesProfileDetails(alias string) error {
	body := map[string]any{
		"nickname":      "berlin-builder",
		"name":          "Alex",
		"surname":       "Tester",
		"country_code":  "DE",
		"date_of_birth": "2000-01-15T00:00:00Z",
		"preferences": map[string]any{
			"platform_theme":    "dark",
			"platform_language": "en",
			"currency_code":     "EUR",
		},
	}
	if _, err := tc.doRequest("PATCH", "/v1/users", alias, body); err != nil {
		return err
	}
	return tc.assertStatus(http.StatusNoContent)
}

func (tc *testContext) userShouldSeeUpdatedProfileDetails(alias string) error {
	if err := tc.userOpensProfile(alias); err != nil {
		return err
	}
	for path, expected := range map[string]string{
		"data.nickname":                      "berlin-builder",
		"data.name":                          "Alex",
		"data.surname":                       "Tester",
		"data.country_code":                  "DE",
		"data.preferences.platform_theme":    "dark",
		"data.preferences.platform_language": "en",
		"data.preferences.currency_code":     "EUR",
	} {
		if err := tc.assertJSONEquals(path, expected); err != nil {
			return err
		}
	}
	if err := tc.dbUserHasNickname(alias, "berlin-builder"); err != nil {
		return err
	}
	return tc.dbUserHasCountryCode(alias, "DE")
}

func (tc *testContext) userRegistersPushToken(alias string) error {
	state := tc.userState(alias)
	state.pushToken = fmt.Sprintf("e2e-%s-push-%d", alias, time.Now().UnixNano())
	body := map[string]any{"push_token": state.pushToken}
	if _, err := tc.doRequest("POST", "/v1/fcm-token", alias, body); err != nil {
		return err
	}
	if err := tc.assertStatus(http.StatusOK); err != nil {
		return err
	}
	return tc.assertJSONEquals("data.success", "true")
}

func (tc *testContext) pushTokenShouldBeStored(alias string) error {
	token := tc.userState(alias).pushToken
	if token == "" {
		return fmt.Errorf("no push token recorded for user %q", alias)
	}
	return tc.dbHasPushToken(token, alias)
}

// --- Legal acceptance ------------------------------------------------

func (tc *testContext) userOpensLegalStatus(alias string) error {
	if _, err := tc.doRequest("GET", "/v1/users/legal", alias, nil); err != nil {
		return err
	}
	return tc.assertStatus(http.StatusOK)
}

func (tc *testContext) userShouldSeeCurrentLegalVersions(alias string) error {
	if err := tc.userOpensLegalStatus(alias); err != nil {
		return err
	}
	if err := tc.assertJSONNotEmpty("data.terms_of_service_version"); err != nil {
		return err
	}
	return tc.assertJSONNotEmpty("data.privacy_policy_version")
}

func (tc *testContext) userAcceptsCurrentLegalDocuments(alias string) error {
	if err := tc.userOpensLegalStatus(alias); err != nil {
		return err
	}
	body := map[string]any{
		"terms_of_service_version": gjson.GetBytes(tc.lastBody, "data.terms_of_service_version").String(),
		"privacy_policy_version":   gjson.GetBytes(tc.lastBody, "data.privacy_policy_version").String(),
	}
	if _, err := tc.doRequest("POST", "/v1/users/legal/accept", alias, body); err != nil {
		return err
	}
	return tc.assertStatus(http.StatusNoContent)
}

func (tc *testContext) userShouldShowAcceptedLegalDocuments(alias string) error {
	if err := tc.userOpensLegalStatus(alias); err != nil {
		return err
	}
	if err := tc.assertJSONEquals("data.has_agreed_with_terms", "true"); err != nil {
		return err
	}
	return tc.assertJSONEquals("data.has_agreed_with_privacy_policy", "true")
}

// --- Account deletion ------------------------------------------------

func (tc *testContext) userDeletesTheirAccount(alias string) error {
	body := map[string]any{
		"reason":      "e2e-account-deletion",
		"description": "Testing account deletion flow",
	}
	if _, err := tc.doRequest("DELETE", "/v1/auth/delete", alias, body); err != nil {
		return err
	}
	// Invalidate the cached resolution so subsequent resolveUser calls
	// re-read from the DB and reflect the deleted state.
	tc.invalidateResolvedUser(alias)
	return tc.assertStatus(http.StatusNoContent)
}

func (tc *testContext) userShouldBeMarkedAsDeletedInDatabase(alias string) error {
	if err := tc.dbUserHasDeletedAt(alias); err != nil {
		return err
	}
	return tc.dbUserIsDeleted(alias, "true")
}

func (tc *testContext) deletedAccountShouldNoLongerBehaveLikeActiveUser() error {
	return tc.assertStatus(http.StatusUnauthorized)
}

// --- Edge cases ------------------------------------------------------

func (tc *testContext) guestRequestsProfile() error {
	_, err := tc.doRequest("GET", "/v1/users/profile", "", nil)
	return err
}

func (tc *testContext) requestShouldBeUnauthorized() error {
	return tc.assertStatus(http.StatusUnauthorized)
}
func (tc *testContext) requestShouldBeRejectedAsInvalid() error {
	return tc.assertStatus(http.StatusBadRequest)
}
func (tc *testContext) requestShouldBeRejectedAsNotFound() error {
	return tc.assertStatus(http.StatusNotFound)
}

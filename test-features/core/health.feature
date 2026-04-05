Feature: Public service checks
  As someone opening the app for the first time
  I need to know the service is alive and the public documents are reachable
  Before I sign in

  Scenario: Anyone can verify the service is online
    When the guest checks the service health
    Then the service should report online

  Scenario: Anyone can read the legal documents before signing in
    When the guest reads the public legal documents
    Then the public legal documents should be available

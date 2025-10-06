Feature: Webhook Forwarding
  As a Lambda Bridge service
  I want to forward ALB events to configured webhook endpoints
  So that downstream systems can process incoming requests

  Scenario: Successfully forward ALB event to webhook
    Given I have a webhook server listening
    When I receive an ALB event with payload:
      """
      {
        "event": "push",
        "repository": "test-repo"
      }
      """
    Then the Lambda should return status code 200 immediately
    And the response body should contain "accepted"
    And the webhook should receive the event within 1 second

  Scenario: Forward event with request ID tracking
    Given I have a webhook server listening
    When I receive an ALB event
    Then the Lambda should generate a unique request ID
    And the webhook should receive header "X-Request-ID"

  Scenario: Lambda returns immediately without blocking
    Given I have a slow webhook server that takes 2 seconds
    When I receive an ALB event
    Then the Lambda should return within 100 milliseconds

  Scenario: Forward to multiple webhook URLs
    Given I have 3 webhook servers listening
    When I receive an ALB event
    Then all 3 webhooks should receive the event

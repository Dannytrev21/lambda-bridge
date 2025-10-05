Feature: Webhook Forwarding
  As a Lambda Bridge service
  I want to forward ALB events to configured webhook endpoints
  So that downstream systems can process incoming requests

  Background:
    Given the Lambda Bridge is configured with webhook URLs

  Scenario: Successfully forward ALB event to webhook
    Given I have a webhook server listening
    When I receive an ALB event with payload:
      """
      {
        "event": "push",
        "repository": "test-repo",
        "commits": [{"id": "abc123", "message": "test commit"}]
      }
      """
    Then the Lambda should return status code 200 immediately
    And the response body should contain "accepted"
    And the webhook should receive the event within 1 second
    And the webhook should receive header "Content-Type" with value "application/json"
    And the webhook should receive header "User-Agent" with value "Lambda-Bridge/1.0"

  Scenario: Forward event with request ID tracking
    Given I have a webhook server listening
    When I receive an ALB event
    Then the Lambda should generate a unique request ID
    And the webhook should receive header "X-Request-ID"
    And the request ID should match pattern "req-[0-9a-f]+"

  Scenario: Lambda returns immediately without blocking
    Given I have a slow webhook server that takes 2 seconds
    When I receive an ALB event
    Then the Lambda should return within 100 milliseconds
    And the Lambda response should be status code 200

  Scenario: Forward to multiple webhook URLs
    Given I have 3 webhook servers listening
    And the Lambda is configured with all 3 webhook URLs
    When I receive an ALB event
    Then all 3 webhooks should receive the event
    And each webhook should receive the same payload

  Scenario: Handle empty webhook URLs gracefully
    Given the Lambda is configured with no webhook URLs
    When I receive an ALB event
    Then the Lambda should return status code 200
    And the response body should contain "no_webhooks_configured"

  Scenario: Preserve complete ALB event structure
    Given I have a webhook server listening
    When I receive an ALB event with:
      | field       | value                |
      | httpMethod  | POST                 |
      | path        | /webhook             |
      | body        | {"test":"data"}      |
    Then the webhook should receive all ALB event fields
    And the webhook payload should be valid JSON

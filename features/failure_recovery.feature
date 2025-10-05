Feature: Failure Recovery and Retry Logic
  As a Lambda Bridge service
  I want to handle webhook failures gracefully with retry logic
  So that temporary failures don't result in lost events

  Background:
    Given the Lambda Bridge is configured with retry enabled
    And maximum retries is set to 3

  Scenario: Retry on webhook server error (5xx)
    Given I have a webhook server that returns 503 on first 2 attempts
    And the webhook server returns 200 on attempt 3
    When I receive an ALB event
    Then the Lambda should return status code 200 immediately
    And the webhook should receive 3 attempts total
    And the final attempt should succeed with status 200

  Scenario: No retry on client error (4xx)
    Given I have a webhook server that always returns 400
    When I receive an ALB event
    Then the Lambda should return status code 200
    And the webhook should receive only 1 attempt
    And no retries should be made

  Scenario: Exponential backoff between retries
    Given I have a webhook server that fails with 500 errors
    When I receive an ALB event
    Then the webhook should receive 4 attempts (initial + 3 retries)
    And the delay between attempt 1 and 2 should be approximately 100 milliseconds
    And the delay between attempt 2 and 3 should be approximately 200 milliseconds
    And the delay between attempt 3 and 4 should be approximately 400 milliseconds

  Scenario: URL isolation during failures
    Given I have 3 webhook servers
    And webhook server 1 always fails with 500 errors
    And webhook server 2 is working correctly
    And webhook server 3 is working correctly
    When I receive an ALB event for all 3 webhooks
    Then webhook 1 should receive 4 attempts and fail
    And webhook 2 should receive 1 attempt and succeed
    And webhook 3 should receive 1 attempt and succeed
    And failing webhook should not block successful webhooks

  Scenario: Lambda doesn't block waiting for retries
    Given I have a webhook server that requires 3 retries to succeed
    When I receive an ALB event
    Then the Lambda should return within 100 milliseconds
    And retries should happen asynchronously in the background

  Scenario: Maximum retry limit respected
    Given I have a webhook server that always returns 503
    When I receive an ALB event
    Then the webhook should receive exactly 4 attempts
    And no additional attempts should be made
    And the system should log "Failed to forward after 3 retries"

  Scenario: Jitter in retry backoff
    Given I have a webhook server that fails with 500 errors
    When I receive 10 identical ALB events simultaneously
    Then retry timing should vary between events
    And not all retries should happen at exactly the same time
    And jitter should prevent thundering herd

  Scenario: Timeout handling
    Given I have a webhook server that times out after 10 seconds
    When I receive an ALB event
    Then the webhook request should timeout
    And the system should treat timeout as a retryable error
    And retry should be attempted

  Scenario: Success on final retry attempt
    Given I have a webhook server that fails 3 times then succeeds
    When I receive an ALB event
    Then all 4 attempts should be made
    And the final attempt should return status 200
    And the event should be marked as successfully delivered

  Scenario: Mixed success and failure across multiple URLs
    Given I have 5 webhook servers
    And 2 servers always succeed
    And 2 servers fail then succeed on retry
    And 1 server always fails
    When I receive an ALB event for all 5 webhooks
    Then the Lambda should return status code 200
    And successful servers should receive 1 attempt each
    And retry servers should receive 2-3 attempts each
    And failing server should receive 4 attempts and fail
    And the system should continue operating normally

  Scenario Outline: Retry behavior for different HTTP status codes
    Given I have a webhook server that returns status <status_code>
    When I receive an ALB event
    Then retries should be <retry_behavior>
    And total attempts should be <total_attempts>

    Examples:
      | status_code | retry_behavior | total_attempts |
      | 200         | not attempted  | 1              |
      | 201         | not attempted  | 1              |
      | 400         | not attempted  | 1              |
      | 401         | not attempted  | 1              |
      | 404         | not attempted  | 1              |
      | 500         | attempted      | 4              |
      | 502         | attempted      | 4              |
      | 503         | attempted      | 4              |
      | 504         | attempted      | 4              |

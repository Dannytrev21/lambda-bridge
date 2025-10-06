Feature: Failure Recovery and Retry Logic
  As a Lambda Bridge service
  I want to handle webhook failures gracefully with retry logic
  So that temporary failures don't result in lost events

  Scenario: Retry on webhook server error (5xx)
    Given I have a webhook server that returns 503 on first 2 attempts
    When I receive an ALB event
    Then the Lambda should return status code 200 immediately
    And the webhook should receive 3 attempts total

  Scenario: No retry on client error (4xx)
    Given I have a webhook server that always returns 400
    When I receive an ALB event
    Then the Lambda should return status code 200
    And the webhook should receive only 1 attempt

  Scenario: Maximum retry limit respected
    Given I have a webhook server that always returns 503
    When I receive an ALB event
    Then the webhook should receive exactly 4 attempts

  Scenario: Lambda returns immediately despite retries
    Given I have a webhook server that always returns 503
    When I receive an ALB event
    Then the Lambda should return within 100 milliseconds

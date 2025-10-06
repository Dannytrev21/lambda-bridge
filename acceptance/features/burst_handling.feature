Feature: Burst Handling and Concurrency
  As a Lambda Bridge service
  I want to handle bursts of concurrent requests efficiently
  So that the system remains responsive under high load

  Scenario: Handle moderate burst of requests
    Given I have a webhook server listening
    When I receive 50 concurrent ALB events
    Then all Lambda invocations should return within 200 milliseconds
    And at least 90% of events should be delivered to the webhook

  Scenario: Handle large burst within queue capacity
    Given I have a webhook server listening
    When I receive 100 concurrent ALB events
    Then all Lambda invocations should return status code 200 immediately
    And at least 90% of events should be delivered to the webhook

  Scenario: Lambda returns immediately without blocking
    Given I have a slow webhook server that takes 2 seconds
    When I receive an ALB event
    Then the Lambda should return within 100 milliseconds

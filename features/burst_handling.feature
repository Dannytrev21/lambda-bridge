Feature: Burst Handling and Concurrency
  As a Lambda Bridge service
  I want to handle bursts of concurrent requests efficiently
  So that the system remains responsive under high load

  Background:
    Given the Lambda Bridge has a worker pool with 25 workers
    And the work queue has capacity for 200 jobs

  Scenario: Handle moderate burst of requests
    Given I have a webhook server listening
    When I receive 50 concurrent ALB events
    Then all Lambda invocations should return within 200 milliseconds
    And all 50 events should eventually reach the webhook
    And the webhook should receive all events within 5 seconds

  Scenario: Handle large burst within queue capacity
    Given I have a webhook server listening
    When I receive 100 concurrent ALB events
    Then all Lambda invocations should return status code 200 immediately
    And at least 90% of events should be delivered to the webhook
    And no events should be blocked waiting for Lambda response

  Scenario: Queue overflow with excessive burst
    Given I have a slow webhook server that takes 500 milliseconds
    When I receive 250 concurrent ALB events
    Then all Lambda invocations should still return status code 200
    And some events may be dropped due to queue overflow
    But the Lambda should log "Queue full" warnings

  Scenario: Worker pool processes jobs concurrently
    Given I have a webhook server with 50 millisecond latency
    When I receive 25 ALB events simultaneously
    Then all 25 workers should process jobs in parallel
    And total processing time should be approximately 50 milliseconds
    And not 1250 milliseconds (25 × 50ms sequential)

  Scenario: Burst smoothing with queue buffering
    Given I have a webhook server listening
    When I receive a burst of 150 events over 1 second
    Then the Lambda should buffer events in the work queue
    And workers should process events at a steady rate
    And no Lambda invocations should be blocked

  Scenario: Queue size monitoring
    Given I have a webhook server listening
    And debug logging is enabled
    When I receive 10 concurrent ALB events
    Then debug logs should show queue size increasing
    And debug logs should show queue size decreasing as workers process jobs

  Scenario: Graceful degradation under extreme load
    Given I have a webhook server listening
    When the system is under extreme load with 500 events
    Then Lambda responses should remain under 200 milliseconds
    And events exceeding queue capacity should be dropped gracefully
    And the system should continue processing queued events

  Scenario: Fair processing across multiple webhook URLs
    Given I have 3 webhook servers listening
    When I receive 30 ALB events with different webhook URLs
    Then each webhook should receive approximately 10 events
    And no single webhook should monopolize worker pool

  Scenario Outline: Performance under various load levels
    Given I have a webhook server listening
    When I receive <num_events> concurrent ALB events
    Then at least <min_percentage>% should be processed successfully
    And average Lambda response time should be under <max_latency> milliseconds

    Examples:
      | num_events | min_percentage | max_latency |
      | 10         | 100            | 50          |
      | 50         | 100            | 100         |
      | 100        | 95             | 150         |
      | 200        | 90             | 200         |

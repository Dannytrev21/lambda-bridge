Feature: Health Check Filtering
  As a Lambda Bridge service
  I want to filter out ALB health checks
  So that webhook endpoints are not overwhelmed with health check traffic

  Background:
    Given the Lambda Bridge is configured with webhook URLs

  Scenario: Detect health check by user agent
    Given I have a webhook server listening
    When I receive an ALB event with header "User-Agent" = "ELB-HealthChecker/2.0"
    Then the Lambda should return status code 200
    And the response body should be "healthy"
    And the webhook should NOT receive any request

  Scenario: Detect health check by path /health
    Given I have a webhook server listening
    When I receive an ALB event to path "/health"
    Then the Lambda should return status code 200
    And the response body should be "healthy"
    And the webhook should NOT receive any request

  Scenario: Detect health check by path /healthz
    Given I have a webhook server listening
    When I receive an ALB event to path "/healthz"
    Then the Lambda should return status code 200
    And the response body should be "healthy"
    And the webhook should NOT receive any request

  Scenario: Detect health check by path /ping
    Given I have a webhook server listening
    When I receive an ALB event to path "/ping"
    Then the Lambda should return status code 200
    And the response body should be "healthy"
    And the webhook should NOT receive any request

  Scenario: Case-insensitive health check detection
    Given I have a webhook server listening
    When I receive an ALB event with header "user-agent" = "elb-healthchecker/2.0"
    Then the Lambda should return status code 200
    And the response body should be "healthy"
    And the webhook should NOT receive any request

  Scenario: Regular request should forward despite health check paths
    Given I have a webhook server listening
    When I receive an ALB event to path "/webhook"
    And the request has header "User-Agent" = "Mozilla/5.0"
    Then the webhook should receive the event
    And the Lambda should return "accepted" status

  Scenario Outline: Various health check paths
    Given I have a webhook server listening
    When I receive an ALB event to path "<path>"
    Then the webhook should NOT receive any request
    And the Lambda should return "healthy" response

    Examples:
      | path     |
      | /health  |
      | /healthz |
      | /ping    |
      | /HEALTH  |
      | /Health  |

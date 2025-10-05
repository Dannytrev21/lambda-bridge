Feature: Multi-Route Webhook Distribution
  As a Lambda Bridge service
  I want to route webhooks based on request headers
  So that different event types can be sent to different destinations

  Background:
    Given the Lambda Bridge is configured with:
      | route_type | webhook_url              |
      | default    | http://default.test      |
      | cloud      | http://cloud.test        |
      | enterprise | http://enterprise.test   |

  Scenario: Route to default webhooks
    Given I have a webhook server for "default" route
    When I receive an ALB event with no special headers
    Then the "default" webhook should receive the event
    And the "cloud" webhook should NOT receive any event
    And the "enterprise" webhook should NOT receive any event

  Scenario: Route to cloud webhooks based on header
    Given I have a webhook server for "cloud" route
    When I receive an ALB event with header "x-dcp-destination-host" = "cloud.example.com"
    Then the "cloud" webhook should receive the event
    And the "default" webhook should NOT receive any event
    And the "enterprise" webhook should NOT receive any event

  Scenario: Route to enterprise webhooks based on header
    Given I have a webhook server for "enterprise" route
    When I receive an ALB event with header "x-github-enterprise-host" = "github.enterprise.com"
    Then the "enterprise" webhook should receive the event
    And the "default" webhook should NOT receive any event
    And the "cloud" webhook should NOT receive any event

  Scenario: Case-insensitive header matching
    Given I have a webhook server for "cloud" route
    When I receive an ALB event with header "X-DCP-Destination-Host" = "cloud.example.com"
    Then the "cloud" webhook should receive the event

  Scenario: Priority of routing headers
    Given I have webhook servers for all routes
    When I receive an ALB event with both routing headers:
      | header                     | value                  |
      | x-dcp-destination-host     | cloud.example.com      |
      | x-github-enterprise-host   | github.enterprise.com  |
    Then the "cloud" webhook should receive the event
    And the "enterprise" webhook should NOT receive any event
    And cloud routing takes priority over enterprise

  Scenario: Fallback to default when cloud URLs not configured
    Given the Lambda Bridge has no cloud webhook URLs configured
    When I receive an ALB event with header "x-dcp-destination-host" = "cloud.example.com"
    Then the "default" webhook should receive the event

  Scenario: Multiple cloud webhooks
    Given the Lambda Bridge is configured with 2 cloud webhook URLs
    When I receive an ALB event with header "x-dcp-destination-host" = "cloud.example.com"
    Then both cloud webhooks should receive the event

  Scenario Outline: Route based on different destination headers
    Given I have webhook servers for all routes
    When I receive an ALB event with header "<header>" = "<value>"
    Then the "<expected_route>" webhook should receive the event

    Examples:
      | header                    | value                 | expected_route |
      | x-dcp-destination-host    | cloud.acme.com        | cloud          |
      | x-github-enterprise-host  | github.acme.com       | enterprise     |
      | content-type              | application/json      | default        |

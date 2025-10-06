Feature: Multi-Route Webhook Distribution
  As a Lambda Bridge service
  I want to route webhooks based on request headers
  So that different event types can be sent to different destinations

  Scenario: Route to default webhooks
    Given I have a webhook server for "default" route
    When I receive an ALB event with no special headers
    Then the "default" webhook should receive the event

  Scenario: Route to cloud webhooks based on header
    Given I have a webhook server for "cloud" route
    When I receive an ALB event with header "x-dcp-destination-host" = "cloud.example.com"
    Then the "cloud" webhook should receive the event

  Scenario: Route to enterprise webhooks based on header
    Given I have a webhook server for "enterprise" route
    When I receive an ALB event with header "x-github-enterprise-host" = "github.enterprise.com"
    Then the "enterprise" webhook should receive the event

  Scenario: Case-insensitive header matching
    Given I have a webhook server for "cloud" route
    When I receive an ALB event with header "X-DCP-Destination-Host" = "cloud.example.com"
    Then the "cloud" webhook should receive the event

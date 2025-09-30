package handler

import (
	"context"
	"encoding/json"
	"log"

	"github.com/aws/aws-lambda-go/events"

	configpkg "github.com/Dannytrev21/lambda-bridge/internal/config"
	"github.com/Dannytrev21/lambda-bridge/internal/forwarder"
)

type ForwarderHandler struct {
	snsForwarder forwarder.SNSForwarderInterface
	albProcessor *ALBProcessor
	config       *configpkg.Config
}

func NewForwarderHandler(
	snsForwarder forwarder.SNSForwarderInterface,
	webhookForwarder forwarder.WebhookForwarderInterface,
	config *configpkg.Config,
) *ForwarderHandler {
	albProcessor := NewALBProcessor(config, webhookForwarder)

	return &ForwarderHandler{
		snsForwarder: snsForwarder,
		albProcessor: albProcessor,
		config:       config,
	}
}

// Handler returns any type as required by Lambda runtime
// ALB events get ALBTargetGroupResponse, SNS events get error/nil
func (h *ForwarderHandler) Handler(ctx context.Context, rawEvent json.RawMessage) any {
	if h.config.Debug {
		log.Printf("Received raw event: %s", string(rawEvent))
	}

	// Try ALB event first (most common)
	var albEvent events.ALBTargetGroupRequest
	if err := json.Unmarshal(rawEvent, &albEvent); err == nil && albEvent.RequestContext.ELB.TargetGroupArn != "" {
		return h.albProcessor.Process(ctx, rawEvent, &albEvent)
	}

	// Try SNS event
	var snsEvent events.SNSEvent
	if err := json.Unmarshal(rawEvent, &snsEvent); err == nil && len(snsEvent.Records) > 0 && snsEvent.Records[0].SNS.MessageID != "" {
		if err := h.snsForwarder.Forward(ctx, h.config.SNSTopicArn, rawEvent); err != nil {
			log.Printf("SNS event handling failed: %v", err)
			return err
		}
		return nil
	}

	// Unknown event type - return safe ALB 200 response
	log.Printf("Unknown event type, returning safe ALB 200 response")
	return events.ALBTargetGroupResponse{
		StatusCode:      200,
		Headers:         map[string]string{"Content-Type": "application/json"},
		Body:            `{"status":"accepted"}`,
		IsBase64Encoded: false,
	}
}

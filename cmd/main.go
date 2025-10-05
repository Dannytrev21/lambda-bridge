package main

import (
	"context"
	"log"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sns"

	"github.com/Dannytrev21/lambda-bridge/internal/config"
	"github.com/Dannytrev21/lambda-bridge/internal/forwarder"
	"github.com/Dannytrev21/lambda-bridge/internal/handler"
)

func main() {
	// Load configuration
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("[FATAL] Failed to load config: %v", err)
	}

	// Setup AWS services
	snsClient, err := setupAWS()
	if err != nil {
		log.Fatalf("[FATAL] Failed to setup AWS: %v", err)
	}

	// Create handler
	snsForwarder := forwarder.NewSNSForwarder(snsClient)
	h := handler.NewHandler(cfg, snsForwarder)

	// Start Lambda handler
	log.Printf("[INFO] Lambda Bridge starting in %s environment", cfg.Environment)
	// Lambda manages the process lifecycle, so no shutdown handler needed
	lambda.Start(h.Handle)
}

func loadConfig() (*config.Config, error) {
	// Get environment from env vars
	environment := os.Getenv("ENV")
	if environment == "" {
		environment = os.Getenv("ENVIRONMENT")
	}
	if environment == "" {
		environment = "dev" // Default
	}

	return config.Load(environment)
}

func setupAWS() (*sns.Client, error) {
	ctx := context.Background()
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	return sns.NewFromConfig(cfg), nil
}

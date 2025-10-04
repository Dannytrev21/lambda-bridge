package handler

import (
	"github.com/Dannytrev21/lambda-bridge/internal/forwarder"
)

// DefaultWorkerPoolFactory implements WorkerPoolFactory.
type DefaultWorkerPoolFactory struct {
	forwarder forwarder.WebhookForwarderInterface
}

// NewDefaultWorkerPoolFactory creates a new default worker pool factory.
func NewDefaultWorkerPoolFactory(forwarder forwarder.WebhookForwarderInterface) *DefaultWorkerPoolFactory {
	return &DefaultWorkerPoolFactory{
		forwarder: forwarder,
	}
}

// CreatePool creates a new BotWorkerPool instance.
func (f *DefaultWorkerPoolFactory) CreatePool(url string, queueSize int) WorkerPool {
	return NewBotWorkerPool(url, f.forwarder, queueSize)
}

package solace

import (
	"context"

	"github.com/Azure/go-amqp"
)

// solaceConnection is an interface for amqp.Client, to allow mocking.
type solaceConnection interface {
	NewSession(ctx context.Context, opts *amqp.SessionOptions) (solaceSession, error)
	Close() error // Corrected: removed ctx
}

// solaceSession is an interface for amqp.Session, to allow mocking.
type solaceSession interface {
	NewSender(ctx context.Context, target string, opts *amqp.SenderOptions) (solaceSender, error)
	NewReceiver(ctx context.Context, source string, opts *amqp.ReceiverOptions) (solaceReceiver, error)
	Close() error // Corrected: removed ctx
}

// solaceSender is an interface for amqp.Sender, to allow mocking.
type solaceSender interface {
	Send(ctx context.Context, msg *amqp.Message, opts *amqp.SendOptions) error
	Close() error // Corrected: removed ctx
}

// solaceReceiver is an interface for amqp.Receiver, to allow mocking.
type solaceReceiver interface {
	Receive(ctx context.Context, opts *amqp.ReceiveOptions) (*amqp.Message, error)
	AcceptMessage(ctx context.Context, msg *amqp.Message) error // Added
	Close() error                                               // Corrected: removed ctx
}

// This interface is used by NewSolaceCheckerWithOpenSolaceFunc for dependency injection
type openSolaceConnectionFunc func(connectionString string) (solaceConnection, error)

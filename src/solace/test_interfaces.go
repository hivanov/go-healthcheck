package solace

import (
	"context"

	"github.com/Azure/go-amqp"
)

// solaceConnection interface for mocking *amqp.Conn in tests.
type solaceConnection interface {
	Close() error
	NewSession(ctx context.Context, opts *amqp.SessionOptions) (solaceSession, error)
}

// solaceSession interface for mocking *amqp.Session in tests.
type solaceSession interface {
	NewSender(ctx context.Context, target string, opts *amqp.SenderOptions) (solaceSender, error)
	NewReceiver(ctx context.Context, source string, opts *amqp.ReceiverOptions) (solaceReceiver, error)
	Close() error
}

// solaceSender interface for mocking *amqp.Sender in tests.
type solaceSender interface {
	Send(ctx context.Context, msg *amqp.Message, opts *amqp.SendOptions) error
	Close() error
}

// solaceReceiver interface for mocking *amqp.Receiver in tests.
type solaceReceiver interface {
	Receive(ctx context.Context, opts *amqp.ReceiveOptions) (*amqp.Message, error)
	AcceptMessage(ctx context.Context, msg *amqp.Message) error
	Close() error
}

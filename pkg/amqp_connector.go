// Copyright (C) 2017-2019 Jan Delgado

package rabtap

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/url"
)

// ReconnectAction signals if connection should be reconnected or not.
type ReconnectAction int

const (
	// doNotReconnect signals caller of worker func not to reconnect
	doNotReconnect ReconnectAction = iota
	// doReconnect signals caller of worker func to reconnect
	doReconnect
)

func (s ReconnectAction) shouldReconnect() bool {
	return s == doReconnect
}

// An AmqpWorkerFunc does the actual work after the connection is established.
// If the worker returns true, the caller should re-connect to the broker.  If
// the worker returne false, the caller should finish its processing.  The
// worker must return with NoReconnect if a ShutdownMessage is received via
// shutdownChan, otherwise with Reconnect.
type AmqpWorkerFunc func(ctx context.Context, session Session) (ReconnectAction, error)

// AmqpConnector manages the connection to the amqp broker and automatically
// reconnects after connections losses
type AmqpConnector struct {
	logger    Logger
	url       *url.URL
	tlsConfig *tls.Config
}

// NewAmqpConnector creates a new AmqpConnector object.
func NewAmqpConnector(url *url.URL, tlsConfig *tls.Config, logger Logger) *AmqpConnector {
	return &AmqpConnector{
		logger:    logger,
		url:       url,
		tlsConfig: tlsConfig,
	}
}

// Connect  (re-)establishes the connection to RabbitMQ broker.
func (s *AmqpConnector) Connect(ctx context.Context, worker AmqpWorkerFunc) error {
	sessions := redial(ctx, s.url.String(), s.tlsConfig, s.logger, FailEarly)
	for session := range sessions {
		s.logger.Debugf("waiting for new session on %+v", s.url.Redacted())
		sub, more := <-session
		if !more {
			// closed. TODO propagate errors from redial()
			return errors.New("session factory closed")
		}
		s.logger.Debugf("got new amqp session ...")
		action, err := worker(ctx, sub)
		if err != nil && action.shouldReconnect() {
			s.logger.Errorf("worker failed with: %v", err)
		}
		if !action.shouldReconnect() {
			if errClose := sub.Connection.Close(); errClose != nil {
				return errors.Join(err, fmt.Errorf("connection close failed: %v", errClose))
			}
			return err
		}
	}
	return nil
}
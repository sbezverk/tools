package telemetry_feeder

import (
	"errors"
)

var (
	ErrUnmarshalTelemetryMsg = errors.New("failed to unmarshal telemetry message")
	ErrReceiveTelemetryMsg   = errors.New("failed to receive telemetry message")
)

type Feed struct {
	TelemetryMsg []byte
	Err          error
}

type Feeder interface {
	GetFeed() chan *Feed
	Stop()
}

package telemetry_feeder

import (
	"errors"

	"github.com/sbezverk/tools/telemetry_feeder/proto/telemetry"
)

var (
	ErrUnmarshalTelemetryMsg = errors.New("failed to unmarshal telemetry message")
	ErrReceiveTelemetryMsg   = errors.New("failed to receive telemetry message")
)

type Feed struct {
	TelemetryMsg *telemetry.Telemetry
	Err          error
}

type Feeder interface {
	GetFeed() chan *Feed
	Stop()
}

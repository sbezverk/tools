package offline_feeder

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/golang/glog"
	feeder "github.com/sbezverk/tools/telemetry_feeder"
)

type offFeeder struct {
	file *os.File
	feed chan *feeder.Feed
	stop chan struct{}
}

func (o *offFeeder) GetFeed() chan *feeder.Feed {
	return o.feed
}

func (o *offFeeder) retrieve() {
	ticker := time.NewTicker(time.Second * 1)
	for {
		select {
		case <-o.stop:
			ticker.Stop()
			return
		case <-ticker.C:
			lb := make([]byte, 4)
			if _, err := o.file.Read(lb); err != nil {
				if err == io.EOF {
					glog.Info("processing offline telemetry file completed")
					return
				}
				glog.Errorf("failed to read length of the record with error: %+v", err)
				return
			}
			l := binary.BigEndian.Uint32(lb)
			glog.Infof("Expected record length %d", l)
			b := make([]byte, l)
			if _, err := o.file.Read(b); err != nil {
				if err == io.EOF {
					glog.Info("processing offline telemetry file completed")
					return
				}
				glog.Errorf("failed to read the record with error: %+v", err)
				return
			}
			f := &feeder.Feed{}
			f.TelemetryMsg = make([]byte, len(b))
			copy(f.TelemetryMsg, b)
			f.Err = nil
			// Sending recieved Telemetry message for processing
			o.feed <- f
		}
	}
}

func (o *offFeeder) Stop() {
	o.file.Close()
}

func New(fn string) (feeder.Feeder, error) {
	f, err := os.Open(fn)
	if err != nil {
		return nil, fmt.Errorf("failed to open offline telemetry file %s with error: %+v", fn, err)
	}
	o := &offFeeder{
		feed: make(chan *feeder.Feed),
		stop: make(chan struct{}),
		file: f,
	}
	o.retrieve()

	return o, nil
}

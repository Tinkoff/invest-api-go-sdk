package investgo

import (
	"github.com/golang/protobuf/ptypes/timestamp"
	"time"
)

// TimeToTimestamp - convert time.Time to *timestamp.Timestamp
func TimeToTimestamp(t time.Time) *timestamp.Timestamp {
	return &timestamp.Timestamp{
		Seconds: t.Unix(),
		Nanos:   0,
	}
}

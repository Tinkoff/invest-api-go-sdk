package investgo

import (
	"github.com/golang/protobuf/ptypes/timestamp"
	"google.golang.org/protobuf/types/known/timestamppb"
	"time"
)

// TimeToTimestamp - convert time.Time to *timestamp.Timestamp
func TimeToTimestamp(t time.Time) *timestamp.Timestamp {
	return timestamppb.New(t)
}

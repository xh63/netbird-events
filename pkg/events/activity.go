package events

import (
	"github.com/xh63/netbird-events/pkg/activity"
)

// EnrichActivityInfo enriches an event with human-readable activity name and code
func EnrichActivityInfo(evt *Event) {
	act := activity.Activity(evt.Activity)
	evt.ActivityName = act.Message()
	evt.ActivityCode = act.StringCode()
}

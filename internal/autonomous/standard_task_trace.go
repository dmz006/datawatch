// standard_task_trace.go — pack attribution for the built-in standard tasks.
package autonomous

import (
	"github.com/dmz006/datawatch/internal/config"
	"github.com/dmz006/datawatch/internal/trace"
)

// BuiltinPackName is the pack_name carried by trace events for the
// built-in standard tasks.
const BuiltinPackName = "builtin"

// StampBuiltinPack attributes ev to the built-in pack: it sets pack_name to
// "builtin" and pack_version to version. A nil event is ignored.
func StampBuiltinPack(ev *trace.Event, version string) {
	if ev == nil {
		return
	}
	ev.PackName = BuiltinPackName
	ev.PackVersion = version
}

// BuiltinStandardTaskTraceEvent returns a trace event for the built-in
// standard task d, attributed to the built-in pack at version. The event's
// TaskID and Name come from the task itself, so each task's event is tied to
// that task rather than being a generic pack stamp.
func BuiltinStandardTaskTraceEvent(d config.TaskTemplateDef, version string) trace.Event {
	ev := trace.Event{TaskID: d.ID, Name: d.Title}
	StampBuiltinPack(&ev, version)
	return ev
}

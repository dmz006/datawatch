// Installs the lifecycle event kind registry check into the supervisor loop
// settings, so supervisor_loop.wake.kinds accepts exactly the kinds a
// subscription may filter on. The supervisorloop package cannot import this
// one, so the check is injected here at start-up.

package autonomous

import "github.com/dmz006/datawatch/internal/supervisorloop"

func init() { supervisorloop.SetEventKindChecker(IsKnownEventKind) }

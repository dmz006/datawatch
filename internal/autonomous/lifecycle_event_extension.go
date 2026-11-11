// Extension registration for lifecycle event kinds and sink types. A plugin or
// another user of the package registers its own kinds and sinks at start-up
// instead of editing the built-in list. Registered kinds appear in
// IsKnownEventKind and ListEventKinds; registered sinks are accepted by
// ValidateSubscription.

package autonomous

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Limits for extension names and descriptions.
const (
	MaxExtensionNameLen        = 64
	MaxExtensionDescriptionLen = 200
	MaxExtensionSinkTargetLen  = 512
)

var extensionNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`)

var (
	extensionMu    sync.RWMutex
	extensionKinds = map[string]EventKindInfo{}
	extensionSinks = map[string]func(SubscriptionSink) error{}
)

func validExtensionName(name string) error {
	switch {
	case name == "":
		return fmt.Errorf("name is required")
	case len(name) > MaxExtensionNameLen:
		return fmt.Errorf("name is %d characters; limit is %d", len(name), MaxExtensionNameLen)
	case !extensionNameRe.MatchString(name):
		return fmt.Errorf("name %q must be lowercase letters, digits and underscores, with dots between parts", name)
	}
	return nil
}

// RegisterEventKind adds an event kind to the registry. The name follows the
// built-in rules, must not collide with a built-in or earlier registration,
// and the description must be one non-empty line in a valid group.
func RegisterEventKind(info EventKindInfo) error {
	if err := validExtensionName(info.Kind); err != nil {
		return fmt.Errorf("event kind: %w", err)
	}
	desc := strings.TrimSpace(info.Description)
	switch {
	case desc == "":
		return fmt.Errorf("event kind %q: a description is required", info.Kind)
	case strings.ContainsAny(desc, "\r\n"):
		return fmt.Errorf("event kind %q: the description must be one line", info.Kind)
	case len(desc) > MaxExtensionDescriptionLen:
		return fmt.Errorf("event kind %q: description is %d characters; limit is %d", info.Kind, len(desc), MaxExtensionDescriptionLen)
	case !validEventGroup(info.Group):
		return fmt.Errorf("event kind %q: unknown group %q", info.Kind, info.Group)
	}
	for _, e := range eventKindRegistry {
		if e.Kind == info.Kind {
			return fmt.Errorf("event kind %q is a built-in kind", info.Kind)
		}
	}
	extensionMu.Lock()
	defer extensionMu.Unlock()
	if _, dup := extensionKinds[info.Kind]; dup {
		return fmt.Errorf("event kind %q is already registered", info.Kind)
	}
	info.Description = desc
	extensionKinds[info.Kind] = info
	return nil
}

// RegisterSinkType adds a sink type that ValidateSubscription accepts. validate
// may be nil. A secret_ref is refused unless validate is set and accepts it.
func RegisterSinkType(name string, validate func(SubscriptionSink) error) error {
	if err := validExtensionName(name); err != nil {
		return fmt.Errorf("sink type: %w", err)
	}
	switch name {
	case SinkComm, SinkSession, SinkTopic, SinkAlert, SinkMCPQueue, SinkWebhook, SinkPeer:
		return fmt.Errorf("sink type %q is a built-in sink", name)
	}
	extensionMu.Lock()
	defer extensionMu.Unlock()
	if _, dup := extensionSinks[name]; dup {
		return fmt.Errorf("sink type %q is already registered", name)
	}
	extensionSinks[name] = validate
	return nil
}

func isExtensionEventKind(k string) bool {
	extensionMu.RLock()
	defer extensionMu.RUnlock()
	_, ok := extensionKinds[k]
	return ok
}

func extensionEventKinds() []EventKindInfo {
	extensionMu.RLock()
	defer extensionMu.RUnlock()
	out := make([]EventKindInfo, 0, len(extensionKinds))
	for _, e := range extensionKinds {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out
}

// validateExtensionSink applies the structure checks to a registered sink.
// found is false when the type is not registered.
func validateExtensionSink(sink SubscriptionSink) (found bool, err error) {
	extensionMu.RLock()
	validate, ok := extensionSinks[sink.Type]
	extensionMu.RUnlock()
	if !ok {
		return false, nil
	}
	if len(sink.Target) > MaxExtensionSinkTargetLen {
		return true, fmt.Errorf("sink target is %d characters; limit is %d", len(sink.Target), MaxExtensionSinkTargetLen)
	}
	if sink.SecretRef != "" && validate == nil {
		return true, fmt.Errorf("sink secret_ref is not valid with the %q sink", sink.Type)
	}
	if validate != nil {
		if err := validate(sink); err != nil {
			return true, err
		}
	}
	return true, nil
}

func isExtensionSink(name string) bool {
	extensionMu.RLock()
	defer extensionMu.RUnlock()
	_, ok := extensionSinks[name]
	return ok
}

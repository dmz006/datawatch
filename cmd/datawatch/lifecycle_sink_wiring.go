// Daemon side of the built-in lifecycle sinks: the alert-store write and
// the push-topic publish that autonomous.InstallLifecycleSinks calls for
// every allowed Automaton lifecycle event.

package main

import (
	"slices"

	alertspkg "github.com/dmz006/datawatch/internal/alerts"
	"github.com/dmz006/datawatch/internal/server"
)

// lifecycleAlertSink writes one lifecycle event to the alert store. level
// is "info", "warn" or "error". The alert is marked so the daemon's alert
// listener does not send it to the comm channels: the lifecycle comm
// fan-out sends the event's text, coalesced.
func lifecycleAlertSink(store *alertspkg.Store) func(level, title, body string) {
	return func(level, title, body string) {
		lv := alertspkg.LevelInfo
		switch level {
		case "warn":
			lv = alertspkg.LevelWarn
		case "error":
			lv = alertspkg.LevelError
		}
		_ = store.AddSkipCommFanout(lv, title, body)
	}
}

// lifecycleTopicSink publishes one lifecycle event to its topics in one
// call. publish (server.PublishToTopics) reaches the subscribers of every
// topic but dials each registered mobile endpoint once, for the last
// topic. The order is reversed so that topic is the global "alerts" feed.
func lifecycleTopicSink(publish func(topics []string, ev server.PushEvent)) func(topics []string, title, message string) {
	return func(topics []string, title, message string) {
		ordered := slices.Clone(topics)
		slices.Reverse(ordered)
		// ID/Time defaults are filled in by the publisher (dw-<unixnano> /
		// unix seconds, Event "message") — same as the other in-process
		// producers.
		publish(ordered, server.PushEvent{
			Title:   title,
			Message: message,
			Tags:    []string{"lifecycle"},
		})
	}
}

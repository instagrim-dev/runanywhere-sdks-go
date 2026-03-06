package device

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// =============================================================================
// Event Category
// =============================================================================

// EventCategory classifies events for subscription filtering.
type EventCategory string

const (
	EventCategorySDK     EventCategory = "sdk"
	EventCategoryModel   EventCategory = "model"
	EventCategoryLLM     EventCategory = "llm"
	EventCategorySTT     EventCategory = "stt"
	EventCategoryTTS     EventCategory = "tts"
	EventCategoryDevice  EventCategory = "device"
	EventCategoryNetwork EventCategory = "network"
	EventCategoryError   EventCategory = "error"
)

// =============================================================================
// Event Interface
// =============================================================================

// Event is the interface all SDK events implement.
type Event interface {
	EventType() string
	EventCategory() EventCategory
	EventTimestamp() int64 // Unix millis
	EventProperties() map[string]string
}

// =============================================================================
// Base Event
// =============================================================================

// BaseEvent provides a reusable Event implementation for embedding.
type BaseEvent struct {
	Type       string
	Category   EventCategory
	Timestamp  int64
	Properties map[string]string
}

// EventType returns the event type string.
func (e *BaseEvent) EventType() string { return e.Type }

// EventCategory returns the event category.
func (e *BaseEvent) EventCategory() EventCategory { return e.Category }

// EventTimestamp returns the event timestamp in Unix millis.
func (e *BaseEvent) EventTimestamp() int64 { return e.Timestamp }

// EventProperties returns the event properties map.
func (e *BaseEvent) EventProperties() map[string]string { return e.Properties }

// =============================================================================
// Concrete Event Types
// =============================================================================

// LifecycleEvent represents SDK lifecycle transitions (initialized, shutdown).
type LifecycleEvent struct {
	BaseEvent
	LifecycleType string // "initialized" or "shutdown"
}

// DeviceErrorEvent represents an error that occurred during a device operation.
type DeviceErrorEvent struct {
	BaseEvent
	Code      ErrorCode
	Message   string
	Operation string
}

// =============================================================================
// Event Constructor Helpers
// =============================================================================

// NewLifecycleEvent creates a LifecycleEvent with the current timestamp.
func NewLifecycleEvent(lifecycleType string) *LifecycleEvent {
	return &LifecycleEvent{
		BaseEvent: BaseEvent{
			Type:      "sdk." + lifecycleType,
			Category:  EventCategorySDK,
			Timestamp: time.Now().UnixMilli(),
		},
		LifecycleType: lifecycleType,
	}
}

// NewDeviceErrorEvent creates a DeviceErrorEvent with the current timestamp.
func NewDeviceErrorEvent(operation string, code ErrorCode, message string) *DeviceErrorEvent {
	return &DeviceErrorEvent{
		BaseEvent: BaseEvent{
			Type:      "error.occurred",
			Category:  EventCategoryError,
			Timestamp: time.Now().UnixMilli(),
			Properties: map[string]string{
				"operation":  operation,
				"error_code": string(code),
			},
		},
		Code:      code,
		Message:   message,
		Operation: operation,
	}
}

// =============================================================================
// Subscription
// =============================================================================

// EventHandler is the callback type for event subscribers.
type EventHandler func(Event)

// Subscription represents an active event subscription. Pass to Unsubscribe to cancel.
type Subscription struct {
	id       uint64
	category EventCategory // empty string means "all"
}

// eventRegistration pairs a subscription with its handler.
type eventRegistration struct {
	sub     *Subscription
	handler EventHandler
}

// =============================================================================
// Global Event Bus (same pattern as metrics.go)
// =============================================================================

var (
	eventMu       sync.RWMutex
	eventHandlers []eventRegistration
	nextSubID     atomic.Uint64
)

// Subscribe registers a handler for events of the given category.
// Returns a Subscription that can be passed to Unsubscribe.
func Subscribe(category EventCategory, handler EventHandler) *Subscription {
	sub := &Subscription{
		id:       nextSubID.Add(1),
		category: category,
	}
	eventMu.Lock()
	defer eventMu.Unlock()
	eventHandlers = append(eventHandlers, eventRegistration{sub: sub, handler: handler})
	return sub
}

// SubscribeAll registers a handler for all event categories.
func SubscribeAll(handler EventHandler) *Subscription {
	sub := &Subscription{
		id:       nextSubID.Add(1),
		category: "", // empty = all
	}
	eventMu.Lock()
	defer eventMu.Unlock()
	eventHandlers = append(eventHandlers, eventRegistration{sub: sub, handler: handler})
	return sub
}

// Unsubscribe removes a subscription. Safe to call with nil.
func Unsubscribe(sub *Subscription) {
	if sub == nil {
		return
	}
	eventMu.Lock()
	defer eventMu.Unlock()
	for i, reg := range eventHandlers {
		if reg.sub.id == sub.id {
			eventHandlers = append(eventHandlers[:i], eventHandlers[i+1:]...)
			return
		}
	}
}

// Publish delivers an event to all matching subscribers synchronously.
// Handlers must be non-blocking. A panicking handler is recovered and logged
// so it cannot break delivery to subsequent subscribers.
func Publish(event Event) {
	if event == nil {
		return
	}
	cat := event.EventCategory()

	eventMu.RLock()
	// Copy the slice header so we can release the lock before calling handlers.
	handlers := make([]eventRegistration, len(eventHandlers))
	copy(handlers, eventHandlers)
	eventMu.RUnlock()

	for _, reg := range handlers {
		if reg.sub.category == "" || reg.sub.category == cat {
			callHandler(reg.handler, event)
		}
	}
}

// callHandler invokes a handler with panic isolation.
func callHandler(handler EventHandler, event Event) {
	defer func() {
		if r := recover(); r != nil {
			LogCore.Error("event handler panicked", fmt.Errorf("%v", r), map[string]string{
				"event_type": event.EventType(),
			})
		}
	}()
	handler(event)
}

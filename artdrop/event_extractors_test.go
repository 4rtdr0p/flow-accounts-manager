package artdrop

import (
	"testing"

	"github.com/onflow/cadence"
	"github.com/onflow/flow-go-sdk"
)

// newTestEvent builds a synthetic flow.Event whose qualified type is
// "A.<contractAddr>.ArtDropCore.<name>" and whose cadence.Event.Value carries
// fields (in the given order — cadence.Event maps values to Field
// identifiers positionally, see cadence.FieldsMappedByName).
func newTestEvent(name string, fields map[string]cadence.Value, order []string) flow.Event {
	cadenceFields := make([]cadence.Field, 0, len(order))
	values := make([]cadence.Value, 0, len(order))
	for _, k := range order {
		cadenceFields = append(cadenceFields, cadence.NewField(k, cadence.UInt64Type))
		values = append(values, fields[k])
	}

	eventType := cadence.NewEventType(
		nil,
		"ArtDropCore."+name,
		cadenceFields,
		nil,
	)

	evt := cadence.NewEvent(values).WithType(eventType)

	return flow.Event{
		Type:  "A.ec581a0282d99a1a.ArtDropCore." + name,
		Value: evt,
	}
}

func TestExtractOriginalCreatedResult(t *testing.T) {
	t.Run("returns the id as JSON", func(t *testing.T) {
		events := []flow.Event{
			newTestEvent("OriginalCreated", map[string]cadence.Value{
				"id": cadence.NewUInt64(42),
			}, []string{"id"}),
		}

		got, err := extractOriginalCreatedResult(events)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want := `{"originalId":42}`
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("ignores unrelated events and finds OriginalCreated among them", func(t *testing.T) {
		events := []flow.Event{
			newTestEvent("SomeOtherEvent", nil, nil),
			newTestEvent("OriginalCreated", map[string]cadence.Value{
				"id": cadence.NewUInt64(7),
			}, []string{"id"}),
		}

		got, err := extractOriginalCreatedResult(events)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want := `{"originalId":7}`
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("errors when OriginalCreated is not among the events", func(t *testing.T) {
		events := []flow.Event{
			newTestEvent("SomeOtherEvent", nil, nil),
		}

		if _, err := extractOriginalCreatedResult(events); err == nil {
			t.Fatal("expected an error, got nil")
		}
	})

	t.Run("errors when id is missing or wrong-typed", func(t *testing.T) {
		events := []flow.Event{
			newTestEvent("OriginalCreated", map[string]cadence.Value{
				"name": cadence.String("no id field here"),
			}, []string{"name"}),
		}

		if _, err := extractOriginalCreatedResult(events); err == nil {
			t.Fatal("expected an error, got nil")
		}
	})
}

func TestExtractEditionCreatedResult(t *testing.T) {
	t.Run("returns editionId and originalId as JSON", func(t *testing.T) {
		events := []flow.Event{
			newTestEvent("EditionCreated", map[string]cadence.Value{
				"id":         cadence.NewUInt64(7),
				"originalId": cadence.NewUInt64(42),
			}, []string{"id", "originalId"}),
		}

		got, err := extractEditionCreatedResult(events)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want := `{"editionId":7,"originalId":42}`
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	})

	t.Run("errors when EditionCreated is not among the events", func(t *testing.T) {
		events := []flow.Event{
			newTestEvent("SomeOtherEvent", nil, nil),
		}

		if _, err := extractEditionCreatedResult(events); err == nil {
			t.Fatal("expected an error, got nil")
		}
	})

	t.Run("errors when originalId is missing or wrong-typed", func(t *testing.T) {
		events := []flow.Event{
			newTestEvent("EditionCreated", map[string]cadence.Value{
				"id": cadence.NewUInt64(7),
			}, []string{"id"}),
		}

		if _, err := extractEditionCreatedResult(events); err == nil {
			t.Fatal("expected an error, got nil")
		}
	})
}

package mergeshadow

import (
	"context"
	"testing"
	"time"
)

func TestOneHolderOfTheSlot(t *testing.T) {
	held, release, err := AcquireSlot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// What runs under the holder does not wait for itself.
	_, again, err := AcquireSlot(held)
	if err != nil {
		t.Fatal(err)
	}
	again()
	short, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, _, err := AcquireSlot(short); err == nil {
		t.Fatal("a second shadow got the slot")
	}
	release()
	release()
	// A context kept after the release holds nothing.
	_, other, err := AcquireSlot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	short2, cancel2 := context.WithTimeout(held, 50*time.Millisecond)
	defer cancel2()
	if _, _, err := AcquireSlot(short2); err == nil {
		t.Error("a released context passed for the holder")
	}
	other()
}

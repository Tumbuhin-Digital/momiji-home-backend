package preorderbatch

import (
	"errors"
	"testing"

	"gorm.io/gorm"
)

func TestApplyReorder_PromoteQueuedOverActive(t *testing.T) {
	batches := []PreorderBatch{
		{ID: "jan", Name: "January", Sequence: 1, Status: StatusClosed},
		{ID: "sep", Name: "September", Sequence: 2, Status: StatusActive},
		{ID: "feb", Name: "February", Sequence: 3, Status: StatusClosed},
		{ID: "dec", Name: "December", Sequence: 4, Status: StatusQueued},
	}

	reordered, moved, err := applyReorder(batches, "dec", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if moved.ID != "dec" {
		t.Fatalf("expected moved batch dec, got %s", moved.ID)
	}
	if moved.Status != StatusActive {
		t.Fatalf("expected dec to become active, got %s", moved.Status)
	}

	wantIDs := []string{"jan", "dec", "sep", "feb"}
	wantStatus := []string{StatusClosed, StatusActive, StatusQueued, StatusClosed}
	for i, batch := range reordered {
		if batch.ID != wantIDs[i] {
			t.Fatalf("index %d: expected id %s, got %s", i, wantIDs[i], batch.ID)
		}
		if batch.Sequence != i+1 {
			t.Fatalf("index %d: expected sequence %d, got %d", i, i+1, batch.Sequence)
		}
		if batch.Status != wantStatus[i] {
			t.Fatalf("index %d: expected status %s, got %s", i, wantStatus[i], batch.Status)
		}
	}
}

func TestApplyReorder_SwapOpenBatches(t *testing.T) {
	batches := []PreorderBatch{
		{ID: "a", Sequence: 1, Status: StatusActive},
		{ID: "b", Sequence: 2, Status: StatusQueued},
		{ID: "c", Sequence: 3, Status: StatusQueued},
	}

	reordered, _, err := applyReorder(batches, "c", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantIDs := []string{"c", "a", "b"}
	wantStatus := []string{StatusActive, StatusQueued, StatusQueued}
	for i, batch := range reordered {
		if batch.ID != wantIDs[i] || batch.Status != wantStatus[i] || batch.Sequence != i+1 {
			t.Fatalf("index %d: got id=%s status=%s seq=%d", i, batch.ID, batch.Status, batch.Sequence)
		}
	}
}

func TestApplyReorder_RejectClosed(t *testing.T) {
	batches := []PreorderBatch{
		{ID: "closed", Sequence: 1, Status: StatusClosed},
		{ID: "active", Sequence: 2, Status: StatusActive},
	}

	_, _, err := applyReorder(batches, "closed", 2)
	if !errors.Is(err, gorm.ErrInvalidData) {
		t.Fatalf("expected ErrInvalidData, got %v", err)
	}
}

func TestApplyReorder_RejectCancelled(t *testing.T) {
	batches := []PreorderBatch{
		{ID: "cancelled", Sequence: 1, Status: StatusCancelled},
		{ID: "active", Sequence: 2, Status: StatusActive},
	}

	_, _, err := applyReorder(batches, "cancelled", 1)
	if !errors.Is(err, gorm.ErrInvalidData) {
		t.Fatalf("expected ErrInvalidData, got %v", err)
	}
}

func TestApplyReorder_ClampSequence(t *testing.T) {
	batches := []PreorderBatch{
		{ID: "a", Sequence: 1, Status: StatusActive},
		{ID: "b", Sequence: 2, Status: StatusQueued},
	}

	reorderedHigh, movedHigh, err := applyReorder(batches, "a", 99)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if movedHigh.ID != "a" || movedHigh.Sequence != 2 {
		t.Fatalf("expected a at sequence 2, got id=%s seq=%d", movedHigh.ID, movedHigh.Sequence)
	}
	if reorderedHigh[0].ID != "b" || reorderedHigh[0].Status != StatusActive {
		t.Fatalf("expected b to become active head, got %+v", reorderedHigh[0])
	}

	reorderedLow, movedLow, err := applyReorder(batches, "b", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if movedLow.ID != "b" || movedLow.Sequence != 1 || movedLow.Status != StatusActive {
		t.Fatalf("expected b active at sequence 1, got id=%s seq=%d status=%s", movedLow.ID, movedLow.Sequence, movedLow.Status)
	}
	if reorderedLow[1].ID != "a" || reorderedLow[1].Status != StatusQueued {
		t.Fatalf("expected a queued second, got %+v", reorderedLow[1])
	}
}

func TestApplyReorder_SamePositionStillSyncs(t *testing.T) {
	batches := []PreorderBatch{
		{ID: "a", Sequence: 1, Status: StatusQueued},
		{ID: "b", Sequence: 2, Status: StatusActive},
	}

	reordered, moved, err := applyReorder(batches, "b", 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if moved.ID != "b" {
		t.Fatalf("expected moved b, got %s", moved.ID)
	}
	if reordered[0].Status != StatusActive || reordered[1].Status != StatusQueued {
		t.Fatalf("expected statuses synced to sequence order, got %s then %s", reordered[0].Status, reordered[1].Status)
	}
}

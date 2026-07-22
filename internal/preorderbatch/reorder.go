package preorderbatch

import "gorm.io/gorm"

// applyReorder moves batchID to a 1-based position in the full variant list,
// renumbers sequences 1..n, and syncs open statuses so the first open batch
// by sequence is active and the rest are queued.
func applyReorder(batches []PreorderBatch, batchID string, sequence int) ([]PreorderBatch, *PreorderBatch, error) {
	if len(batches) == 0 {
		return nil, nil, gorm.ErrRecordNotFound
	}

	fromIdx := -1
	for i := range batches {
		if batches[i].ID == batchID {
			fromIdx = i
			break
		}
	}
	if fromIdx < 0 {
		return nil, nil, gorm.ErrRecordNotFound
	}

	moving := batches[fromIdx]
	if moving.Status != StatusActive && moving.Status != StatusQueued {
		return nil, nil, gorm.ErrInvalidData
	}

	toIdx := sequence - 1
	if toIdx < 0 {
		toIdx = 0
	}
	if toIdx >= len(batches) {
		toIdx = len(batches) - 1
	}
	if fromIdx == toIdx {
		reordered := make([]PreorderBatch, len(batches))
		copy(reordered, batches)
		syncOpenStatuses(reordered)
		moved := reordered[toIdx]
		return reordered, &moved, nil
	}

	without := make([]PreorderBatch, 0, len(batches)-1)
	without = append(without, batches[:fromIdx]...)
	without = append(without, batches[fromIdx+1:]...)

	reordered := make([]PreorderBatch, 0, len(batches))
	reordered = append(reordered, without[:toIdx]...)
	reordered = append(reordered, moving)
	reordered = append(reordered, without[toIdx:]...)

	for i := range reordered {
		reordered[i].Sequence = i + 1
	}
	syncOpenStatuses(reordered)

	moved := reordered[toIdx]
	return reordered, &moved, nil
}

func syncOpenStatuses(batches []PreorderBatch) {
	firstOpen := true
	for i := range batches {
		switch batches[i].Status {
		case StatusActive, StatusQueued:
			if firstOpen {
				batches[i].Status = StatusActive
				firstOpen = false
			} else {
				batches[i].Status = StatusQueued
			}
		}
	}
}

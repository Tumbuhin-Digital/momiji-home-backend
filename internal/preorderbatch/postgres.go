package preorderbatch

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PostgresStore struct {
	db *gorm.DB
}

func NewPostgresStore(db *gorm.DB) Store {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) ListByVariantID(ctx context.Context, variantID string) ([]PreorderBatch, error) {
	var batches []PreorderBatch
	err := s.db.WithContext(ctx).
		Where("variant_id = ?", variantID).
		Order("sequence ASC, created_at ASC").
		Find(&batches).Error
	return batches, err
}

func (s *PostgresStore) GetBatchByID(ctx context.Context, batchID string) (*PreorderBatch, error) {
	var batch PreorderBatch
	err := s.db.WithContext(ctx).Where("id = ?", batchID).First(&batch).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &batch, nil
}

func (s *PostgresStore) GetCommittedAllocationsByOrderLineItemID(ctx context.Context, orderLineItemID string) ([]BatchAllocation, error) {
	var allocations []BatchAllocation
	err := s.db.WithContext(ctx).
		Where("order_line_item_id = ? AND status = ?", orderLineItemID, AllocationStatusCommitted).
		Find(&allocations).Error
	return allocations, err
}

func (s *PostgresStore) GetCommittedAllocationsByOrderLineItemIDs(ctx context.Context, orderLineItemIDs []string) ([]BatchAllocation, error) {
	if len(orderLineItemIDs) == 0 {
		return nil, nil
	}
	var allocations []BatchAllocation
	err := s.db.WithContext(ctx).
		Where("order_line_item_id IN ? AND status = ?", orderLineItemIDs, AllocationStatusCommitted).
		Find(&allocations).Error
	return allocations, err
}

func (s *PostgresStore) GetBatchesByIDs(ctx context.Context, batchIDs []string) ([]PreorderBatch, error) {
	if len(batchIDs) == 0 {
		return nil, nil
	}
	var batches []PreorderBatch
	err := s.db.WithContext(ctx).
		Where("id IN ?", batchIDs).
		Find(&batches).Error
	return batches, err
}

func (s *PostgresStore) CreateBatch(ctx context.Context, batch *PreorderBatch) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing []PreorderBatch
		if err := tx.
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("variant_id = ? AND status IN ?", batch.VariantID, []string{StatusActive, StatusQueued}).
			Order("sequence ASC").
			Find(&existing).Error; err != nil {
			return err
		}

		if len(existing) == 0 {
			batch.Status = StatusActive
			batch.Sequence = 1
		} else {
			batch.Status = StatusQueued
			maxSequence := existing[len(existing)-1].Sequence
			batch.Sequence = maxSequence + 1
		}

		return tx.Create(batch).Error
	})
}

func (s *PostgresStore) UpdateBatch(ctx context.Context, batchID string, updates BatchUpdates) (*PreorderBatch, error) {
	var updated PreorderBatch
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var batch PreorderBatch
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", batchID).First(&batch).Error; err != nil {
			return err
		}
		if batch.Status != StatusActive && batch.Status != StatusQueued {
			return gorm.ErrInvalidData
		}
		if updates.Name != nil {
			batch.Name = strings.TrimSpace(*updates.Name)
		}
		if updates.QtyAllocated != nil {
			batch.QtyAllocated = *updates.QtyAllocated
		}
		if err := tx.Model(&batch).Updates(map[string]any{
			"name":          batch.Name,
			"qty_allocated": batch.QtyAllocated,
			"updated_at":    gorm.Expr("now()"),
		}).Error; err != nil {
			return err
		}
		updated = batch
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func (s *PostgresStore) CloseBatch(ctx context.Context, batchID string) ([]PreorderBatch, error) {
	var result []PreorderBatch
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var batch PreorderBatch
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", batchID).First(&batch).Error; err != nil {
			return err
		}
		if batch.Status != StatusActive {
			return gorm.ErrInvalidData
		}

		var batches []PreorderBatch
		if err := tx.
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("variant_id = ? AND status IN ?", batch.VariantID, []string{StatusActive, StatusQueued}).
			Order("sequence ASC").
			Find(&batches).Error; err != nil {
			return err
		}

		now := time.Now().UTC()
		if err := tx.Model(&PreorderBatch{}).
			Where("id = ?", batch.ID).
			Updates(map[string]any{
				"status":     StatusClosed,
				"closed_at":  now,
				"updated_at": gorm.Expr("now()"),
			}).Error; err != nil {
			return err
		}

		for _, item := range batches {
			if item.ID == batch.ID {
				continue
			}
			if item.Status == StatusQueued {
				if err := tx.Model(&PreorderBatch{}).
					Where("id = ?", item.ID).
					Updates(map[string]any{
						"status":     StatusActive,
						"updated_at": gorm.Expr("now()"),
					}).Error; err != nil {
					return err
				}
				break
			}
		}

		return tx.Where("variant_id = ?", batch.VariantID).
			Order("sequence ASC, created_at ASC").
			Find(&result).Error
	})
	return result, err
}

func (s *PostgresStore) CancelBatch(ctx context.Context, batchID string) (*PreorderBatch, error) {
	var updated PreorderBatch
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var batch PreorderBatch
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", batchID).First(&batch).Error; err != nil {
			return err
		}
		if batch.Status != StatusQueued {
			return gorm.ErrInvalidData
		}
		if err := tx.Model(&PreorderBatch{}).
			Where("id = ?", batch.ID).
			Updates(map[string]any{
				"status":     StatusCancelled,
				"closed_at":  time.Now().UTC(),
				"updated_at": gorm.Expr("now()"),
			}).Error; err != nil {
			return err
		}
		batch.Status = StatusCancelled
		updated = batch
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func (s *PostgresStore) ReorderBatch(ctx context.Context, batchID string, sequence int) (*PreorderBatch, error) {
	var updated PreorderBatch
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var target PreorderBatch
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", batchID).First(&target).Error; err != nil {
			return err
		}
		if target.Status != StatusActive && target.Status != StatusQueued {
			return gorm.ErrInvalidData
		}

		var batches []PreorderBatch
		if err := tx.
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("variant_id = ?", target.VariantID).
			Order("sequence ASC, created_at ASC").
			Find(&batches).Error; err != nil {
			return err
		}

		reordered, moved, err := applyReorder(batches, batchID, sequence)
		if err != nil {
			return err
		}

		// Clear the unique one-active constraint before applying new statuses.
		if err := tx.Model(&PreorderBatch{}).
			Where("variant_id = ? AND status = ?", target.VariantID, StatusActive).
			Updates(map[string]any{
				"status":     StatusQueued,
				"updated_at": gorm.Expr("now()"),
			}).Error; err != nil {
			return err
		}

		for _, batch := range reordered {
			if err := tx.Model(&PreorderBatch{}).
				Where("id = ?", batch.ID).
				Updates(map[string]any{
					"sequence":   batch.Sequence,
					"status":     batch.Status,
					"updated_at": gorm.Expr("now()"),
				}).Error; err != nil {
				return err
			}
		}

		updated = *moved
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func (s *PostgresStore) CancelOpenBatchesByVariantID(ctx context.Context, variantID string) error {
	return s.db.WithContext(ctx).Model(&PreorderBatch{}).
		Where("variant_id = ? AND status IN ?", variantID, []string{StatusActive, StatusQueued}).
		Updates(map[string]any{
			"status":     StatusCancelled,
			"closed_at":  time.Now().UTC(),
			"updated_at": gorm.Expr("now()"),
		}).Error
}

func (s *PostgresStore) AllocateToBatch(ctx context.Context, input AllocateInput) (*AllocateExecution, error) {
	execution := &AllocateExecution{
		Allocations: make([]BatchAllocationLine, 0),
	}

	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var batches []PreorderBatch
		if err := tx.
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("variant_id = ? AND status IN ?", input.VariantID, []string{StatusActive, StatusQueued}).
			Order("sequence ASC, created_at ASC").
			Find(&batches).Error; err != nil {
			return err
		}

		execution.Batches = append(execution.Batches, batches...)
		if len(batches) == 0 {
			execution.UnlimitedQty = input.Quantity
			return nil
		}

		if input.Commit && input.OrderLineItemID != nil {
			var existingCount int64
			if err := tx.Model(&BatchAllocation{}).
				Where("order_line_item_id = ? AND status = ?", *input.OrderLineItemID, AllocationStatusCommitted).
				Count(&existingCount).Error; err != nil {
				return err
			}
			if existingCount > 0 {
				return nil
			}
		}

		remainingQty := input.Quantity
		for remainingQty > 0 {
			activeIdx := -1
			for idx, batch := range batches {
				if batch.Status == StatusActive {
					activeIdx = idx
					break
				}
			}
			if activeIdx == -1 {
				for idx, batch := range batches {
					if batch.Status == StatusQueued {
						if err := tx.Model(&PreorderBatch{}).
							Where("id = ?", batch.ID).
							Updates(map[string]any{
								"status":     StatusActive,
								"updated_at": gorm.Expr("now()"),
							}).Error; err != nil {
							return err
						}
						batches[idx].Status = StatusActive
						activeIdx = idx
						break
					}
				}
			}
			if activeIdx == -1 {
				execution.UnlimitedQty += remainingQty
				break
			}

			active := &batches[activeIdx]
			lockedQty := 0
			if input.LockedQtyByBatchID != nil {
				lockedQty = input.LockedQtyByBatchID[active.ID]
			}
			available := active.QtyRemaining() - lockedQty
			if available < 0 {
				available = 0
			}
			if available <= 0 {
				// Active batch already has no free capacity (sold out or locked by others).
				// Skip/promote without signaling depletion — this order never consumed from it.
				// Depletion is only confirmed when THIS order takes the last units and spills
				// (handled after take below).
				if input.Commit {
					if _, err := s.closeActiveAndPromote(tx, active, batches); err != nil {
						return err
					}
				} else {
					active.Status = StatusClosed
					for idx := range batches {
						if batches[idx].Status == StatusQueued {
							batches[idx].Status = StatusActive
							break
						}
					}
				}
				continue
			}

			take := remainingQty
			if take > available {
				take = available
			}

			if input.Commit {
				active.QtySold += take
				if err := tx.Model(&PreorderBatch{}).
					Where("id = ?", active.ID).
					Updates(map[string]any{
						"qty_sold":   active.QtySold,
						"updated_at": gorm.Expr("now()"),
					}).Error; err != nil {
					return err
				}

				allocation := &BatchAllocation{
					BatchID:          active.ID,
					OrderLineItemID:  input.OrderLineItemID,
					ShopifyVariantID: input.ShopifyVariantID,
					Quantity:         take,
					Status:           AllocationStatusCommitted,
				}
				if allocation.ID == "" {
					allocation.ID = uuid.NewString()
				}
				if err := tx.Create(allocation).Error; err != nil {
					return err
				}
			} else {
				// Simulate consumption for preview so remaining/depletion stay accurate.
				active.QtySold += take
			}

			execution.Allocations = append(execution.Allocations, BatchAllocationLine{
				BatchID:   active.ID,
				BatchName: active.Name,
				Quantity:  take,
			})
			remainingQty -= take

			if active.QtyRemaining()-lockedQty <= 0 {
				var nextName *string
				var err error
				if input.Commit {
					nextName, err = s.closeActiveAndPromote(tx, active, batches)
					if err != nil {
						return err
					}
				} else {
					nextName = s.peekNextQueuedName(batches)
					active.Status = StatusClosed
					for idx := range batches {
						if batches[idx].Status == StatusQueued {
							batches[idx].Status = StatusActive
							break
						}
					}
				}
				// Only confirm depletion when this order spills into next batch / unlimited.
				if remainingQty > 0 && execution.Depletion == nil {
					execution.Depletion = &BatchDepletionEvent{
						VariantID:       input.ShopifyVariantID,
						ClosedBatchName: active.Name,
						NextBatchName:   nextName,
						ProductTitle:    input.ProductTitle,
						ImageURL:        input.ImageURL,
					}
				}
			}
		}

		execution.Batches = batches
		return nil
	})
	if err != nil {
		return nil, err
	}

	return execution, nil
}

func (s *PostgresStore) peekNextQueuedName(batches []PreorderBatch) *string {
	for idx := range batches {
		if batches[idx].Status == StatusQueued {
			name := batches[idx].Name
			return &name
		}
	}
	return nil
}

func (s *PostgresStore) closeActiveAndPromote(tx *gorm.DB, active *PreorderBatch, batches []PreorderBatch) (*string, error) {
	now := time.Now().UTC()
	if err := tx.Model(&PreorderBatch{}).
		Where("id = ?", active.ID).
		Updates(map[string]any{
			"status":     StatusClosed,
			"closed_at":  now,
			"updated_at": gorm.Expr("now()"),
		}).Error; err != nil {
		return nil, err
	}
	active.Status = StatusClosed
	active.ClosedAt = &now

	for idx := range batches {
		if batches[idx].Status != StatusQueued {
			continue
		}
		if err := tx.Model(&PreorderBatch{}).
			Where("id = ?", batches[idx].ID).
			Updates(map[string]any{
				"status":     StatusActive,
				"updated_at": gorm.Expr("now()"),
			}).Error; err != nil {
			return nil, err
		}
		batches[idx].Status = StatusActive
		nextName := batches[idx].Name
		return &nextName, nil
	}

	return nil, nil
}

func (s *PostgresStore) ReleaseAllocationsByOrderLineItemID(ctx context.Context, orderLineItemID string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var allocations []BatchAllocation
		if err := tx.
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("order_line_item_id = ? AND status = ?", orderLineItemID, AllocationStatusCommitted).
			Find(&allocations).Error; err != nil {
			return err
		}
		if len(allocations) == 0 {
			return nil
		}

		for _, allocation := range allocations {
			var batch PreorderBatch
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", allocation.BatchID).First(&batch).Error; err != nil {
				return err
			}
			newSold := batch.QtySold - allocation.Quantity
			if newSold < 0 {
				newSold = 0
			}
			if err := tx.Model(&PreorderBatch{}).
				Where("id = ?", batch.ID).
				Updates(map[string]any{
					"qty_sold":   newSold,
					"updated_at": gorm.Expr("now()"),
				}).Error; err != nil {
				return err
			}
			if err := tx.Model(&BatchAllocation{}).
				Where("id = ?", allocation.ID).
				Updates(map[string]any{
					"status":      AllocationStatusReleased,
					"released_at": time.Now().UTC(),
				}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *PostgresStore) GetActiveLockedQtyForBatch(ctx context.Context, batchID string) (int, error) {
	var totalQty int64
	err := s.db.WithContext(ctx).
		Model(&BatchLock{}).
		Where("batch_id = ? AND expires_at > ?", batchID, time.Now()).
		Select("COALESCE(SUM(quantity), 0)").
		Scan(&totalQty).Error
	return int(totalQty), err
}

func (s *PostgresStore) GetActiveLockedQtyByBatchIDs(ctx context.Context, batchIDs []string, excludeUserID, excludeSessionID *string) (map[string]int, error) {
	result := make(map[string]int, len(batchIDs))
	if len(batchIDs) == 0 {
		return result, nil
	}

	type row struct {
		BatchID string `gorm:"column:batch_id"`
		Total   int    `gorm:"column:total"`
	}
	var rows []row
	query := s.db.WithContext(ctx).
		Model(&BatchLock{}).
		Select("batch_id, COALESCE(SUM(quantity), 0) AS total").
		Where("batch_id IN ? AND expires_at > ?", batchIDs, time.Now())

	// Do not count this shopper's own soft-locks against themselves (retry / reopen checkout).
	if excludeUserID != nil && *excludeUserID != "" && excludeSessionID != nil && *excludeSessionID != "" {
		query = query.Where(
			"NOT ((user_id IS NOT NULL AND user_id = ?) OR (session_id IS NOT NULL AND session_id = ?))",
			*excludeUserID, *excludeSessionID,
		)
	} else if excludeUserID != nil && *excludeUserID != "" {
		query = query.Where("NOT (user_id IS NOT NULL AND user_id = ?)", *excludeUserID)
	} else if excludeSessionID != nil && *excludeSessionID != "" {
		query = query.Where("NOT (session_id IS NOT NULL AND session_id = ?)", *excludeSessionID)
	}

	err := query.Group("batch_id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, item := range rows {
		result[item.BatchID] = item.Total
	}
	return result, nil
}

func (s *PostgresStore) CreateBatchLocks(ctx context.Context, locks []BatchLock) error {
	if len(locks) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Create(&locks).Error
}

func (s *PostgresStore) DeleteBatchLocksBySession(ctx context.Context, userID, sessionID *string) error {
	query := s.db.WithContext(ctx)
	hasCond := false

	if userID != nil && *userID != "" {
		query = query.Where("user_id = ?", *userID)
		hasCond = true
	}
	if sessionID != nil && *sessionID != "" {
		if hasCond {
			query = query.Or("session_id = ?", *sessionID)
		} else {
			query = query.Where("session_id = ?", *sessionID)
		}
		hasCond = true
	}
	if !hasCond {
		return nil
	}
	return query.Delete(&BatchLock{}).Error
}

func (s *PostgresStore) DeleteBatchLocksByCheckoutReference(
	ctx context.Context,
	checkoutReference string,
	userID, sessionID *string,
) error {
	if checkoutReference == "" {
		return nil
	}

	query := s.db.WithContext(ctx).Where("checkout_reference = ?", checkoutReference)
	if userID != nil && *userID != "" && sessionID != nil && *sessionID != "" {
		query = query.Where("user_id = ? OR session_id = ?", *userID, *sessionID)
	} else if userID != nil && *userID != "" {
		query = query.Where("user_id = ?", *userID)
	} else if sessionID != nil && *sessionID != "" {
		query = query.Where("session_id = ?", *sessionID)
	}
	return query.Delete(&BatchLock{}).Error
}

func (s *PostgresStore) DeleteExpiredBatchLocks(ctx context.Context) error {
	return s.db.WithContext(ctx).Where("expires_at <= ?", time.Now()).Delete(&BatchLock{}).Error
}

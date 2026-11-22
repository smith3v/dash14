package storage

import (
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	adminRequestPending  = "pending"
	adminRequestApproved = "approved"
	adminRequestDenied   = "denied"
)

var (
	ErrAdminUnauthorized   = errors.New("storage: admin access required")
	ErrAdminActionStale    = errors.New("storage: admin action is stale")
	ErrAdminRequestPending = errors.New("storage: admin request already pending")
	ErrNoAdmins            = errors.New("storage: no admins available")
	ErrLastAdmin           = errors.New("storage: cannot revoke the last admin")
)

// AdminRequest records one request and its final decision, if any.
type AdminRequest struct {
	ID          int64  `gorm:"primaryKey"`
	RequesterID int64  `gorm:"not null;index"`
	Status      string `gorm:"not null"`
	DecidedBy   int64  `gorm:"not null;default:0"`
	CreatedAt   time.Time
}

func (r AdminRequest) Pending() bool  { return r.Status == adminRequestPending }
func (r AdminRequest) Approved() bool { return r.Status == adminRequestApproved }

// AdminRequestNotice identifies an admin's copy of a request in Telegram.
type AdminRequestNotice struct {
	RequestID int64 `gorm:"primaryKey;autoIncrement:false"`
	ChatID    int64 `gorm:"primaryKey;autoIncrement:false"`
	MessageID int   `gorm:"primaryKey;autoIncrement:false"`
}

// ListAdmins returns all Dash14 admins, including unsubscribed admins.
func (r *UserRepository) ListAdmins() ([]User, error) {
	var users []User
	err := r.db.Where("is_admin = ?", true).Order("display_name COLLATE NOCASE, telegram_user_id").Find(&users).Error
	return users, err
}

func (r *UserRepository) ListPendingAdminRequests() ([]AdminRequest, error) {
	var requests []AdminRequest
	err := r.db.Where("status = ?", adminRequestPending).Order("id").Find(&requests).Error
	return requests, err
}

func (r *UserRepository) GetAdminRequest(id int64) (AdminRequest, error) {
	var request AdminRequest
	err := r.db.Take(&request, id).Error
	return request, err
}

func (r *UserRepository) GetPendingAdminRequest(userID int64) (AdminRequest, error) {
	var request AdminRequest
	err := r.db.Where("requester_id = ? AND status = ?", userID, adminRequestPending).Take(&request).Error
	return request, err
}

// GetLatestAdminRequest returns the newest request and its persisted outcome.
func (r *UserRepository) GetLatestAdminRequest(userID int64) (AdminRequest, error) {
	var request AdminRequest
	err := r.db.Where("requester_id = ?", userID).Order("id DESC").Take(&request).Error
	return request, err
}

func (r *UserRepository) RecordAdminRequestNotice(notice AdminRequestNotice) error {
	return r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&notice).Error
}

func (r *UserRepository) ListAdminRequestNotices(requestID int64) ([]AdminRequestNotice, error) {
	var notices []AdminRequestNotice
	err := r.db.Where("request_id = ?", requestID).Find(&notices).Error
	return notices, err
}

const currentAdminSQL = "EXISTS (SELECT 1 FROM users WHERE telegram_user_id = ? AND is_admin = ? AND admin_version = ?)"

// RequestAdminAccess consumes the request button and creates a pending request.
func (r *UserRepository) RequestAdminAccess(userID, version int64) (AdminRequest, error) {
	request := AdminRequest{RequesterID: userID, Status: adminRequestPending}
	err := r.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&User{}).
			Where("telegram_user_id = ? AND is_admin = ? AND admin_version = ?", userID, false, version).
			Where("NOT EXISTS (SELECT 1 FROM admin_requests WHERE requester_id = ? AND status = ?)", userID, adminRequestPending).
			Where("EXISTS (SELECT 1 FROM users WHERE is_admin = ?)", true).
			UpdateColumn("admin_version", gorm.Expr("admin_version + 1"))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 1 {
			return tx.Create(&request).Error
		}

		users := NewUserRepository(tx)
		_, err := users.GetPendingAdminRequest(userID)
		if err == nil {
			return ErrAdminRequestPending
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var admins int64
		if err := tx.Model(&User{}).Where("is_admin = ?", true).Count(&admins).Error; err != nil {
			return err
		}
		if admins == 0 {
			return ErrNoAdmins
		}
		return ErrAdminActionStale
	})
	return request, err
}

// DecideAdminRequest claims a pending request and, on approval, grants access
// in the same transaction. The first committed decision wins.
func (r *UserRepository) DecideAdminRequest(id, actorID, actorVersion int64, approve bool) (AdminRequest, error) {
	status := adminRequestDenied
	if approve {
		status = adminRequestApproved
	}
	var request AdminRequest
	err := r.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&AdminRequest{}).
			Where("id = ? AND status = ?", id, adminRequestPending).
			Where(currentAdminSQL, actorID, true, actorVersion).
			Updates(map[string]any{"status": status, "decided_by": actorID})
		if err := checkAdminMutation(tx, result, actorID, actorVersion); err != nil {
			return err
		}

		var err error
		request, err = NewUserRepository(tx).GetAdminRequest(id)
		if err != nil || !approve {
			return err
		}
		// Bootstrap SQL may already have granted access to this requester.
		return tx.Model(&User{}).Where("telegram_user_id = ? AND is_admin = ?", request.RequesterID, false).
			Updates(map[string]any{"is_admin": true, "admin_version": gorm.Expr("admin_version + 1")}).Error
	})
	return request, err
}

// RevokeAdminAccess removes the role and releases ownership of the current
// match atomically. It reports whether another admin needs to take over.
func (r *UserRepository) RevokeAdminAccess(actorID, actorVersion, targetID, targetVersion int64) (bool, error) {
	var released bool
	err := r.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&User{}).
			Where("telegram_user_id = ? AND is_admin = ? AND admin_version = ?", targetID, true, targetVersion).
			Where(currentAdminSQL, actorID, true, actorVersion).
			Where("(SELECT COUNT(*) FROM users WHERE is_admin = ?) > 1", true).
			Updates(map[string]any{"is_admin": false, "admin_version": gorm.Expr("admin_version + 1")})
		err := checkAdminMutation(tx, result, actorID, actorVersion)
		if errors.Is(err, ErrAdminActionStale) {
			if targetID == actorID && targetVersion == actorVersion {
				return ErrLastAdmin
			}
		}
		if err != nil {
			return err
		}

		control := tx.Model(&Game{}).
			Where("current_admin_user_id = ? AND status <> ?", targetID, GameStatusFinished).
			Updates(map[string]any{"current_admin_user_id": 0, "control_chat_id": 0, "control_message_id": 0})
		released = control.RowsAffected != 0
		return control.Error
	})
	return released, err
}

func checkAdminMutation(tx *gorm.DB, result *gorm.DB, actorID, version int64) error {
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 0 {
		return nil
	}

	actor, err := NewUserRepository(tx).GetUserByTelegramID(actorID)
	if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && (!actor.IsAdmin || actor.AdminVersion != version) {
		return ErrAdminUnauthorized
	}
	if err != nil {
		return fmt.Errorf("storage: check admin mutation: %w", err)
	}
	return ErrAdminActionStale
}

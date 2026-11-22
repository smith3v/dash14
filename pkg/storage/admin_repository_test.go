package storage_test

import (
	"errors"
	"sync"
	"testing"

	"github.com/smith3v/dash14/pkg/storage"
)

func TestAdminRequestConcurrentSubmissionAndDecision(t *testing.T) {
	db := openTestDB(t)
	users := storage.NewUserRepository(db)
	if err := db.Create(&[]storage.User{
		{TelegramUserID: 1, IsAdmin: true},
		{TelegramUserID: 2, IsAdmin: true},
		{TelegramUserID: 3},
	}).Error; err != nil {
		t.Fatal(err)
	}

	outcomes := raceAdminChanges(
		func() error { _, err := users.RequestAdminAccess(3, 0); return err },
		func() error { _, err := users.RequestAdminAccess(3, 0); return err },
	)
	requireOneAdminChange(t, outcomes, storage.ErrAdminRequestPending)
	pending, err := users.GetPendingAdminRequest(3)
	if err != nil || pending.ID == 0 {
		t.Fatalf("pending request = %+v, %v", pending, err)
	}

	outcomes = raceAdminChanges(
		func() error { _, err := users.DecideAdminRequest(pending.ID, 1, 0, true); return err },
		func() error { _, err := users.DecideAdminRequest(pending.ID, 2, 0, false); return err },
	)
	requireOneAdminChange(t, outcomes, storage.ErrAdminActionStale)
	finished, err := users.GetAdminRequest(pending.ID)
	if err != nil || finished.Pending() || finished.DecidedBy == 0 {
		t.Fatalf("finished request = %+v, %v", finished, err)
	}
	requester, err := users.GetUserByTelegramID(3)
	if err != nil || requester.IsAdmin != finished.Approved() || requester.Subscribed {
		t.Fatalf("requester = %+v, finished request = %+v, %v", requester, finished, err)
	}
	if _, err := users.RequestAdminAccess(3, 0); !errors.Is(err, storage.ErrAdminActionStale) {
		t.Fatalf("old request button returned %v", err)
	}
}

func TestConcurrentAdminRevocationsPreserveOneAdmin(t *testing.T) {
	db := openTestDB(t)
	users := storage.NewUserRepository(db)
	if err := db.Create(&[]storage.User{
		{TelegramUserID: 1, IsAdmin: true},
		{TelegramUserID: 2, IsAdmin: true},
	}).Error; err != nil {
		t.Fatal(err)
	}
	outcomes := raceAdminChanges(
		func() error { _, err := users.RevokeAdminAccess(1, 0, 2, 0); return err },
		func() error { _, err := users.RevokeAdminAccess(2, 0, 1, 0); return err },
	)
	requireOneAdminChange(t, outcomes, storage.ErrAdminUnauthorized)
	admins, err := users.ListAdmins()
	if err != nil || len(admins) != 1 {
		t.Fatalf("remaining admins = %+v, %v", admins, err)
	}
	last := admins[0]
	if _, err := users.RevokeAdminAccess(last.TelegramUserID, last.AdminVersion, last.TelegramUserID, last.AdminVersion); !errors.Is(err, storage.ErrLastAdmin) {
		t.Fatalf("last admin revocation returned %v", err)
	}
}

func raceAdminChanges(left, right func() error) [2]error {
	var results [2]error
	var workers sync.WaitGroup
	start := make(chan struct{})
	for index, change := range []func() error{left, right} {
		workers.Go(func() {
			<-start
			results[index] = change()
		})
	}
	close(start)
	workers.Wait()
	return results
}

func requireOneAdminChange(t *testing.T, outcomes [2]error, rejected error) {
	t.Helper()
	if outcomes[0] == nil && errors.Is(outcomes[1], rejected) ||
		outcomes[1] == nil && errors.Is(outcomes[0], rejected) {
		return
	}
	t.Fatalf("got %v and %v, want one success and one %v", outcomes[0], outcomes[1], rejected)
}

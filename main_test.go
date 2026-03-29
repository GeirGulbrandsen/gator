package main

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/geirgulbrandsen/gator/internal/config"
	"github.com/geirgulbrandsen/gator/internal/database"
	"github.com/google/uuid"
)

type fakeDB struct {
	getUserFn               func(context.Context, string) (database.User, error)
	createUserFn            func(context.Context, database.CreateUserParams) (database.User, error)
	resetUsersFn            func(context.Context) error
	getUsersFn              func(context.Context) ([]database.User, error)
	createFeedFn            func(context.Context, database.CreateFeedParams) (database.Feed, error)
	getFeedsFn              func(context.Context) ([]database.GetFeedsRow, error)
	getFeedByURLFn          func(context.Context, string) (database.Feed, error)
	createFeedFollowFn      func(context.Context, database.CreateFeedFollowParams) (database.CreateFeedFollowRow, error)
	getFeedFollowsForUserFn func(context.Context, uuid.UUID) ([]database.GetFeedFollowsForUserRow, error)

	createFeedCalls            []database.CreateFeedParams
	createFeedFollowCalls      []database.CreateFeedFollowParams
	getFeedByURLCalls          []string
	getFeedFollowsForUserCalls []uuid.UUID
	resetUsersCalled           bool
}

func (f *fakeDB) GetUser(ctx context.Context, name string) (database.User, error) {
	if f.getUserFn != nil {
		return f.getUserFn(ctx, name)
	}
	return database.User{}, errors.New("not implemented")
}

func (f *fakeDB) CreateUser(ctx context.Context, params database.CreateUserParams) (database.User, error) {
	if f.createUserFn != nil {
		return f.createUserFn(ctx, params)
	}
	return database.User{}, errors.New("not implemented")
}

func (f *fakeDB) ResetUsers(ctx context.Context) error {
	f.resetUsersCalled = true
	if f.resetUsersFn != nil {
		return f.resetUsersFn(ctx)
	}
	return nil
}

func (f *fakeDB) GetUsers(ctx context.Context) ([]database.User, error) {
	if f.getUsersFn != nil {
		return f.getUsersFn(ctx)
	}
	return nil, errors.New("not implemented")
}

func (f *fakeDB) CreateFeed(ctx context.Context, params database.CreateFeedParams) (database.Feed, error) {
	f.createFeedCalls = append(f.createFeedCalls, params)
	if f.createFeedFn != nil {
		return f.createFeedFn(ctx, params)
	}
	return database.Feed{}, errors.New("not implemented")
}

func (f *fakeDB) GetFeeds(ctx context.Context) ([]database.GetFeedsRow, error) {
	if f.getFeedsFn != nil {
		return f.getFeedsFn(ctx)
	}
	return nil, errors.New("not implemented")
}

func (f *fakeDB) GetFeedByURL(ctx context.Context, url string) (database.Feed, error) {
	f.getFeedByURLCalls = append(f.getFeedByURLCalls, url)
	if f.getFeedByURLFn != nil {
		return f.getFeedByURLFn(ctx, url)
	}
	return database.Feed{}, errors.New("not implemented")
}

func (f *fakeDB) CreateFeedFollow(ctx context.Context, params database.CreateFeedFollowParams) (database.CreateFeedFollowRow, error) {
	f.createFeedFollowCalls = append(f.createFeedFollowCalls, params)
	if f.createFeedFollowFn != nil {
		return f.createFeedFollowFn(ctx, params)
	}
	return database.CreateFeedFollowRow{}, errors.New("not implemented")
}

func (f *fakeDB) GetFeedFollowsForUser(ctx context.Context, userID uuid.UUID) ([]database.GetFeedFollowsForUserRow, error) {
	f.getFeedFollowsForUserCalls = append(f.getFeedFollowsForUserCalls, userID)
	if f.getFeedFollowsForUserFn != nil {
		return f.getFeedFollowsForUserFn(ctx, userID)
	}
	return nil, errors.New("not implemented")
}

func captureOutput(t *testing.T, fn func()) string {
	t.Helper()

	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}

	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close write pipe: %v", err)
	}

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}

	if err := r.Close(); err != nil {
		t.Fatalf("close read pipe: %v", err)
	}

	return string(out)
}

func TestCommandsRunUnknownCommand(t *testing.T) {
	cmds := &commands{handlers: map[string]func(*state, command) error{}}
	err := cmds.run(&state{}, command{name: "does-not-exist"})
	if err == nil {
		t.Fatal("expected unknown command error")
	}
}

func TestHandlerArgValidation(t *testing.T) {
	tests := []struct {
		name    string
		handler func(*state, command) error
		cmd     command
	}{
		{
			name:    "login requires username",
			handler: handlerLogin,
			cmd:     command{name: "login", args: []string{}},
		},
		{
			name:    "register requires username",
			handler: handlerRegister,
			cmd:     command{name: "register", args: []string{}},
		},
		{
			name:    "addfeed requires name and url",
			handler: handlerAddFeed,
			cmd:     command{name: "addfeed", args: []string{"only-name"}},
		},
		{
			name:    "follow requires url",
			handler: handlerFollow,
			cmd:     command{name: "follow", args: []string{}},
		},
		{
			name:    "feeds takes no args",
			handler: handlerFeeds,
			cmd:     command{name: "feeds", args: []string{"extra"}},
		},
		{
			name:    "following takes no args",
			handler: handlerFollowing,
			cmd:     command{name: "following", args: []string{"extra"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.handler(&state{db: &fakeDB{}, cfg: &config.Config{}}, tc.cmd)
			if err == nil {
				t.Fatalf("expected validation error for %s", tc.cmd.name)
			}
		})
	}
}

func TestHandlerResetCallsDB(t *testing.T) {
	fdb := &fakeDB{}
	s := &state{db: fdb, cfg: &config.Config{}}

	if err := handlerReset(s, command{name: "reset"}); err != nil {
		t.Fatalf("handlerReset returned error: %v", err)
	}

	if !fdb.resetUsersCalled {
		t.Fatal("expected ResetUsers to be called")
	}
}

func TestHandlerUsersPrintsCurrentUserMarker(t *testing.T) {
	users := []database.User{
		{Name: "alice"},
		{Name: "bob"},
	}
	fdb := &fakeDB{
		getUsersFn: func(context.Context) ([]database.User, error) {
			return users, nil
		},
	}
	s := &state{db: fdb, cfg: &config.Config{CurrentUserName: "bob"}}

	out := captureOutput(t, func() {
		err := handlerUsers(s, command{name: "users"})
		if err != nil {
			t.Fatalf("handlerUsers returned error: %v", err)
		}
	})

	if !strings.Contains(out, "* bob (current)") {
		t.Fatalf("expected current user marker in output, got: %q", out)
	}
}

func TestHandlerAddFeedAutoCreatesFollow(t *testing.T) {
	currentUserID := uuid.New()
	createdFeedID := uuid.New()
	fdb := &fakeDB{
		getUserFn: func(context.Context, string) (database.User, error) {
			return database.User{ID: currentUserID, Name: "alice"}, nil
		},
		createFeedFn: func(_ context.Context, params database.CreateFeedParams) (database.Feed, error) {
			return database.Feed{
				ID:        createdFeedID,
				CreatedAt: params.CreatedAt,
				UpdatedAt: params.UpdatedAt,
				Name:      params.Name,
				Url:       params.Url,
				UserID:    params.UserID,
			}, nil
		},
		createFeedFollowFn: func(context.Context, database.CreateFeedFollowParams) (database.CreateFeedFollowRow, error) {
			return database.CreateFeedFollowRow{}, nil
		},
	}
	s := &state{db: fdb, cfg: &config.Config{CurrentUserName: "alice"}}

	err := handlerAddFeed(s, command{name: "addfeed", args: []string{"Changelog", "https://example.com/rss"}})
	if err != nil {
		t.Fatalf("handlerAddFeed returned error: %v", err)
	}

	if len(fdb.createFeedCalls) != 1 {
		t.Fatalf("expected one CreateFeed call, got %d", len(fdb.createFeedCalls))
	}
	if len(fdb.createFeedFollowCalls) != 1 {
		t.Fatalf("expected one CreateFeedFollow call, got %d", len(fdb.createFeedFollowCalls))
	}

	followCall := fdb.createFeedFollowCalls[0]
	if followCall.UserID != currentUserID {
		t.Fatalf("expected follow user id %s, got %s", currentUserID, followCall.UserID)
	}
	if followCall.FeedID != createdFeedID {
		t.Fatalf("expected follow feed id %s, got %s", createdFeedID, followCall.FeedID)
	}
}

func TestHandlerFollowLooksUpFeedByURLAndCreatesFollow(t *testing.T) {
	currentUserID := uuid.New()
	feedID := uuid.New()
	url := "https://hnrss.org/newest"
	fdb := &fakeDB{
		getUserFn: func(context.Context, string) (database.User, error) {
			return database.User{ID: currentUserID, Name: "alice"}, nil
		},
		getFeedByURLFn: func(context.Context, string) (database.Feed, error) {
			return database.Feed{ID: feedID, Name: "HN"}, nil
		},
		createFeedFollowFn: func(context.Context, database.CreateFeedFollowParams) (database.CreateFeedFollowRow, error) {
			return database.CreateFeedFollowRow{FeedName: "HN", UserName: "alice"}, nil
		},
	}
	s := &state{db: fdb, cfg: &config.Config{CurrentUserName: "alice"}}

	err := handlerFollow(s, command{name: "follow", args: []string{url}})
	if err != nil {
		t.Fatalf("handlerFollow returned error: %v", err)
	}

	if len(fdb.getFeedByURLCalls) != 1 || fdb.getFeedByURLCalls[0] != url {
		t.Fatalf("expected GetFeedByURL to be called with %q, got %+v", url, fdb.getFeedByURLCalls)
	}
	if len(fdb.createFeedFollowCalls) != 1 {
		t.Fatalf("expected one CreateFeedFollow call, got %d", len(fdb.createFeedFollowCalls))
	}
	if fdb.createFeedFollowCalls[0].FeedID != feedID {
		t.Fatalf("expected follow feed id %s, got %s", feedID, fdb.createFeedFollowCalls[0].FeedID)
	}
}

func TestHandlerFollowingPrintsFeedNamesForCurrentUser(t *testing.T) {
	currentUserID := uuid.New()
	fdb := &fakeDB{
		getUserFn: func(context.Context, string) (database.User, error) {
			return database.User{ID: currentUserID, Name: "alice"}, nil
		},
		getFeedFollowsForUserFn: func(context.Context, uuid.UUID) ([]database.GetFeedFollowsForUserRow, error) {
			return []database.GetFeedFollowsForUserRow{
				{FeedName: "Feed One"},
				{FeedName: "Feed Two"},
			}, nil
		},
	}
	s := &state{db: fdb, cfg: &config.Config{CurrentUserName: "alice"}}

	out := captureOutput(t, func() {
		err := handlerFollowing(s, command{name: "following"})
		if err != nil {
			t.Fatalf("handlerFollowing returned error: %v", err)
		}
	})

	if len(fdb.getFeedFollowsForUserCalls) != 1 || fdb.getFeedFollowsForUserCalls[0] != currentUserID {
		t.Fatalf("expected GetFeedFollowsForUser to be called with current user id, got %+v", fdb.getFeedFollowsForUserCalls)
	}
	if !strings.Contains(out, "* Feed One") || !strings.Contains(out, "* Feed Two") {
		t.Fatalf("expected followed feed names in output, got: %q", out)
	}
}

func TestHandlerFeedsPrintsFeedList(t *testing.T) {
	fdb := &fakeDB{
		getFeedsFn: func(context.Context) ([]database.GetFeedsRow, error) {
			return []database.GetFeedsRow{
				{FeedName: "The Changelog", Url: "https://changelog.com/feed", UserName: "alice"},
			}, nil
		},
	}
	s := &state{db: fdb, cfg: &config.Config{}}

	out := captureOutput(t, func() {
		err := handlerFeeds(s, command{name: "feeds"})
		if err != nil {
			t.Fatalf("handlerFeeds returned error: %v", err)
		}
	})

	if !strings.Contains(out, "name: The Changelog") || !strings.Contains(out, "user: alice") {
		t.Fatalf("expected feed details in output, got: %q", out)
	}
}

func TestHandlerAddFeedReturnsErrorWhenAutoFollowFails(t *testing.T) {
	errBoom := errors.New("insert failed")
	fdb := &fakeDB{
		getUserFn: func(context.Context, string) (database.User, error) {
			return database.User{ID: uuid.New(), Name: "alice"}, nil
		},
		createFeedFn: func(_ context.Context, params database.CreateFeedParams) (database.Feed, error) {
			return database.Feed{ID: uuid.New(), Name: params.Name, Url: params.Url, UserID: params.UserID, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
		},
		createFeedFollowFn: func(context.Context, database.CreateFeedFollowParams) (database.CreateFeedFollowRow, error) {
			return database.CreateFeedFollowRow{}, errBoom
		},
	}
	s := &state{db: fdb, cfg: &config.Config{CurrentUserName: "alice"}}

	err := handlerAddFeed(s, command{name: "addfeed", args: []string{"Blog", "https://blog.example.com/rss"}})
	if err == nil {
		t.Fatal("expected error when auto follow fails")
	}
	if !strings.Contains(err.Error(), "auto follow feed") {
		t.Fatalf("expected auto follow context in error, got: %v", err)
	}
}

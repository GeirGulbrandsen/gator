package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/geirgulbrandsen/gator/internal/config"
	"github.com/geirgulbrandsen/gator/internal/database"
	"github.com/geirgulbrandsen/gator/internal/rss"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

type state struct {
	db  db
	cfg *config.Config
}

type db interface {
	GetUser(context.Context, string) (database.User, error)
	CreateUser(context.Context, database.CreateUserParams) (database.User, error)
	ResetUsers(context.Context) error
	GetUsers(context.Context) ([]database.User, error)
	CreateFeed(context.Context, database.CreateFeedParams) (database.Feed, error)
	GetFeeds(context.Context) ([]database.GetFeedsRow, error)
	GetFeedByURL(context.Context, string) (database.Feed, error)
	CreateFeedFollow(context.Context, database.CreateFeedFollowParams) (database.CreateFeedFollowRow, error)
	GetFeedFollowsForUser(context.Context, uuid.UUID) ([]database.GetFeedFollowsForUserRow, error)
}

type command struct {
	name string
	args []string
}

type commands struct {
	handlers map[string]func(*state, command) error
}

func (c *commands) run(s *state, cmd command) error {
	handler, ok := c.handlers[cmd.name]
	if !ok {
		return fmt.Errorf("unknown command: %s", cmd.name)
	}

	return handler(s, cmd)
}

func (c *commands) register(name string, f func(*state, command) error) {
	c.handlers[name] = f
}

func middlewareLoggedIn(handler func(s *state, cmd command, user database.User) error) func(*state, command) error {
	return func(s *state, cmd command) error {
		user, err := s.db.GetUser(context.Background(), s.cfg.CurrentUserName)
		if err != nil {
			return fmt.Errorf("get current user: %w", err)
		}

		return handler(s, cmd, user)
	}
}

func handlerLogin(s *state, cmd command) error {
	if len(cmd.args) == 0 {
		return errors.New("username is required")
	}

	username := cmd.args[0]
	_, err := s.db.GetUser(context.Background(), username)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("user %s does not exist", username)
	}
	if err != nil {
		return fmt.Errorf("checking user: %w", err)
	}

	if err := s.cfg.SetUser(username); err != nil {
		return err
	}

	fmt.Printf("User set to %s\n", username)
	return nil
}

func handlerRegister(s *state, cmd command) error {
	if len(cmd.args) == 0 {
		return errors.New("username is required")
	}

	name := cmd.args[0]

	_, err := s.db.GetUser(context.Background(), name)
	if err == nil {
		return fmt.Errorf("user %s already exists", name)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("checking for existing user: %w", err)
	}

	now := time.Now().UTC()
	user, err := s.db.CreateUser(context.Background(), database.CreateUserParams{
		ID:        uuid.New(),
		CreatedAt: now,
		UpdatedAt: now,
		Name:      name,
	})
	if err != nil {
		return err
	}

	if err := s.cfg.SetUser(name); err != nil {
		return err
	}

	fmt.Printf("User created: %s\n", name)
	log.Printf("created user: %+v", user)

	return nil
}

func handlerReset(s *state, cmd command) error {
	err := s.db.ResetUsers(context.Background())
	if err != nil {
		return fmt.Errorf("reset users: %w", err)
	}

	fmt.Println("All users deleted")
	return nil
}

func handlerAgg(s *state, cmd command) error {
	feed, err := rss.FetchFeed(context.Background(), "https://www.wagslane.dev/index.xml")
	if err != nil {
		return fmt.Errorf("fetching feed: %w", err)
	}

	fmt.Printf("%+v\n", feed)
	return nil
}

func handlerUsers(s *state, cmd command) error {
	users, err := s.db.GetUsers(context.Background())
	if err != nil {
		return fmt.Errorf("get users: %w", err)
	}

	for _, user := range users {
		if user.Name == s.cfg.CurrentUserName {
			fmt.Printf("* %s (current)\n", user.Name)
			continue
		}

		fmt.Printf("* %s\n", user.Name)
	}

	return nil
}

func handlerAddFeed(s *state, cmd command, user database.User) error {
	if len(cmd.args) != 2 {
		return errors.New("usage: addfeed <name> <url>")
	}

	ctx := context.Background()
	now := time.Now().UTC()
	feed, err := s.db.CreateFeed(ctx, database.CreateFeedParams{
		ID:        uuid.New(),
		CreatedAt: now,
		UpdatedAt: now,
		Name:      cmd.args[0],
		Url:       cmd.args[1],
		UserID:    user.ID,
	})
	if err != nil {
		return fmt.Errorf("create feed: %w", err)
	}

	_, err = s.db.CreateFeedFollow(ctx, database.CreateFeedFollowParams{
		ID:        uuid.New(),
		CreatedAt: now,
		UpdatedAt: now,
		UserID:    user.ID,
		FeedID:    feed.ID,
	})
	if err != nil {
		return fmt.Errorf("auto follow feed: %w", err)
	}

	fmt.Printf("id: %s\n", feed.ID)
	fmt.Printf("created_at: %s\n", feed.CreatedAt)
	fmt.Printf("updated_at: %s\n", feed.UpdatedAt)
	fmt.Printf("name: %s\n", feed.Name)
	fmt.Printf("url: %s\n", feed.Url)
	fmt.Printf("user_id: %s\n", feed.UserID)

	return nil
}

func handlerFollow(s *state, cmd command, user database.User) error {
	if len(cmd.args) != 1 {
		return errors.New("usage: follow <url>")
	}

	ctx := context.Background()
	feed, err := s.db.GetFeedByURL(ctx, cmd.args[0])
	if err != nil {
		return fmt.Errorf("get feed by url: %w", err)
	}

	now := time.Now().UTC()
	feedFollow, err := s.db.CreateFeedFollow(ctx, database.CreateFeedFollowParams{
		ID:        uuid.New(),
		CreatedAt: now,
		UpdatedAt: now,
		UserID:    user.ID,
		FeedID:    feed.ID,
	})
	if err != nil {
		return fmt.Errorf("create feed follow: %w", err)
	}

	fmt.Printf("feed: %s\n", feedFollow.FeedName)
	fmt.Printf("user: %s\n", feedFollow.UserName)

	return nil
}

func handlerFollowing(s *state, cmd command, user database.User) error {
	if len(cmd.args) != 0 {
		return errors.New("usage: following")
	}

	ctx := context.Background()
	feedFollows, err := s.db.GetFeedFollowsForUser(ctx, user.ID)
	if err != nil {
		return fmt.Errorf("get follows for user: %w", err)
	}

	for _, feedFollow := range feedFollows {
		fmt.Printf("* %s\n", feedFollow.FeedName)
	}

	return nil
}

func handlerFeeds(s *state, cmd command) error {
	if len(cmd.args) != 0 {
		return errors.New("usage: feeds")
	}

	feeds, err := s.db.GetFeeds(context.Background())
	if err != nil {
		return fmt.Errorf("get feeds: %w", err)
	}

	for _, feed := range feeds {
		fmt.Printf("name: %s\n", feed.FeedName)
		fmt.Printf("url: %s\n", feed.Url)
		fmt.Printf("user: %s\n\n", feed.UserName)
	}

	return nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "not enough arguments provided")
		os.Exit(1)
	}

	cfg, err := config.Read()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading config: %v\n", err)
		os.Exit(1)
	}

	db, err := sql.Open("postgres", cfg.DBURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	dbQueries := database.New(db)

	s := &state{
		db:  dbQueries,
		cfg: &cfg,
	}

	cmds := &commands{
		handlers: make(map[string]func(*state, command) error),
	}
	cmds.register("login", handlerLogin)
	cmds.register("agg", handlerAgg)
	cmds.register("register", handlerRegister)
	cmds.register("reset", handlerReset)
	cmds.register("users", handlerUsers)
	cmds.register("addfeed", middlewareLoggedIn(handlerAddFeed))
	cmds.register("feeds", handlerFeeds)
	cmds.register("follow", middlewareLoggedIn(handlerFollow))
	cmds.register("following", middlewareLoggedIn(handlerFollowing))

	cmd := command{
		name: os.Args[1],
		args: os.Args[2:],
	}

	err = cmds.run(s, cmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

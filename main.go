package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/geirgulbrandsen/gator/internal/config"
	"github.com/geirgulbrandsen/gator/internal/database"
	"github.com/geirgulbrandsen/gator/internal/rss"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

var htmlTagRe = regexp.MustCompile(`<[^>]+>`)

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
	DeleteFeedFollow(context.Context, database.DeleteFeedFollowParams) error
	MarkFeedFetched(context.Context, uuid.UUID) error
	GetNextFeedToFetch(context.Context) (database.Feed, error)
	CreatePost(context.Context, database.CreatePostParams) error
	GetPostsForUser(context.Context, database.GetPostsForUserParams) ([]database.GetPostsForUserRow, error)
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

func scrapeFeeds(s *state) {
	ctx := context.Background()
	feed, err := s.db.GetNextFeedToFetch(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			fmt.Println("no feeds to fetch")
			return
		}

		fmt.Printf("error getting next feed to fetch: %v\n", err)
		return
	}

	if err := s.db.MarkFeedFetched(ctx, feed.ID); err != nil {
		fmt.Printf("error marking feed fetched: %v\n", err)
		return
	}

	fmt.Printf("Fetching feed: %s (%s)\n", feed.Name, feed.Url)
	rssFeed, err := rss.FetchFeed(ctx, feed.Url)
	if err != nil {
		fmt.Printf("error fetching feed %s: %v\n", feed.Url, err)
		return
	}

	now := time.Now().UTC()
	for _, item := range rssFeed.Channel.Item {
		title := stripHTML(item.Title)
		descriptionText := stripHTML(item.Description)
		description := sql.NullString{String: descriptionText, Valid: descriptionText != ""}
		publishedAt := parsePublishedAt(item.PubDate)

		err := s.db.CreatePost(ctx, database.CreatePostParams{
			ID:          uuid.New(),
			CreatedAt:   now,
			UpdatedAt:   now,
			Title:       title,
			Url:         strings.TrimSpace(item.Link),
			Description: description,
			PublishedAt: publishedAt,
			FeedID:      feed.ID,
		})
		if err != nil {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) && pqErr.Code == "23505" {
				continue
			}

			log.Printf("create post for %q failed: %v", item.Link, err)
			continue
		}

		fmt.Printf("  - saved: %s\n", title)
	}
}

func stripHTML(value string) string {
	withoutTags := htmlTagRe.ReplaceAllString(value, " ")
	return strings.Join(strings.Fields(withoutTags), " ")
}

func parsePublishedAt(value string) time.Time {
	layouts := []string{
		time.RFC1123Z,
		time.RFC1123,
		time.RFC822Z,
		time.RFC822,
		time.RFC3339,
		time.RFC3339Nano,
		"Mon, 2 Jan 2006 15:04:05 -0700",
		"Mon, 2 Jan 2006 15:04:05 MST",
	}

	for _, layout := range layouts {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed.UTC()
		}
	}

	log.Printf("could not parse published date %q, defaulting to now", value)
	return time.Now().UTC()
}

func handlerAgg(s *state, cmd command) error {
	if len(cmd.args) != 1 {
		return errors.New("usage: agg <time_between_reqs>")
	}

	timeBetweenRequests, err := time.ParseDuration(cmd.args[0])
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", cmd.args[0], err)
	}

	fmt.Printf("Collecting feeds every %s\n", timeBetweenRequests)

	ticker := time.NewTicker(timeBetweenRequests)
	for ; ; <-ticker.C {
		scrapeFeeds(s)
	}
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

func handlerUnfollow(s *state, cmd command, user database.User) error {
	if len(cmd.args) != 1 {
		return errors.New("usage: unfollow <url>")
	}

	ctx := context.Background()
	feed, err := s.db.GetFeedByURL(ctx, cmd.args[0])
	if err != nil {
		return fmt.Errorf("get feed by url: %w", err)
	}

	err = s.db.DeleteFeedFollow(ctx, database.DeleteFeedFollowParams{
		UserID: user.ID,
		FeedID: feed.ID,
	})
	if err != nil {
		return fmt.Errorf("delete feed follow: %w", err)
	}

	fmt.Printf("unfollowed: %s\n", feed.Name)

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

func handlerBrowse(s *state, cmd command, user database.User) error {
	if len(cmd.args) > 1 {
		return errors.New("usage: browse [limit]")
	}

	limit := int32(2)
	if len(cmd.args) == 1 {
		parsedLimit, err := strconv.Atoi(cmd.args[0])
		if err != nil || parsedLimit <= 0 {
			return fmt.Errorf("invalid limit %q", cmd.args[0])
		}
		limit = int32(parsedLimit)
	}

	posts, err := s.db.GetPostsForUser(context.Background(), database.GetPostsForUserParams{
		UserID: user.ID,
		Limit:  limit,
	})
	if err != nil {
		return fmt.Errorf("get posts for user: %w", err)
	}

	for _, post := range posts {
		fmt.Printf("title: %s\n", stripHTML(post.Title))
		fmt.Printf("url: %s\n", post.Url)
		if post.Description.Valid {
			fmt.Printf("description: %s\n", stripHTML(post.Description.String))
		}
		fmt.Printf("published_at: %s\n", post.PublishedAt)
		fmt.Printf("feed: %s\n\n", post.FeedName)
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
	cmds.register("unfollow", middlewareLoggedIn(handlerUnfollow))
	cmds.register("browse", middlewareLoggedIn(handlerBrowse))

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

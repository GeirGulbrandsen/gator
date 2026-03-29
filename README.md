# gator

`gator` is a CLI RSS aggregator written in Go with PostgreSQL.

## Requirements

You need these installed before running the program:

- Go (1.25+ recommended)
- PostgreSQL

## Install the CLI

From anywhere, install with `go install`:

```bash
go install github.com/geirgulbrandsen/gator@latest
```

That puts the `gator` binary in your Go bin directory (usually `$GOPATH/bin` or `$HOME/go/bin`).
Make sure that directory is in your `PATH`.

If you are developing locally in this repo, you can also install your local version with:

```bash
go install .
```

## Database setup

Create a Postgres database (example name: `gator`):

```bash
createdb gator
```

Then apply the schema migrations from `sql/schema`.

At minimum, your database needs tables for:

- `users`
- `feeds`
- `feed_follows`
- `posts`

## Configure gator

`gator` reads config from:

- `~/.gatorconfig.json`

Create this file:

```json
{
  "db_url": "postgres://YOUR_USER:YOUR_PASSWORD@localhost:5432/gator?sslmode=disable",
  "current_user_name": ""
}
```

The program updates `current_user_name` for you when you run `register` or `login`.

## Running commands

Example workflow:

```bash
gator register alice
gator addfeed "Boot.dev Blog" "https://www.boot.dev/blog/index.xml"
gator follow "https://techcrunch.com/feed/"
gator feeds
gator following
gator agg 1m
gator browse
gator browse 10
```

## Useful commands

- `register <username>`: create a user and set as current user
- `login <username>`: set current user
- `users`: list all users
- `addfeed <name> <url>`: add a feed and auto-follow it
- `feeds`: list all feeds
- `follow <url>`: follow an existing feed by URL
- `unfollow <url>`: unfollow a feed by URL
- `following`: list feeds followed by current user
- `agg <time_between_reqs>`: run the scraper loop (for example `1s`, `30s`, `1m`)
- `browse [limit]`: show recent posts from feeds you follow (default limit is `2`)
- `reset`: delete all users

## Notes

- `agg` is intended to run continuously; stop it with `Ctrl+C`.
- Keep request intervals reasonable so you do not spam feed servers.

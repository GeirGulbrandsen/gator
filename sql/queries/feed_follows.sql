-- name: CreateFeedFollow :one
WITH inserted_feed_follow AS (
    INSERT INTO feed_follows (id, created_at, updated_at, user_id, feed_id)
    VALUES (
        $1,
        $2,
        $3,
        $4,
        $5
    )
    RETURNING id, created_at, updated_at, user_id, feed_id
)
SELECT
    inserted_feed_follow.id,
    inserted_feed_follow.created_at,
    inserted_feed_follow.updated_at,
    inserted_feed_follow.user_id,
    users.name AS user_name,
    inserted_feed_follow.feed_id,
    feeds.name AS feed_name
FROM inserted_feed_follow
JOIN users ON inserted_feed_follow.user_id = users.id
JOIN feeds ON inserted_feed_follow.feed_id = feeds.id;

-- name: GetFeedFollowsForUser :many
SELECT
    feed_follows.id,
    feed_follows.created_at,
    feed_follows.updated_at,
    feed_follows.user_id,
    users.name AS user_name,
    feed_follows.feed_id,
    feeds.name AS feed_name
FROM feed_follows
JOIN users ON feed_follows.user_id = users.id
JOIN feeds ON feed_follows.feed_id = feeds.id
WHERE feed_follows.user_id = $1
ORDER BY feed_follows.created_at ASC;

-- name: DeleteFeedFollow :exec
DELETE FROM feed_follows
WHERE user_id = $1
    AND feed_id = $2;

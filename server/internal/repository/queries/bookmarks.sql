-- name: CreateBookmark :one
INSERT INTO bookmarks (id, channel_id, title, type, link, emoji, created_by, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
RETURNING id, channel_id, title, type, link, emoji, created_by, updated_by, created_at, updated_at;

-- name: GetBookmark :one
SELECT id, channel_id, title, type, link, emoji, created_by, updated_by, created_at, updated_at
FROM bookmarks WHERE id = $1;

-- name: UpdateBookmark :one
UPDATE bookmarks SET title = $2, link = $3, emoji = $4, updated_by = $5
WHERE id = $1
RETURNING id, channel_id, title, type, link, emoji, created_by, updated_by, created_at, updated_at;

-- name: DeleteBookmark :exec
DELETE FROM bookmarks WHERE id = $1;

-- name: ListBookmarks :many
SELECT id, channel_id, title, type, link, emoji, created_by, updated_by, created_at, updated_at
FROM bookmarks WHERE channel_id = $1
ORDER BY created_at ASC;

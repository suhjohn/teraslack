-- name: CreateFile :exec
INSERT INTO files (id, name, title, mimetype, filetype, size, user_id, s3_key,
                   url_private, url_private_download, permalink, is_external, external_url, upload_complete)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14);

-- name: GetFile :one
SELECT id, name, title, mimetype, filetype, size, user_id,
       url_private, url_private_download, permalink, is_external, external_url,
       created_at, updated_at
FROM files WHERE id = $1;

-- name: UpdateFileComplete :exec
UPDATE files SET title = $2, url_private = $3, url_private_download = $4,
                 permalink = $5, upload_complete = TRUE
WHERE id = $1;

-- name: DeleteFile :exec
DELETE FROM files WHERE id = $1;

-- name: ListFiles :many
SELECT id, name, title, mimetype, filetype, size, user_id,
       url_private, url_private_download, permalink, is_external, external_url,
       created_at, updated_at
FROM files
WHERE id > $1
ORDER BY id ASC
LIMIT $2;

-- name: ListFilesByUser :many
SELECT id, name, title, mimetype, filetype, size, user_id,
       url_private, url_private_download, permalink, is_external, external_url,
       created_at, updated_at
FROM files
WHERE user_id = $1 AND id > $2
ORDER BY id ASC
LIMIT $3;

-- name: ListFilesByChannel :many
SELECT f.id, f.name, f.title, f.mimetype, f.filetype, f.size, f.user_id,
       f.url_private, f.url_private_download, f.permalink, f.is_external, f.external_url,
       f.created_at, f.updated_at
FROM files f
INNER JOIN file_channels fc ON f.id = fc.file_id
WHERE fc.channel_id = $1 AND f.id > $2
ORDER BY f.id ASC
LIMIT $3;

-- name: ListFilesByChannelAndUser :many
SELECT f.id, f.name, f.title, f.mimetype, f.filetype, f.size, f.user_id,
       f.url_private, f.url_private_download, f.permalink, f.is_external, f.external_url,
       f.created_at, f.updated_at
FROM files f
INNER JOIN file_channels fc ON f.id = fc.file_id
WHERE fc.channel_id = $1 AND f.user_id = $2 AND f.id > $3
ORDER BY f.id ASC
LIMIT $4;

-- name: ShareFileToChannel :exec
INSERT INTO file_channels (file_id, channel_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: GetFileChannels :many
SELECT channel_id FROM file_channels WHERE file_id = $1 ORDER BY shared_at ASC;

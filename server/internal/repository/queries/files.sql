-- name: CreateFile :exec
INSERT INTO files (id, workspace_id, name, title, mimetype, filetype, size, user_id, s3_key,
                   url_private, url_private_download, permalink, is_external, external_url, upload_complete)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15);

-- name: GetFile :one
SELECT id, workspace_id, name, title, mimetype, filetype, size, user_id,
       url_private, url_private_download, permalink, is_external, external_url,
       created_at, updated_at
FROM files WHERE workspace_id = $1 AND id = $2;

-- name: GetFileByID :one
SELECT id, workspace_id, name, title, mimetype, filetype, size, user_id,
       url_private, url_private_download, permalink, is_external, external_url,
       created_at, updated_at
FROM files WHERE id = $1;

-- name: UpdateFileComplete :exec
UPDATE files SET title = $3, url_private = $4, url_private_download = $5,
                 permalink = $6, upload_complete = TRUE
WHERE workspace_id = $1 AND id = $2;

-- name: DeleteFile :exec
DELETE FROM files WHERE workspace_id = $1 AND id = $2;

-- name: ListFiles :many
SELECT id, workspace_id, name, title, mimetype, filetype, size, user_id,
       url_private, url_private_download, permalink, is_external, external_url,
       created_at, updated_at
FROM files
WHERE workspace_id = $1 AND id > $2
ORDER BY id ASC
LIMIT $3;

-- name: ListFilesByUser :many
SELECT id, workspace_id, name, title, mimetype, filetype, size, user_id,
       url_private, url_private_download, permalink, is_external, external_url,
       created_at, updated_at
FROM files
WHERE workspace_id = $1 AND user_id = $2 AND id > $3
ORDER BY id ASC
LIMIT $4;

-- name: ListFilesByChannel :many
SELECT f.id, f.workspace_id, f.name, f.title, f.mimetype, f.filetype, f.size, f.user_id,
       f.url_private, f.url_private_download, f.permalink, f.is_external, f.external_url,
       f.created_at, f.updated_at
FROM files f
INNER JOIN file_channels fc ON f.id = fc.file_id
WHERE f.workspace_id = $1 AND fc.channel_id = $2 AND f.id > $3
ORDER BY f.id ASC
LIMIT $4;

-- name: ListFilesByChannelAndUser :many
SELECT f.id, f.workspace_id, f.name, f.title, f.mimetype, f.filetype, f.size, f.user_id,
       f.url_private, f.url_private_download, f.permalink, f.is_external, f.external_url,
       f.created_at, f.updated_at
FROM files f
INNER JOIN file_channels fc ON f.id = fc.file_id
WHERE f.workspace_id = $1 AND fc.channel_id = $2 AND f.user_id = $3 AND f.id > $4
ORDER BY f.id ASC
LIMIT $5;

-- name: ShareFileToChannel :execrows
INSERT INTO file_channels (file_id, channel_id)
SELECT $2, $3
FROM files f
JOIN conversations c ON c.id = $3 AND c.owner_workspace_id = f.workspace_id
WHERE f.workspace_id = $1 AND f.id = $2
ON CONFLICT DO NOTHING;

-- name: GetFileChannels :many
SELECT channel_id FROM file_channels WHERE file_id = $1 ORDER BY shared_at ASC;

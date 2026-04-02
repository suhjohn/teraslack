package domain

import "time"

// File represents an uploaded file.
type File struct {
	ID                 string    `json:"id"`
	WorkspaceID        string    `json:"workspace_id"`
	Name               string    `json:"name"`
	Title              string    `json:"title"`
	Mimetype           string    `json:"mimetype"`
	Filetype           string    `json:"filetype"`
	Size               int64     `json:"size"`
	UserID             string    `json:"user_id"`
	URLPrivate         string    `json:"url_private"`
	URLPrivateDownload string    `json:"url_private_download"`
	Permalink          string    `json:"permalink"`
	IsExternal         bool      `json:"is_external"`
	ExternalURL        string    `json:"external_url,omitempty"`
	Channels           []string  `json:"channels,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// GetUploadURLParams holds the parameters for getting an upload URL.
type GetUploadURLParams struct {
	Filename  string `json:"filename"`
	Length    int64  `json:"length"`
	ChannelID string `json:"channel_id,omitempty"`
}

// GetUploadURLResponse holds the response for getting an upload URL.
type GetUploadURLResponse struct {
	UploadURL string `json:"upload_url"`
	FileID    string `json:"file_id"`
}

// CompleteUploadParams holds the parameters for completing a file upload.
type CompleteUploadParams struct {
	FileID    string `json:"file_id"`
	Title     string `json:"title,omitempty"`
	ChannelID string `json:"channel_id,omitempty"`
	ThreadTS  string `json:"thread_ts,omitempty"`
}

// ListFilesParams holds the parameters for listing files.
type ListFilesParams struct {
	WorkspaceID string `json:"workspace_id"`
	ChannelID   string `json:"channel_id,omitempty"`
	UserID      string `json:"user_id,omitempty"`
	Cursor      string `json:"cursor"`
	Limit       int    `json:"limit"`
}

// AddRemoteFileParams holds the parameters for adding a remote file.
type AddRemoteFileParams struct {
	Title       string `json:"title"`
	ExternalURL string `json:"external_url"`
	Filetype    string `json:"filetype"`
	UserID      string `json:"user_id"`
	ChannelID   string `json:"channel_id,omitempty"`
}

// ShareRemoteFileParams holds the parameters for sharing a remote file.
type ShareRemoteFileParams struct {
	FileID   string   `json:"file_id"`
	Channels []string `json:"channels"`
}

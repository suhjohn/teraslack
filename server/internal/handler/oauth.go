package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/johnsuh/teraslack/server/internal/api"
	teracrypto "github.com/johnsuh/teraslack/server/internal/crypto"
	"github.com/johnsuh/teraslack/server/internal/dbsqlc"
)

func (s *Server) handleOAuthStart(provider string, clientID string, callbackURL string, w http.ResponseWriter, r *http.Request) {
	if !s.limiter.allow("auth:oauth:start:"+provider+":ip:"+clientIP(r), 30, time.Hour) {
		s.writeAppError(w, r, rateLimited("Too many OAuth start attempts from this IP."))
		return
	}
	if clientID == "" {
		s.writeAppError(w, r, &appError{
			Status:  http.StatusNotImplemented,
			Code:    "oauth_not_configured",
			Message: strings.Title(provider) + " OAuth is not configured.",
		})
		return
	}
	var request api.OAuthStartRequest
	if err := decodeJSON(r, &request); err != nil {
		s.writeAppError(w, r, err)
		return
	}
	if err := expectURL(request.RedirectURI, "redirect_uri"); err != nil {
		s.writeAppError(w, r, err)
		return
	}
	state, err := teracrypto.RandomToken(24)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	now := time.Now().UTC()
	if err := s.queries.CreateOAuthState(r.Context(), dbsqlc.CreateOAuthStateParams{
		ID:          uuid.New(),
		Provider:    provider,
		StateHash:   teracrypto.SHA256Hex(state),
		RedirectUri: &request.RedirectURI,
		ExpiresAt:   dbsqlc.Timestamptz(now.Add(10 * time.Minute)),
		CreatedAt:   dbsqlc.Timestamptz(now),
	}); err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	authURL := buildOAuthAuthorizeURL(provider, clientID, callbackURL, state)
	writeJSON(w, http.StatusOK, api.OAuthStartResponse{
		AuthURL: authURL,
		State:   state,
	})
}

func (s *Server) handleOAuthCallback(provider string, w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if code == "" || state == "" {
		s.writeAppError(w, r, malformed("OAuth callback is missing code or state."))
		return
	}

	stateRow, err := s.queries.GetOAuthStateByHash(r.Context(), dbsqlc.GetOAuthStateByHashParams{
		Provider:  provider,
		StateHash: teracrypto.SHA256Hex(state),
		NowAt:     dbsqlc.Timestamptz(time.Now().UTC()),
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			s.writeAppError(w, r, unauthorized("OAuth state is invalid or expired."))
			return
		}
		s.writeAppError(w, r, internalError(err))
		return
	}

	token, err := s.exchangeOAuthCode(r.Context(), provider, code)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}
	providerUserID, email, err := s.fetchOAuthIdentity(r.Context(), provider, token)
	if err != nil {
		s.writeAppError(w, r, internalError(err))
		return
	}

	var authResponse api.AuthResponse
	var sessionToken string
	var sessionID uuid.UUID
	err = withTransaction(r.Context(), s.db, func(tx pgx.Tx) error {
		txQueries := s.queries.WithTx(tx)
		if err := txQueries.DeleteOAuthState(r.Context(), stateRow.ID); err != nil {
			return err
		}
		user, created, err := s.resolveOrCreateOAuthUserTx(r.Context(), tx, provider, providerUserID, email)
		if err != nil {
			return err
		}
		if created {
			userID := user.ID
			if err := s.appendEvent(r.Context(), tx, "user.created", "user", user.ID, nil, &userID, map[string]any{
				"user_id": user.ID.String(),
				"email":   email,
			}); err != nil {
				return err
			}
		}
		sessionID, sessionToken, authResponse.Session, err = s.createSessionTx(r.Context(), tx, user.ID)
		if err != nil {
			return err
		}
		userID := user.ID
		if err := s.appendEvent(r.Context(), tx, "auth.session.created", "auth_session", sessionID, nil, &userID, map[string]any{
			"session_id": sessionID.String(),
			"user_id":    user.ID.String(),
		}); err != nil {
			return err
		}
		authResponse.User = userToAPI(user)
		return nil
	})
	if err != nil {
		if appErr, ok := err.(*appError); ok {
			s.writeAppError(w, r, appErr)
			return
		}
		s.writeAppError(w, r, internalError(err))
		return
	}

	s.setSessionCookie(w, sessionToken)
	if stateRow.RedirectUri != nil && strings.TrimSpace(*stateRow.RedirectUri) != "" {
		http.Redirect(w, r, *stateRow.RedirectUri, http.StatusFound)
		return
	}
	writeJSON(w, http.StatusOK, authResponse)
}

func buildOAuthAuthorizeURL(provider string, clientID string, callbackURL string, state string) string {
	values := url.Values{}
	values.Set("client_id", clientID)
	values.Set("redirect_uri", callbackURL)
	values.Set("state", state)
	switch provider {
	case "google":
		values.Set("response_type", "code")
		values.Set("scope", "openid email profile")
		values.Set("access_type", "online")
		values.Set("prompt", "select_account")
		return "https://accounts.google.com/o/oauth2/v2/auth?" + values.Encode()
	case "github":
		values.Set("scope", "user:email")
		return "https://github.com/login/oauth/authorize?" + values.Encode()
	default:
		return ""
	}
}

func (s *Server) exchangeOAuthCode(ctx context.Context, provider string, code string) (string, error) {
	values := url.Values{}
	values.Set("code", code)
	var endpoint string
	switch provider {
	case "google":
		endpoint = "https://oauth2.googleapis.com/token"
		values.Set("client_id", s.cfg.GoogleOAuthClientID)
		values.Set("client_secret", s.cfg.GoogleOAuthClientSecret)
		values.Set("redirect_uri", strings.TrimRight(s.cfg.BaseURL, "/")+"/auth/oauth/google/callback")
		values.Set("grant_type", "authorization_code")
	case "github":
		endpoint = "https://github.com/login/oauth/access_token"
		values.Set("client_id", s.cfg.GitHubOAuthClientID)
		values.Set("client_secret", s.cfg.GitHubOAuthClientSecret)
		values.Set("redirect_uri", strings.TrimRight(s.cfg.BaseURL, "/")+"/auth/oauth/github/callback")
	default:
		return "", fmt.Errorf("unsupported provider %q", provider)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("%s token exchange failed with status %d", provider, resp.StatusCode)
	}
	var decoded struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		return "", err
	}
	if decoded.AccessToken == "" {
		return "", fmt.Errorf("%s token exchange returned no access token", provider)
	}
	return decoded.AccessToken, nil
}

func (s *Server) fetchOAuthIdentity(ctx context.Context, provider string, accessToken string) (providerUserID string, email string, err error) {
	switch provider {
	case "google":
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.googleapis.com/oauth2/v2/userinfo", nil)
		if err != nil {
			return "", "", err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		resp, err := s.httpClient.Do(req)
		if err != nil {
			return "", "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			return "", "", fmt.Errorf("google userinfo returned status %d", resp.StatusCode)
		}
		var payload struct {
			ID            string `json:"id"`
			Email         string `json:"email"`
			VerifiedEmail bool   `json:"verified_email"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			return "", "", err
		}
		if !payload.VerifiedEmail {
			return payload.ID, "", nil
		}
		return payload.ID, normalizeEmail(payload.Email), nil
	case "github":
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
		if err != nil {
			return "", "", err
		}
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Accept", "application/vnd.github+json")
		resp, err := s.httpClient.Do(req)
		if err != nil {
			return "", "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 300 {
			return "", "", fmt.Errorf("github user returned status %d", resp.StatusCode)
		}
		var payload struct {
			ID    int64   `json:"id"`
			Email *string `json:"email"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			return "", "", err
		}
		email := ""
		if payload.Email != nil {
			email = normalizeEmail(*payload.Email)
		}
		if email == "" {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user/emails", nil)
			if err != nil {
				return "", "", err
			}
			req.Header.Set("Authorization", "Bearer "+accessToken)
			req.Header.Set("Accept", "application/vnd.github+json")
			resp, err := s.httpClient.Do(req)
			if err != nil {
				return "", "", err
			}
			defer resp.Body.Close()
			if resp.StatusCode >= 300 {
				return "", "", fmt.Errorf("github user emails returned status %d", resp.StatusCode)
			}
			var emails []struct {
				Email    string `json:"email"`
				Primary  bool   `json:"primary"`
				Verified bool   `json:"verified"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
				return "", "", err
			}
			for _, candidate := range emails {
				if candidate.Primary && candidate.Verified {
					email = normalizeEmail(candidate.Email)
					break
				}
			}
			if email == "" {
				for _, candidate := range emails {
					if candidate.Verified {
						email = normalizeEmail(candidate.Email)
						break
					}
				}
			}
		}
		return strconv.FormatInt(payload.ID, 10), email, nil
	default:
		return "", "", fmt.Errorf("unsupported provider %q", provider)
	}
}

func (s *Server) resolveOrCreateOAuthUserTx(ctx context.Context, tx pgx.Tx, provider string, providerUserID string, email string) (userRow, bool, error) {
	queries := s.queries.WithTx(tx)
	row, err := queries.GetUserByOAuthAccount(ctx, dbsqlc.GetUserByOAuthAccountParams{
		Provider:       provider,
		ProviderUserID: providerUserID,
	})
	if err == nil {
		return userRow{
			ID:            row.ID,
			PrincipalType: row.PrincipalType,
			Status:        row.Status,
			Email:         row.Email,
			Handle:        row.Handle,
			DisplayName:   row.DisplayName,
			AvatarURL:     row.AvatarUrl,
			Bio:           row.Bio,
		}, false, nil
	}
	if err != pgx.ErrNoRows {
		return userRow{}, false, err
	}

	var user userRow
	created := false
	if email != "" {
		user, created, err = s.resolveOrCreateUserByEmailTx(ctx, tx, email)
		if err != nil {
			return userRow{}, false, err
		}
	} else {
		user, err = s.insertUserWithProfile(ctx, tx, "user+"+uuid.NewString()+"@example.invalid")
		if err != nil {
			return userRow{}, false, err
		}
		created = true
	}
	now := time.Now().UTC()
	accountEmail := stringPtr(email)
	if strings.TrimSpace(email) == "" {
		accountEmail = nil
	}
	if err := queries.UpsertOAuthAccount(ctx, dbsqlc.UpsertOAuthAccountParams{
		ID:             uuid.New(),
		Provider:       provider,
		ProviderUserID: providerUserID,
		UserID:         user.ID,
		Email:          accountEmail,
		CreatedAt:      dbsqlc.Timestamptz(now),
		UpdatedAt:      dbsqlc.Timestamptz(now),
	}); err != nil {
		return userRow{}, false, err
	}
	return user, created, nil
}

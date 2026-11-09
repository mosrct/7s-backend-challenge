package httpapi

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"user-management/internal/application"
	"user-management/internal/domain"
)

// The `// @...` comments on each handler are swag annotations. They are the
// source of truth for docs/swagger.{json,yaml}; run `make swagger` after
// changing them.

type handler struct {
	users  *application.UserService
	auth   *application.AuthService
	logger *slog.Logger
}

// health reports liveness.
//
//	@Summary	Liveness check
//	@Tags		health
//	@Produce	json
//	@Success	200	{object}	healthResponse
//	@Router		/healthz [get]
func (h *handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{Status: "ok"})
}

// register creates a new account.
//
//	@Summary		Register a new account
//	@Description	Public sign-up. Email is trimmed, lower-cased and must be unique. Password: 8 characters to 72 bytes.
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		createUserRequest	true	"New account"
//	@Success		201		{object}	userResponse
//	@Header			201		{string}	Location	"/api/v1/users/{id}"
//	@Failure		400		{object}	errorResponse	"Validation failed or malformed JSON"
//	@Failure		409		{object}	errorResponse	"Email already in use"
//	@Router			/api/v1/auth/register [post]
func (h *handler) register(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, h.logger, err)
		return
	}
	user, err := h.auth.Register(r.Context(), application.CreateUserInput(req))
	if err != nil {
		writeError(w, h.logger, err)
		return
	}
	w.Header().Set("Location", "/api/v1/users/"+user.ID)
	writeJSON(w, http.StatusCreated, toUserResponse(user))
}

// login exchanges credentials for a JWT.
//
//	@Summary		Log in and get a JWT
//	@Description	Returns an HS256-signed access token. Use it as `Authorization: Bearer <access_token>` (in Swagger UI: click "Authorize").
//	@Tags			auth
//	@Accept			json
//	@Produce		json
//	@Param			body	body		loginRequest	true	"Credentials"
//	@Success		200		{object}	loginResponse
//	@Failure		400		{object}	errorResponse	"Missing email or password"
//	@Failure		401		{object}	errorResponse	"Invalid email or password"
//	@Router			/api/v1/auth/login [post]
func (h *handler) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, h.logger, err)
		return
	}
	res, err := h.auth.Login(r.Context(), application.LoginInput(req))
	if err != nil {
		writeError(w, h.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, loginResponse{
		AccessToken: res.Token,
		TokenType:   "Bearer",
		ExpiresAt:   res.ExpiresAt,
		ExpiresIn:   int64(time.Until(res.ExpiresAt).Seconds()),
		User:        toUserResponse(res.User),
	})
}

// createUser creates a user on behalf of an authenticated caller.
//
//	@Summary		Create a user
//	@Description	Same rules as registration, but requires authentication.
//	@Tags			users
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			body	body		createUserRequest	true	"New user"
//	@Success		201		{object}	userResponse
//	@Header			201		{string}	Location	"/api/v1/users/{id}"
//	@Failure		400		{object}	errorResponse	"Validation failed or malformed JSON"
//	@Failure		401		{object}	errorResponse	"Missing or invalid token"
//	@Failure		409		{object}	errorResponse	"Email already in use"
//	@Router			/api/v1/users [post]
func (h *handler) createUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, h.logger, err)
		return
	}
	user, err := h.users.Create(r.Context(), application.CreateUserInput(req))
	if err != nil {
		writeError(w, h.logger, err)
		return
	}
	w.Header().Set("Location", "/api/v1/users/"+user.ID)
	writeJSON(w, http.StatusCreated, toUserResponse(user))
}

// listUsers returns a page of users.
//
//	@Summary	List users
//	@Tags		users
//	@Produce	json
//	@Security	BearerAuth
//	@Param		offset	query		int	false	"Number of users to skip"	default(0)	minimum(0)
//	@Param		limit	query		int	false	"Page size"					default(50)	minimum(1)	maximum(100)
//	@Success	200		{object}	listUsersResponse
//	@Failure	400		{object}	errorResponse	"Invalid pagination"
//	@Failure	401		{object}	errorResponse	"Missing or invalid token"
//	@Router		/api/v1/users [get]
func (h *handler) listUsers(w http.ResponseWriter, r *http.Request) {
	in, err := parseListQuery(r)
	if err != nil {
		writeError(w, h.logger, err)
		return
	}
	page, err := h.users.List(r.Context(), in)
	if err != nil {
		writeError(w, h.logger, err)
		return
	}
	resp := listUsersResponse{
		Users:  make([]userResponse, 0, len(page.Users)),
		Total:  page.Total,
		Offset: page.Offset,
		Limit:  page.Limit,
	}
	for i := range page.Users {
		resp.Users = append(resp.Users, toUserResponse(&page.Users[i]))
	}
	writeJSON(w, http.StatusOK, resp)
}

func parseListQuery(r *http.Request) (application.ListUsersInput, error) {
	var in application.ListUsersInput
	verr := domain.NewValidationError()
	q := r.URL.Query()
	if s := q.Get("offset"); s != "" {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			verr.Add("offset", "must be an integer")
		}
		in.Offset = v
	}
	if s := q.Get("limit"); s != "" {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil || v == 0 {
			verr.Add("limit", "must be a positive integer")
		}
		in.Limit = v
	}
	return in, verr.OrNil()
}

// getUser fetches a user by ID.
//
//	@Summary	Get a user by ID
//	@Tags		users
//	@Produce	json
//	@Security	BearerAuth
//	@Param		id	path		string	true	"User ID (24-char hex)"	example(6af0532d5b25c4c7a5e43caa)
//	@Success	200	{object}	userResponse
//	@Failure	401	{object}	errorResponse	"Missing or invalid token"
//	@Failure	404	{object}	errorResponse	"User not found"
//	@Router		/api/v1/users/{id} [get]
func (h *handler) getUser(w http.ResponseWriter, r *http.Request) {
	user, err := h.users.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, h.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, toUserResponse(user))
}

// updateUser changes the caller's own name and/or email.
//
//	@Summary		Update your own name and/or email
//	@Description	Partial update: send only the fields to change. Only the account owner may update it (token `sub` must equal `id`).
//	@Tags			users
//	@Accept			json
//	@Produce		json
//	@Security		BearerAuth
//	@Param			id		path		string				true	"User ID (must be your own)"
//	@Param			body	body		updateUserRequest	true	"Fields to change"
//	@Success		200		{object}	userResponse
//	@Failure		400		{object}	errorResponse	"Validation failed, empty body or unknown field"
//	@Failure		401		{object}	errorResponse	"Missing or invalid token"
//	@Failure		403		{object}	errorResponse	"Not your account"
//	@Failure		404		{object}	errorResponse	"User not found"
//	@Failure		409		{object}	errorResponse	"Email already in use"
//	@Router			/api/v1/users/{id} [patch]
func (h *handler) updateUser(w http.ResponseWriter, r *http.Request) {
	claims, _ := ClaimsFromContext(r.Context())
	var req updateUserRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, h.logger, err)
		return
	}
	user, err := h.users.Update(r.Context(), claims.UserID, r.PathValue("id"), application.UpdateUserInput(req))
	if err != nil {
		writeError(w, h.logger, err)
		return
	}
	writeJSON(w, http.StatusOK, toUserResponse(user))
}

// deleteUser deletes the caller's own account.
//
//	@Summary		Delete your own account
//	@Description	Only the account owner may delete it. Note: already-issued tokens stay valid until they expire.
//	@Tags			users
//	@Security		BearerAuth
//	@Param			id	path	string	true	"User ID (must be your own)"
//	@Success		204	"Deleted"
//	@Failure		401	{object}	errorResponse	"Missing or invalid token"
//	@Failure		403	{object}	errorResponse	"Not your account"
//	@Failure		404	{object}	errorResponse	"User not found"
//	@Router			/api/v1/users/{id} [delete]
func (h *handler) deleteUser(w http.ResponseWriter, r *http.Request) {
	claims, _ := ClaimsFromContext(r.Context())
	if err := h.users.Delete(r.Context(), claims.UserID, r.PathValue("id")); err != nil {
		writeError(w, h.logger, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

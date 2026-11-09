package httpapi

import (
	"time"

	"user-management/internal/domain"
)

// The `example`, `minLength`, `maxLength` and `format` tags, and the trailing
// `@name` comments, are read by swag to build the OpenAPI spec (docs/).

type createUserRequest struct {
	Name     string `json:"name" example:"Alice" minLength:"1" maxLength:"100"`
	Email    string `json:"email" example:"alice@example.com" format:"email" maxLength:"254"`
	Password string `json:"password" example:"s3cure-pass" minLength:"8" maxLength:"72"`
} // @name CreateUserRequest

type loginRequest struct {
	Email    string `json:"email" example:"alice@example.com" format:"email"`
	Password string `json:"password" example:"s3cure-pass"`
} // @name LoginRequest

// updateUserRequest uses pointers to distinguish "absent" from "empty".
type updateUserRequest struct {
	Name  *string `json:"name,omitempty" example:"Alice Smith" minLength:"1" maxLength:"100"`
	Email *string `json:"email,omitempty" example:"alice.smith@example.com" format:"email"`
} // @name UpdateUserRequest

// userResponse never exposes the password hash.
type userResponse struct {
	ID        string    `json:"id" example:"6af0532d5b25c4c7a5e43caa"`
	Name      string    `json:"name" example:"Alice"`
	Email     string    `json:"email" example:"alice@example.com"`
	CreatedAt time.Time `json:"created_at" example:"2026-11-08T10:47:09.956Z"`
} // @name User

func toUserResponse(u *domain.User) userResponse {
	return userResponse{ID: u.ID, Name: u.Name, Email: u.Email, CreatedAt: u.CreatedAt}
}

type listUsersResponse struct {
	Users  []userResponse `json:"users"`
	Total  int64          `json:"total" example:"2"`
	Offset int64          `json:"offset" example:"0"`
	Limit  int64          `json:"limit" example:"50"`
} // @name UserList

type loginResponse struct {
	AccessToken string       `json:"access_token" example:"eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..."`
	TokenType   string       `json:"token_type" example:"Bearer"`
	ExpiresAt   time.Time    `json:"expires_at" example:"2026-11-08T11:47:10Z"`
	ExpiresIn   int64        `json:"expires_in" example:"3599"`
	User        userResponse `json:"user"`
} // @name LoginResponse

type errorResponse struct {
	Error   string            `json:"error" example:"validation failed"`
	Details map[string]string `json:"details,omitempty"`
} // @name Error

type healthResponse struct {
	Status string `json:"status" example:"ok"`
} // @name Health

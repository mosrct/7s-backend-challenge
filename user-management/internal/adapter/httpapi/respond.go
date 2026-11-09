package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"user-management/internal/domain"
)

const maxBodyBytes = 1 << 20 // 1 MiB

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// writeError maps domain errors to HTTP status codes. Unknown errors become
// 500 and are logged without leaking details to the client.
func writeError(w http.ResponseWriter, logger *slog.Logger, err error) {
	var verr *domain.ValidationError
	switch {
	case errors.As(err, &verr):
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "validation failed", Details: verr.Fields})
	case errors.Is(err, domain.ErrUserNotFound):
		writeJSON(w, http.StatusNotFound, errorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrEmailTaken):
		writeJSON(w, http.StatusConflict, errorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrInvalidCredentials), errors.Is(err, domain.ErrUnauthenticated):
		writeJSON(w, http.StatusUnauthorized, errorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrForbidden):
		writeJSON(w, http.StatusForbidden, errorResponse{Error: err.Error()})
	default:
		if logger != nil {
			logger.Error("unhandled error", "error", err)
		}
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal server error"})
	}
}

// decodeJSON strictly decodes a single JSON object from the request body.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		switch {
		case errors.As(err, &maxErr):
			return badRequest("body", "must not exceed 1 MiB")
		case errors.Is(err, io.EOF):
			return badRequest("body", "must not be empty")
		default:
			return badRequest("body", fmt.Sprintf("invalid JSON: %v", err))
		}
	}
	if dec.More() {
		return badRequest("body", "must contain a single JSON object")
	}
	return nil
}

func badRequest(field, msg string) error {
	v := domain.NewValidationError()
	v.Add(field, msg)
	return v
}

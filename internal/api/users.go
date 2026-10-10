package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"umirhack/internal/auth"
)

// codeConflict — 409 для пользователей: в 7.4 общего кода конфликта нет (допущение, см. PR).
const codeConflict = "conflict"

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		validationError(w, "id", "id must be a positive integer")
		return 0, false
	}
	return id, true
}

func (rt *Router) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := rt.deps.Auth.Repo.ListUsers(r.Context())
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": users})
}

func (rt *Router) createUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Login    string    `json:"login"`
		Password string    `json:"password"`
		Role     auth.Role `json:"role"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	switch {
	case !validLabel(in.Login):
		validationError(w, "login", "login must be 1..128 characters without control characters or edge spaces")
		return
	case len(in.Password) < minPassBytes || len(in.Password) > maxPassBytes:
		validationError(w, "password", "password must be 8..72 bytes")
		return
	case !in.Role.Valid():
		validationError(w, "role", "role must be viewer, operator or admin")
		return
	}
	u, err := rt.deps.Auth.CreateUser(r.Context(), in.Login, in.Password, in.Role)
	switch {
	case err == nil:
	case errors.Is(err, auth.ErrExists):
		WriteError(w, http.StatusConflict, codeConflict, "user with this login already exists", nil)
		return
	default:
		rt.internalError(w, r, err)
		return
	}
	rt.audit(r, "user_created", slog.Int64("target_user_id", u.ID), slog.String("target_login", u.Login), slog.String("target_role", string(u.Role)))
	WriteJSON(w, http.StatusCreated, u)
}

func (rt *Router) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	switch err := rt.deps.Auth.Repo.DeleteUser(r.Context(), id); {
	case err == nil:
	case errors.Is(err, auth.ErrNotFound):
		WriteError(w, http.StatusNotFound, CodeNotFound, "user not found", nil)
		return
	case errors.Is(err, auth.ErrLastAdmin):
		WriteError(w, http.StatusConflict, codeConflict, "the last admin cannot be deleted", nil)
		return
	default:
		rt.internalError(w, r, err)
		return
	}
	rt.audit(r, "user_deleted", slog.Int64("target_user_id", id))
	w.WriteHeader(http.StatusNoContent)
}

func (rt *Router) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := rt.deps.Auth.Repo.ListAPIKeys(r.Context())
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": keys})
}

func (rt *Router) createAPIKey(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string    `json:"name"`
		Role auth.Role `json:"role"`
	}
	if !decodeBody(w, r, &in) {
		return
	}
	switch {
	case !validLabel(in.Name):
		validationError(w, "name", "name must be 1..128 characters without control characters or edge spaces")
		return
	case in.Role != auth.Viewer && in.Role != auth.Operator:
		validationError(w, "role", "role must be viewer or operator")
		return
	}
	k, key, err := rt.deps.Auth.CreateAPIKey(r.Context(), in.Name, in.Role)
	if err != nil {
		rt.internalError(w, r, err)
		return
	}
	rt.audit(r, "api_key_created", slog.Int64("target_key_id", k.ID), slog.String("target_name", k.Name), slog.String("target_role", string(k.Role)))
	WriteJSON(w, http.StatusCreated, map[string]any{
		"id": k.ID, "name": k.Name, "role": k.Role, "created_at": k.CreatedAt, "key": key,
	})
}

func (rt *Router) deleteAPIKey(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	switch err := rt.deps.Auth.Repo.DeleteAPIKey(r.Context(), id); {
	case err == nil:
	case errors.Is(err, auth.ErrNotFound):
		WriteError(w, http.StatusNotFound, CodeNotFound, "api key not found", nil)
		return
	default:
		rt.internalError(w, r, err)
		return
	}
	rt.audit(r, "api_key_deleted", slog.Int64("target_key_id", id))
	w.WriteHeader(http.StatusNoContent)
}

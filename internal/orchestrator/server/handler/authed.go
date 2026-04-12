package handler

import (
	"net/http"

	orchestrator "github.com/Crawbl-AI/crawbl-backend/internal/orchestrator"
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"
)

// AuthedHandlerDeps bundles the per-request dependencies that every authed
// handler needs: the handler context and the authenticated user. Handlers
// receive this struct so they have everything required to talk to services
// without re-plumbing boilerplate. The database session is carried on the
// request context via the session middleware.
type AuthedHandlerDeps struct {
	// Ctx is the shared handler context (services, logger, broadcaster, ...).
	Ctx *Context
	// User is the authenticated, non-banned, non-deleted caller.
	User *orchestrator.User
}

// AuthedJSONFunc is the business logic signature for handlers that read a
// JSON request body. The decorator decodes the body into Req before calling.
// The handler returns the response payload (wrapped in the success envelope
// automatically) and an optional domain error.
type AuthedJSONFunc[Req any, Resp any] func(
	r *http.Request,
	deps *AuthedHandlerDeps,
	req *Req,
) (Resp, *merrors.Error)

// AuthedFunc is the business logic signature for handlers that do not read a
// request body (GET, DELETE, or handlers that pull all inputs from URL/query
// params). The decorator skips body decoding entirely.
type AuthedFunc[Resp any] func(
	r *http.Request,
	deps *AuthedHandlerDeps,
) (Resp, *merrors.Error)

// AuthedHandler wires a JSON-bodied business function into an http.HandlerFunc.
//
// It centralises the boilerplate that every handler otherwise duplicates:
//  1. Load the current user via Context.CurrentUser (rejects banned/deleted).
//  2. Decode the JSON body into *Req (ErrInvalidInput on failure).
//  3. Invoke the business closure with the decoded body and deps.
//  4. Map any *merrors.Error to the correct HTTP status via WriteError.
//  5. Write the response through the {"data": ...} success envelope on 200.
//
// Handlers should prefer this decorator when they match the JSON-in /
// envelope-out shape. Complex flows (streaming, custom status codes,
// multipart uploads, no-content responses) should continue to use the
// plain http.HandlerFunc form.
func AuthedHandler[Req any, Resp any](
	c *Context,
	fn AuthedJSONFunc[Req, Resp],
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		var req Req
		if err := DecodeJSON(r, &req); err != nil {
			WriteError(w, merrors.ErrInvalidInput)
			return
		}

		deps := &AuthedHandlerDeps{
			Ctx:  c,
			User: user,
		}

		resp, mErr := fn(r, deps, &req)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		WriteSuccess(w, http.StatusOK, resp)
	}
}

// AuthedHandlerNoBody wires a no-body business function into an
// http.HandlerFunc. It behaves identically to AuthedHandler but skips the
// JSON body decode step, which is correct for GET / DELETE endpoints that
// pull all input from URL params and query strings.
func AuthedHandlerNoBody[Resp any](
	c *Context,
	fn AuthedFunc[Resp],
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		deps := &AuthedHandlerDeps{
			Ctx:  c,
			User: user,
		}

		resp, mErr := fn(r, deps)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		WriteSuccess(w, http.StatusOK, resp)
	}
}

// AuthedHandlerNoContent wires a no-body, no-response business function into
// an http.HandlerFunc. It centralises the "write, return 204" flow used by
// DELETE endpoints and side-effect-only POSTs.
//
// If the business function needs a request body, wrap AuthedHandler and
// ignore the Resp instead; this variant is for handlers that carry neither.
func AuthedHandlerNoContent(
	c *Context,
	fn func(r *http.Request, deps *AuthedHandlerDeps) *merrors.Error,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		deps := &AuthedHandlerDeps{
			Ctx:  c,
			User: user,
		}

		if mErr := fn(r, deps); mErr != nil {
			WriteError(w, mErr)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// AuthedHandlerCreated wires a JSON-bodied business function into an
// http.HandlerFunc that responds with 201 Created instead of 200 OK.
// This is appropriate for POST endpoints that create resources (e.g.
// ConversationCreate). Body decoding and error handling are identical
// to AuthedHandler.
func AuthedHandlerCreated[Req any, Resp any](
	c *Context,
	fn AuthedJSONFunc[Req, Resp],
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		var req Req
		if err := DecodeJSON(r, &req); err != nil {
			WriteError(w, merrors.ErrInvalidInput)
			return
		}

		deps := &AuthedHandlerDeps{
			Ctx:  c,
			User: user,
		}

		resp, mErr := fn(r, deps, &req)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		WriteSuccess(w, http.StatusCreated, resp)
	}
}

// AuthedJSONNoContent wires a JSON-bodied, no-response business function
// into an http.HandlerFunc. It decodes the request body like AuthedHandler
// but writes a 204 on success (e.g. CreateAgentMemory).
func AuthedJSONNoContent[Req any](
	c *Context,
	fn func(r *http.Request, deps *AuthedHandlerDeps, req *Req) *merrors.Error,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, mErr := c.CurrentUser(r)
		if mErr != nil {
			WriteError(w, mErr)
			return
		}

		var req Req
		if err := DecodeJSON(r, &req); err != nil {
			WriteError(w, merrors.ErrInvalidInput)
			return
		}

		deps := &AuthedHandlerDeps{
			Ctx:  c,
			User: user,
		}

		if mErr := fn(r, deps, &req); mErr != nil {
			WriteError(w, mErr)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

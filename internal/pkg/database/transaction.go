package database

import (
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"

	"github.com/gocraft/dbr/v2"
)

// WithTransaction executes the provided function within a database transaction.
// It handles transaction lifecycle management including begin, commit, and rollback.
// If the function returns an error, the transaction is rolled back automatically.
// The operation parameter is used in error messages for debugging and logging.
//
// Type parameter T allows the function to return a typed result along with any error.
//
// Returns:
//   - The result of the operation function on success.
//   - merrors.ErrInvalidInput if the session is nil.
//   - A wrapped server error if begin or commit fails.
//   - Any error returned by the operation function.
func WithTransaction[T any](sess *dbr.Session, operation string, fn func(tx *dbr.Tx) (T, *merrors.Error)) (T, *merrors.Error) {
	var zero T

	if sess == nil {
		return zero, merrors.ErrInvalidInput
	}

	tx, err := sess.Begin()
	if err != nil {
		return zero, merrors.WrapStdServerError(err, "begin "+operation+" transaction")
	}
	defer tx.RollbackUnlessCommitted()

	result, mErr := fn(tx)
	if mErr != nil {
		return zero, mErr
	}

	if err := tx.Commit(); err != nil {
		return zero, merrors.WrapStdServerError(err, "commit "+operation+" transaction")
	}

	return result, nil
}

// WithTransactionNoResult executes the provided function within a database transaction
// without returning a result. It handles transaction lifecycle management including
// begin, commit, and rollback. If the function returns an error, the transaction
// is rolled back automatically.
// The operation parameter is used in error messages for debugging and logging.
//
// Returns:
//   - merrors.ErrInvalidInput if the session is nil.
//   - A wrapped server error if begin or commit fails.
//   - Any error returned by the operation function.
//   - nil on successful completion.
func WithTransactionNoResult(sess *dbr.Session, operation string, fn func(tx *dbr.Tx) *merrors.Error) *merrors.Error {
	if sess == nil {
		return merrors.ErrInvalidInput
	}

	tx, err := sess.Begin()
	if err != nil {
		return merrors.WrapStdServerError(err, "begin "+operation+" transaction")
	}
	defer tx.RollbackUnlessCommitted()

	if mErr := fn(tx); mErr != nil {
		return mErr
	}

	if err := tx.Commit(); err != nil {
		return merrors.WrapStdServerError(err, "commit "+operation+" transaction")
	}

	return nil
}

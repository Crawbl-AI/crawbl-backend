package database

import (
	merrors "github.com/Crawbl-AI/crawbl-backend/internal/pkg/errors"

	"github.com/gocraft/dbr/v2"
)

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

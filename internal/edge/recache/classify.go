package recache

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/edgecomet/engine/internal/edge/orchestrator"
	"github.com/edgecomet/engine/internal/edge/rsclient"
	"github.com/edgecomet/engine/pkg/types"
)

// noOriginStatus marks a failure that observed no origin response at all (validation
// rejected the request, the origin was unreachable, or no render service answered).
const noOriginStatus = 0

// HTTP status class boundaries used to map an uncacheable origin status onto an error type.
const (
	statusRedirectMin    = 300
	statusClientErrorMin = 400
	statusServerErrorMin = 500
	statusServerErrorMax = 600
)

// recacheError is the classified outcome of a failed recache attempt. Every terminal failure
// return in the recache flow builds one, so the outcome class is decided where the cause is
// known instead of being re-derived from an error string by the handler.
type recacheError struct {
	errorType string
	message   string
	// statusCode is the origin status the attempt observed, noOriginStatus when there is none.
	statusCode int
	// redirectTo is where an uncacheable redirect pointed; empty for every other class.
	redirectTo string
	// permanent marks a failure that a retry cannot resolve.
	permanent bool
	// cause keeps the underlying error reachable for errors.Is/As; message already renders it.
	cause error
}

func (e *recacheError) Error() string {
	return e.message
}

func (e *recacheError) Unwrap() error {
	return e.cause
}

// withCause attaches the underlying error so sentinel checks survive the classification.
func (e *recacheError) withCause(cause error) *recacheError {
	e.cause = cause
	return e
}

// withRedirect records where an uncacheable redirect pointed. It applies to origin_redirect only:
// both call sites know a final URL or Location for responses of any status, and a 404's final URL
// is the page itself, not a redirect target.
func (e *recacheError) withRedirect(location string) *recacheError {
	if e.errorType == types.ErrorTypeOriginRedirect {
		e.redirectTo = location
	}
	return e
}

// warnLevelErrorTypes lists the failure classes logged at warn rather than error. They share one
// property: the edge gateway neither caused them nor can fix them - the origin answered badly or
// was unreachable, the request was invalid, render capacity ran out, or the render service
// returned an unusable response - and they arrive in bursts, since one origin outage carries a
// whole autorecache queue with it. Logging those at error level would drown error tracking, which
// is the pathology commit 7c74761 fixed for configuration declines. Everything else - a dead
// Chrome, a failed cache write - is ours and stays at error level.
var warnLevelErrorTypes = map[string]bool{
	types.ErrorTypeNetworkError:        true,
	types.ErrorTypeOrigin4xx:           true,
	types.ErrorTypeOrigin5xx:           true,
	types.ErrorTypeOriginRedirect:      true,
	types.ErrorTypeOriginUncacheable:   true,
	types.ErrorTypeStatusCaptureFailed: true,
	types.ErrorTypeEmptyResponse:       true,
	types.ErrorTypeRenderUnavailable:   true,
	types.ErrorTypePoolUnavailable:     true,
	types.ErrorTypeInvalidRequest:      true,
	// The render service answers 400 with this for a malformed request or a tab reservation it
	// no longer recognises. It stays retryable - the next attempt reserves a different tab - but
	// a stale-reservation burst must not reach error tracking three times per URL.
	types.ErrorTypeInvalidURL: true,
}

// logAtError reports whether the failure names a fault on the edge gateway's own side, which must
// reach error tracking.
func (e *recacheError) logAtError() bool {
	return !warnLevelErrorTypes[e.errorType]
}

// classifiedFailure extracts the classification a terminal recache failure carries, or nil when
// the error is not a failed attempt: success and configuration declines are neither counted nor
// persisted.
func classifiedFailure(err error) *recacheError {
	if err == nil || errors.Is(err, ErrRecacheSkipped) {
		return nil
	}

	var failure *recacheError
	if errors.As(err, &failure) {
		return failure
	}

	// Every terminal return in the recache flow is classified. An unclassified one would
	// otherwise disappear from both the event ledger and the retry protocol, so name it.
	return retryableFailure(types.ErrorTypeUnknown, noOriginStatus, err.Error()).withCause(err)
}

// permanentFailure classifies a failure a retry cannot resolve.
func permanentFailure(errorType string, statusCode int, message string) *recacheError {
	return &recacheError{errorType: errorType, message: message, statusCode: statusCode, permanent: true}
}

// retryableFailure classifies a failure worth another attempt.
func retryableFailure(errorType string, statusCode int, message string) *recacheError {
	return &recacheError{errorType: errorType, message: message, statusCode: statusCode, permanent: false}
}

// classifyUncacheableStatus maps an origin status the resolved cache config rejects onto the
// outcome taxonomy. Of the origin statuses only 5xx is worth retrying; the rest report a
// stable decision by the origin, and retrying them (403, 404, 429) only adds load.
func classifyUncacheableStatus(statusCode int) *recacheError {
	message := "origin returned uncacheable status " + strconv.Itoa(statusCode)

	switch {
	case statusCode >= statusServerErrorMin && statusCode < statusServerErrorMax:
		return retryableFailure(types.ErrorTypeOrigin5xx, statusCode, message)
	case statusCode >= statusClientErrorMin && statusCode < statusServerErrorMin:
		return permanentFailure(types.ErrorTypeOrigin4xx, statusCode, message)
	case statusCode >= statusRedirectMin && statusCode < statusClientErrorMin:
		return permanentFailure(types.ErrorTypeOriginRedirect, statusCode, message)
	default:
		return permanentFailure(types.ErrorTypeOriginUncacheable, statusCode, message)
	}
}

// classifyStatus applies the live path's cacheable-status rule to an origin status and returns
// nil when the status is cacheable (a configured non-200 is a success, not a failure).
func (rs *RecacheService) classifyStatus(statusCode int, cacheableStatusCodes []int) *recacheError {
	if rs.cacheCoord.IsStatusCodeCacheable(statusCode, cacheableStatusCodes) {
		return nil
	}
	return classifyUncacheableStatus(statusCode)
}

// classifyRenderFailure maps an unusable render service response onto the outcome taxonomy.
// All three cases are transient infrastructure conditions, so they stay retryable regardless
// of the origin-status policy. An error type the render service left empty becomes
// ErrorTypeUnknown: error_type is the success discriminator on the emitted rows, so a failure
// must never carry an empty one, and it must not borrow a diagnosis nobody made.
func classifyRenderFailure(failure *orchestrator.RenderResponseFailure) *recacheError {
	errorType := failure.ErrorType
	if errorType == "" {
		errorType = types.ErrorTypeUnknown
	}
	return retryableFailure(errorType, failure.StatusCode, failure.Message)
}

// classifyRenderCallError maps a failed render service call onto the outcome taxonomy. A non-200
// answer carries the render service's own error type (pool_unavailable, chrome_crash,
// chrome_restart_failed, hard_timeout), which must reach the event row verbatim; everything else
// is a transport failure the edge gateway cannot attribute any further. Both are infrastructure
// conditions and therefore retryable.
func classifyRenderCallError(err error) *recacheError {
	message := fmt.Sprintf("render service call failed: %v", err)

	var svcErr *rsclient.ServiceError
	if errors.As(err, &svcErr) && svcErr.ErrorType != "" {
		return retryableFailure(svcErr.ErrorType, noOriginStatus, message).withCause(err)
	}

	return retryableFailure(types.ErrorTypeRenderUnavailable, noOriginStatus, message).withCause(err)
}

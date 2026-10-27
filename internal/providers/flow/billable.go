package flow

import (
	"github.com/google/uuid"

	"SnakeMedia/internal/models"
)

// billableJob is what openBillable and resumeBillable need to know about one
// kind of billable request (image or video generation).
type billableJob[T any] struct {
	kind string
	// wrongKind refuses a resume of another kind's request ID.
	wrongKind string
	// pending are the placeholders shown while another call holds the job.
	pending func(job generationJob) []T
	// result is a finished job's answer from its journal.
	result func(job generationJob) []T
	// resume reconciles an unfinished job without sending it again.
	resume func(job generationJob) models.Response[[]T]
}

// openBillable is the shared entry of a billable request. A second call with
// the same request ID gets the pending placeholders instead of running beside
// the first; a request already in the journal is answered from it or resumed,
// never sent again. With proceed the caller sends the request and must call
// unlock once done.
func openBillable[T any](s *Provider, b billableJob[T], id, fingerprint string) (unlock func(), res models.Response[[]T], proceed bool) {
	unlock, ok := s.tryJobLock(id)
	if !ok {
		return nil, models.SuccessResponse(b.pending(newGenerationJob(id, b.kind, fingerprint, ""))), false
	}
	job, exists, err := s.checkGenerationJob(id, b.kind, fingerprint)
	if err != nil {
		unlock()
		return nil, models.ErrorResponse[[]T](err.Error(), models.CodeInvalidArgument), false
	}
	if !exists {
		return unlock, res, true
	}
	defer unlock()
	return nil, settledOrResumed(b, job), false
}

// resumeBillable reconciles a saved request by its ID without resubmitting it.
func resumeBillable[T any](s *Provider, b billableJob[T], id string, busy []T) models.Response[[]T] {
	if _, err := uuid.Parse(id); err != nil {
		return models.ErrorResponse[[]T]("Mã yêu cầu không hợp lệ.", models.CodeInvalidArgument)
	}
	unlock, ok := s.tryJobLock(id)
	if !ok {
		return models.SuccessResponse(busy)
	}
	defer unlock()
	job, err := s.loadGenerationJob(id)
	if err != nil {
		return models.ErrorResponse[[]T](err.Error(), models.CodeNotFound)
	}
	if job.Kind != b.kind {
		return models.ErrorResponse[[]T](b.wrongKind, models.CodeInvalidArgument)
	}
	return settledOrResumed(b, job)
}

func settledOrResumed[T any](b billableJob[T], job generationJob) models.Response[[]T] {
	switch job.Phase {
	case generationComplete:
		return models.SuccessResponse(b.result(job))
	case generationRejected:
		return models.ErrorResponse[[]T](job.Message, models.CodeUnavailable)
	default:
		return b.resume(job)
	}
}

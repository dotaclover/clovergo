package service

import (
	"context"
	"sync"
	"time"

	"clovergo/internal/domain"
	"clovergo/internal/platforms"
	"clovergo/internal/prompts"
)

const (
	// extractionWorkers bounds how many materials are extracted at once. Two
	// keeps the Ark request rate predictable while still overlapping the
	// network wait of one material with the other.
	extractionWorkers = 2
	// extractionQueueDepth absorbs a burst of imports without blocking the
	// HTTP handler that produced them.
	extractionQueueDepth = 256
)

// extractionQueue runs knowledge extraction in the background, so an import
// request returns as soon as the material is summarized and stored rather than
// waiting for the extraction model call.
//
// A document is queued at most once at a time. Extraction runs on the whole
// material in one call, so a retry starts the material over rather than
// resuming a finished piece of it.
type extractionQueue struct {
	service *Service
	work    chan int64
	mu      sync.Mutex
	pending map[int64]struct{}
	wg      sync.WaitGroup
	stop    chan struct{}
	once    sync.Once
}

func newExtractionQueue(service *Service) *extractionQueue {
	return &extractionQueue{
		service: service,
		work:    make(chan int64, extractionQueueDepth),
		pending: make(map[int64]struct{}),
		stop:    make(chan struct{}),
	}
}

// Enqueue schedules one document for extraction, ignoring ids already waiting.
func (q *extractionQueue) Enqueue(documentID int64) {
	if documentID <= 0 {
		return
	}
	q.mu.Lock()
	if _, exists := q.pending[documentID]; exists {
		q.mu.Unlock()
		return
	}
	q.pending[documentID] = struct{}{}
	q.mu.Unlock()
	select {
	case q.work <- documentID:
	case <-q.stop:
		q.forget(documentID)
	}
}

func (q *extractionQueue) forget(documentID int64) {
	q.mu.Lock()
	delete(q.pending, documentID)
	q.mu.Unlock()
}

// Depth reports how many documents are waiting, for the health endpoint.
func (q *extractionQueue) Depth() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.pending)
}

// Start launches the workers. Nothing is resumed on startup: extraction is an
// explicit owner action now, so an archived material that a previous process
// left unextracted simply stays archived until the owner asks for it.
func (q *extractionQueue) Start(ctx context.Context) error {
	for worker := 0; worker < extractionWorkers; worker++ {
		q.wg.Add(1)
		go func() {
			defer q.wg.Done()
			for {
				select {
				case <-q.stop:
					return
				case documentID := <-q.work:
					q.service.processQueued(documentID)
					q.forget(documentID)
				}
			}
		}()
	}
	return nil
}

// Stop asks the workers to finish the current document and exit.
func (q *extractionQueue) Stop() {
	q.once.Do(func() { close(q.stop) })
	q.wg.Wait()
}

const (
	// draftWorkers lets a second article start while the first waits on the
	// model, which is the whole point of moving generation off the request.
	draftWorkers = 2
	// draftQueueDepth absorbs a burst of submissions. The queue is in memory:
	// a draft still running at shutdown is marked failed on the next start.
	draftQueueDepth = 64
)

// draftJob carries the already validated request: the sources were resolved by
// the HTTP handler, so the worker only writes the article and stores it.
type draftJob struct {
	draftID    int64
	runID      string
	runStarted time.Time
	input      GenerateDraftInput
	platform   platforms.Platform
	system     prompts.Composed
	knowledge  []domain.KnowledgeItem
	documents  []domain.SourceDocument
}

// draftQueue writes articles in the background. The HTTP request returns as
// soon as the draft row exists, so the screen never waits on a model call.
// ctx is cancelled by Stop, so a model call in flight at shutdown is abandoned
// instead of holding the process open.
type draftQueue struct {
	service *Service
	work    chan draftJob
	wg      sync.WaitGroup
	stop    chan struct{}
	once    sync.Once
	ctx     context.Context
	cancel  context.CancelFunc
}

func newDraftQueue(service *Service) *draftQueue {
	ctx, cancel := context.WithCancel(context.Background())
	return &draftQueue{
		service: service,
		work:    make(chan draftJob, draftQueueDepth),
		stop:    make(chan struct{}),
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Enqueue schedules one draft and reports whether the queue accepted it. A
// full queue refuses at once instead of holding the HTTP request open.
func (q *draftQueue) Enqueue(job draftJob) bool {
	select {
	case q.work <- job:
		return true
	case <-q.stop:
		return false
	default:
		return false
	}
}

// Depth reports how many drafts are waiting to be written.
func (q *draftQueue) Depth() int { return len(q.work) }

func (q *draftQueue) Start(ctx context.Context) error {
	for worker := 0; worker < draftWorkers; worker++ {
		q.wg.Add(1)
		go func() {
			defer q.wg.Done()
			for {
				select {
				case <-q.stop:
					return
				case job := <-q.work:
					q.service.processDraftJob(q.ctx, job)
				}
			}
		}()
	}
	return nil
}

func (q *draftQueue) Stop() {
	q.once.Do(func() {
		close(q.stop)
		q.cancel()
	})
	q.wg.Wait()
}

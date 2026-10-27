package flow

import (
	"context"
	"sync"

	"SnakeMedia/internal/logger"
	"SnakeMedia/internal/providers/flow/session"
)

// accountChoice is one account's answer to "can you run this request?": the
// choice when it can, or why not. catalogErr means its catalogue could not be
// read; selectErr means the catalogue has no fitting model or variant.
type accountChoice[C any] struct {
	choice     C
	ok         bool
	catalogErr error
	selectErr  error
}

// accountChoices asks every account in parallel (each reads its own cached
// catalogue) and keeps the choices in account order, with the first catalogue
// and selection errors to explain an empty result.
func accountChoices[C any](
	ctx context.Context,
	accounts []session.StoredAccount,
	pick func(ctx context.Context, acc session.StoredAccount) accountChoice[C],
) (choices []C, firstCatalogErr, firstSelectErr error) {
	answers := make([]accountChoice[C], len(accounts))
	var wg sync.WaitGroup
	for i, acc := range accounts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer logger.Recover("chọn tài khoản Flow")
			answers[i] = pick(ctx, acc)
		}()
	}
	wg.Wait()
	for _, a := range answers {
		switch {
		case a.ok:
			choices = append(choices, a.choice)
		case a.catalogErr != nil && firstCatalogErr == nil:
			firstCatalogErr = a.catalogErr
		case a.selectErr != nil && firstSelectErr == nil:
			firstSelectErr = a.selectErr
		}
	}
	return choices, firstCatalogErr, firstSelectErr
}

// flightGroup lets concurrent callers asking for the same key share one call
// (an account's catalogue is read once even when many requests need it).
type flightGroup struct {
	mu    sync.Mutex
	calls map[string]*flightCall
}

type flightCall struct {
	done chan struct{}
	val  any
	err  error
}

func (g *flightGroup) do(key string, fn func() (any, error)) (any, error) {
	g.mu.Lock()
	if g.calls == nil {
		g.calls = map[string]*flightCall{}
	}
	if c, ok := g.calls[key]; ok {
		g.mu.Unlock()
		<-c.done
		return c.val, c.err
	}
	c := &flightCall{done: make(chan struct{})}
	g.calls[key] = c
	g.mu.Unlock()
	c.val, c.err = fn()
	g.mu.Lock()
	delete(g.calls, key)
	g.mu.Unlock()
	close(c.done)
	return c.val, c.err
}

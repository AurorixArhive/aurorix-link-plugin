package guard

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrGate = errors.New("被本地闸门拦下")

type Limiter struct {
	mu     sync.Mutex
	rate   float64
	burst  float64
	tokens float64
	last   time.Time
}

func NewLimiter(rate float64, burst int) *Limiter {
	return &Limiter{rate: rate, burst: float64(burst), tokens: float64(burst)}
}

func (l *Limiter) take(now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.last.IsZero() {
		l.tokens = min(l.burst, l.tokens+now.Sub(l.last).Seconds()*l.rate)
	}
	l.last = now
	if l.tokens >= 1 {
		l.tokens--
		return 0
	}
	return time.Duration((1 - l.tokens) / l.rate * float64(time.Second))
}

func (l *Limiter) wait(budget time.Duration) bool {
	deadline := time.Now().Add(budget)
	for {
		wait := l.take(time.Now())
		if wait == 0 {
			return true
		}
		if time.Now().Add(wait).After(deadline) {
			return false
		}
		time.Sleep(wait)
	}
}

type breakerState int

const (
	closed breakerState = iota
	open
	halfOpen
)

type Breaker struct {
	mu        sync.Mutex
	threshold int
	cooldown  time.Duration
	failures  int
	state     breakerState
	until     time.Time
	probing   bool
}

func NewBreaker(threshold int, cooldown time.Duration) *Breaker {
	return &Breaker{threshold: threshold, cooldown: cooldown}
}

func (b *Breaker) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch b.state {
	case closed:
		return true
	case open:
		if time.Now().Before(b.until) {
			return false
		}
		b.state = halfOpen
	}
	if b.probing {
		return false
	}
	b.probing = true
	return true
}

func (b *Breaker) done(ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	probing := b.state == halfOpen
	b.probing = false
	if ok {
		b.failures = 0
		b.state = closed
		return
	}
	if probing {
		b.state = open
		b.until = time.Now().Add(b.cooldown)
		return
	}
	b.failures++
	if b.failures >= b.threshold {
		b.failures = 0
		b.state = open
		b.until = time.Now().Add(b.cooldown)
	}
}

type Options struct {
	Name       string
	RPS        float64
	Burst      int
	WaitBudget time.Duration
	Failures   int
	Cooldown   time.Duration
}

type Gate struct {
	name    string
	limiter *Limiter
	breaker *Breaker
	budget  time.Duration
}

func NewGate(opt Options) *Gate {
	return &Gate{
		name:    opt.Name,
		limiter: NewLimiter(opt.RPS, opt.Burst),
		breaker: NewBreaker(opt.Failures, opt.Cooldown),
		budget:  opt.WaitBudget,
	}
}

func (g *Gate) Enter() error {
	if !g.breaker.allow() {
		return fmt.Errorf("%s 暂时不可用: %w", g.name, ErrGate)
	}
	if !g.limiter.wait(g.budget) {
		return fmt.Errorf("%s 请求太密集: %w", g.name, ErrGate)
	}
	return nil
}

func (g *Gate) Done(ok bool) { g.breaker.done(ok) }

type Flight[T any] struct {
	mu sync.Mutex
	m  map[string]*call[T]
}

type call[T any] struct {
	wg  sync.WaitGroup
	val T
	err error
}

func NewFlight[T any]() *Flight[T] { return &Flight[T]{m: make(map[string]*call[T])} }

func (f *Flight[T]) Do(key string, fn func() (T, error)) (T, error) {
	f.mu.Lock()
	if existing, ok := f.m[key]; ok {
		f.mu.Unlock()
		existing.wg.Wait()
		return existing.val, existing.err
	}
	c := &call[T]{}
	c.wg.Add(1)
	f.m[key] = c
	f.mu.Unlock()

	c.val, c.err = fn()

	f.mu.Lock()
	delete(f.m, key)
	f.mu.Unlock()
	c.wg.Done()
	return c.val, c.err
}

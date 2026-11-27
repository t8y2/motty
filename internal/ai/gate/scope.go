package gate

import (
	"sync/atomic"

	"motty/internal/ai/risk"
)

// Scope is the highest risk a running request executes without asking, shared
// between the window that sets it and the request's goroutine that reads it at
// each decision. The zero value is risk.Low.
type Scope struct{ level atomic.Uint32 }

// NewScope returns a scope that starts at level.
func NewScope(level risk.Level) *Scope {
	scope := new(Scope)
	scope.Set(level)
	return scope
}

// Set changes the scope for the decisions that follow.
func (s *Scope) Set(level risk.Level) { s.level.Store(uint32(level)) }

// Level is the scope at this moment.
func (s *Scope) Level() risk.Level { return risk.Level(s.level.Load()) }

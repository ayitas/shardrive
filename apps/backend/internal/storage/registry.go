package storage

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

func NewRegistry() *Registry {
	return &Registry{providers: make(map[string]Provider)}
}

func (r *Registry) Register(name string, provider Provider) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("register provider: %w: empty name", ErrInvalidRequest)
	}
	if provider == nil {
		return fmt.Errorf("register provider %q: %w: nil provider", name, ErrInvalidRequest)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providers[name]; exists {
		return fmt.Errorf("register provider %q: %w", name, ErrProviderExists)
	}
	r.providers[name] = provider
	return nil
}

func (r *Registry) Get(name string) (Provider, error) {
	r.mu.RLock()
	provider, exists := r.providers[name]
	r.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("get provider %q: %w", name, ErrProviderNotFound)
	}
	return provider, nil
}

func (r *Registry) Names() []string {
	r.mu.RLock()
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	r.mu.RUnlock()
	sort.Strings(names)
	return names
}

func IsRetryable(err error) bool {
	return errors.Is(err, ErrUnavailable)
}

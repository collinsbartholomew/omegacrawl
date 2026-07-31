package jsengine

import (
	"context"
	"sync"

	"go.uber.org/zap"

	"github.com/user/clone/internal/util"
)

// ServiceWorkerManager tracks and manages service worker registrations in a browser context.
type ServiceWorkerManager struct {
	initialState *ServiceWorkerInfo
	mu           sync.RWMutex
}

// NewServiceWorkerManager returns an empty ServiceWorkerManager.
func NewServiceWorkerManager() *ServiceWorkerManager {
	return &ServiceWorkerManager{}
}

// Detect finds service worker registrations and records them as the manager's initial state.
func (m *ServiceWorkerManager) Detect(ctx context.Context) (*ServiceWorkerInfo, error) {
	info, err := DetectServiceWorkers(ctx)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.initialState = info
	m.mu.Unlock()

	if info.Count > 0 {
		util.LogDebug("service workers detected",
			zap.Int("count", info.Count),
		)
		for _, reg := range info.Registrations {
			util.LogDebug("service worker registration",
				zap.String("scope", reg.Scope),
				zap.Stringp("active", reg.Active),
			)
		}
	}
	return info, nil
}

// Unregister removes all service worker registrations and returns how many were removed.
func (m *ServiceWorkerManager) Unregister(ctx context.Context) (int, error) {
	count, err := UnregisterServiceWorkers(ctx)
	if err != nil {
		return 0, err
	}
	if count > 0 {
		util.LogDebug("unregistered service workers", zap.Int("count", count))
	}
	return count, nil
}

// GetInitial returns the service worker state captured at detect time.
func (m *ServiceWorkerManager) GetInitial() *ServiceWorkerInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.initialState
}

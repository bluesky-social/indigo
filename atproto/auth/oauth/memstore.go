package oauth

import (
	"context"
	"fmt"
	"sync"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// Simple in-memory implementation of [ClientAuthStore], for use in development and demos.
//
// This is not appropriate even casual real-world use: all users will be logged-out every time the process is restarted.
type MemStore struct {
	requests map[string]AuthRequestData
	sessions map[string]ClientSessionData

	lk sync.Mutex
}

var _ ClientAuthStore = &MemStore{}

func NewMemStore() *MemStore {
	return &MemStore{
		requests: make(map[string]AuthRequestData),
		sessions: make(map[string]ClientSessionData),
	}
}

func memKey(did syntax.DID, sessionID string) string {
	return fmt.Sprintf("%s/%s", did, sessionID)
}

func (m *MemStore) GetSession(ctx context.Context, did syntax.DID, sessionID string) (*ClientSessionData, error) {
	m.lk.Lock()
	defer m.lk.Unlock()

	sess, ok := m.sessions[memKey(did, sessionID)]
	if !ok {
		return nil, fmt.Errorf("session not found: %s", did)
	}
	return &sess, nil
}

func (m *MemStore) SaveSession(ctx context.Context, sess ClientSessionData) error {
	m.lk.Lock()
	defer m.lk.Unlock()

	m.sessions[memKey(sess.AccountDID, sess.SessionID)] = sess
	return nil
}

func (m *MemStore) DeleteSession(ctx context.Context, did syntax.DID, sessionID string) error {
	m.lk.Lock()
	defer m.lk.Unlock()

	delete(m.sessions, memKey(did, sessionID))
	return nil
}

func (m *MemStore) GetAuthRequestInfo(ctx context.Context, state string) (*AuthRequestData, error) {
	m.lk.Lock()
	defer m.lk.Unlock()

	req, ok := m.requests[state]
	if !ok {
		return nil, fmt.Errorf("request info not found: %s", state)
	}
	// TODO: delete? should only ever fetch once
	return &req, nil
}

func (m *MemStore) SaveAuthRequestInfo(ctx context.Context, info AuthRequestData) error {
	m.lk.Lock()
	defer m.lk.Unlock()

	m.requests[info.State] = info
	return nil
}

func (m *MemStore) DeleteAuthRequestInfo(ctx context.Context, state string) error {
	m.lk.Lock()
	defer m.lk.Unlock()

	delete(m.requests, state)
	return nil
}

package main

import (
	"fmt"
	"sync"

	"github.com/bluesky-social/indigo/atproto/auth"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// simple in-process database for sessions and auth requests
type AuthMemStore struct {
	requests map[string]auth.OAuthRequestInfo
	sessions map[string]auth.OAuthSession

	lk sync.Mutex
}

func NewAuthMemStore() AuthMemStore {
	return AuthMemStore{
		requests: make(map[string]auth.OAuthRequestInfo),
		sessions: make(map[string]auth.OAuthSession),
	}
}

func (m *AuthMemStore) GetSession(did syntax.DID) (*auth.OAuthSession, error) {
	m.lk.Lock()
	defer m.lk.Unlock()

	sess, ok := m.sessions[did.String()]
	if !ok {
		return nil, fmt.Errorf("session not found: %s", did)
	}
	return &sess, nil
}

func (m *AuthMemStore) GetAuthRequestInfo(state string) (*auth.OAuthRequestInfo, error) {
	m.lk.Lock()
	defer m.lk.Unlock()

	req, ok := m.requests[state]
	if !ok {
		return nil, fmt.Errorf("request info not found: %s", state)
	}
	// TODO: delete? should only ever fetch once
	return &req, nil
}

func (m *AuthMemStore) SaveSession(sess auth.OAuthSession) {
	m.lk.Lock()
	defer m.lk.Unlock()

	m.sessions[sess.AccountDID.String()] = sess
}

func (m *AuthMemStore) SaveAuthRequestInfo(info auth.OAuthRequestInfo) {
	m.lk.Lock()
	defer m.lk.Unlock()

	m.requests[info.State] = info
}

package repositories

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"chainora-api/core/constants"
	"chainora-api/core/entities"
)

// InMemoryAuthRepository stores auth sessions in-process using sync.Map.
type InMemoryAuthRepository struct {
	sessions sync.Map
	users    sync.Map
}

func NewInMemoryAuthRepository() *InMemoryAuthRepository {
	return &InMemoryAuthRepository{}
}

func (r *InMemoryAuthRepository) SaveSession(session entities.AuthSession) error {
	r.sessions.Store(session.ID, session)
	return nil
}

func (r *InMemoryAuthRepository) GetSession(sessionID string) (entities.AuthSession, error) {
	value, ok := r.sessions.Load(sessionID)
	if !ok {
		return entities.AuthSession{}, fmt.Errorf("session not found: %s", sessionID)
	}

	session, ok := value.(entities.AuthSession)
	if !ok {
		return entities.AuthSession{}, fmt.Errorf("invalid session payload: %s", sessionID)
	}
	return session, nil
}

func (r *InMemoryAuthRepository) DeleteSession(sessionID string) error {
	r.sessions.Delete(sessionID)
	return nil
}

func (r *InMemoryAuthRepository) UpdateUser(user entities.User) error {
	address := strings.ToLower(strings.TrimSpace(user.Address))
	if address == "" {
		return fmt.Errorf("update user: %w", constants.ErrUserNotFound)
	}

	if existing, ok := r.users.Load(address); ok {
		oldUser, castOk := existing.(entities.User)
		if !castOk {
			return errors.New("invalid user payload")
		}
		if strings.TrimSpace(user.PublicKey) == "" {
			user.PublicKey = oldUser.PublicKey
		}
	}

	r.users.Store(address, user)
	return nil
}

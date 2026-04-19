package repositories

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"chainora-api/adapter/models"
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

func (r *InMemoryAuthRepository) GetUser(address string) (entities.User, error) {
	key := strings.ToLower(strings.TrimSpace(address))
	if key == "" {
		return entities.User{}, fmt.Errorf("get user: %w", constants.ErrUserNotFound)
	}

	value, ok := r.users.Load(key)
	if !ok {
		return entities.User{}, fmt.Errorf("get user: %w", constants.ErrUserNotFound)
	}

	model, castOk := value.(models.UserModel)
	if !castOk {
		return entities.User{}, errors.New("invalid user payload")
	}

	return model.ToEntity(), nil
}

func (r *InMemoryAuthRepository) GetUserByUsername(username string) (entities.User, error) {
	normalized := strings.ToLower(strings.TrimSpace(username))
	normalized = strings.TrimPrefix(normalized, "@")
	normalized = strings.TrimSpace(strings.TrimSuffix(normalized, ".init"))
	if normalized == "" {
		return entities.User{}, fmt.Errorf("get user by username: %w", constants.ErrUserNotFound)
	}

	var matched entities.User
	found := false
	r.users.Range(func(_, value any) bool {
		model, ok := value.(models.UserModel)
		if !ok {
			return true
		}

		candidateUsername := strings.ToLower(strings.TrimSpace(model.Username))
		candidateUsername = strings.TrimSpace(strings.TrimSuffix(candidateUsername, ".init"))
		if candidateUsername != normalized {
			return true
		}

		matched = model.ToEntity()
		found = true
		return false
	})

	if !found {
		return entities.User{}, fmt.Errorf("get user by username: %w", constants.ErrUserNotFound)
	}

	return matched, nil
}

func (r *InMemoryAuthRepository) UsernameExists(username string) (bool, error) {
	trimmed := strings.TrimSpace(username)
	if trimmed == "" {
		return false, nil
	}

	lowerUsername := strings.ToLower(trimmed)
	exists := false
	r.users.Range(func(_, value any) bool {
		model, ok := value.(models.UserModel)
		if !ok {
			return true
		}

		if strings.ToLower(strings.TrimSpace(model.Username)) == lowerUsername {
			exists = true
			return false
		}

		return true
	})

	return exists, nil
}

func (r *InMemoryAuthRepository) UpsertUser(user entities.User) error {
	address := strings.ToLower(strings.TrimSpace(user.Address))
	if address == "" {
		return fmt.Errorf("upsert user: %w", constants.ErrUserNotFound)
	}

	if existing, ok := r.users.Load(address); ok {
		oldUser, castOk := existing.(models.UserModel)
		if !castOk {
			return errors.New("invalid user payload")
		}

		if strings.TrimSpace(user.Username) == "" {
			user.Username = oldUser.Username
		}
		if strings.TrimSpace(user.AvatarURL) == "" {
			user.AvatarURL = oldUser.AvatarURL
		}
		if strings.TrimSpace(user.TCNR) == "" {
			user.TCNR = oldUser.TCNR
		}
		if user.UsernameCount == 0 {
			user.UsernameCount = oldUser.UsernameCount
		}
		if !user.PrimarySelectionSponsoredUsed {
			user.PrimarySelectionSponsoredUsed = oldUser.PrimarySelectionSponsoredUsed
		}
		if strings.TrimSpace(user.KYCStatus) == "" {
			user.KYCStatus = oldUser.KYCStatus
		}
		if strings.TrimSpace(user.PublicKey) == "" {
			user.PublicKey = oldUser.PublicKey
		}
	}

	user.Username = strings.TrimSpace(user.Username)
	if strings.TrimSpace(user.TCNR) == "" {
		user.TCNR = "0"
	}
	if strings.TrimSpace(user.KYCStatus) == "" {
		user.KYCStatus = "unavailable"
	}
	if user.UsernameCount < 0 {
		user.UsernameCount = 0
	}

	r.users.Store(address, models.UserModelFromEntity(user))
	return nil
}

func (r *InMemoryAuthRepository) UpdateUser(user entities.User) error {
	return r.UpsertUser(user)
}

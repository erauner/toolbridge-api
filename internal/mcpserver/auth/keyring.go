package auth

import (
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/zalando/go-keyring"
)

const (
	keyringService = "com.erauner.toolbridge.mcpbridge"
)

// StoreRefreshToken stores a refresh token securely in the OS keychain
// Gracefully falls back to in-memory if keyring is unavailable
func StoreRefreshToken(domain, clientID, refreshToken string) error {
	account := fmt.Sprintf("%s:%s", domain, clientID)

	if err := keyring.Set(keyringService, account, refreshToken); err != nil {
		log.Debug().
			Err(err).
			Str("account", account).
			Msg("keyring not available, token will be stored in-memory only")
		return err
	}

	log.Debug().
		Str("account", account).
		Msg("refresh token stored in keyring")

	return nil
}

// GetRefreshToken retrieves a stored refresh token from the OS keychain
// Returns empty string if not found (not an error)
func GetRefreshToken(domain, clientID string) (string, error) {
	account := fmt.Sprintf("%s:%s", domain, clientID)

	token, err := keyring.Get(keyringService, account)
	if err == keyring.ErrNotFound {
		log.Debug().
			Str("account", account).
			Msg("no refresh token found in keyring")
		return "", nil // Not an error, just no token stored
	}
	if err != nil {
		log.Debug().
			Err(err).
			Str("account", account).
			Msg("failed to get refresh token from keyring")
		return "", err
	}

	log.Debug().
		Str("account", account).
		Msg("refresh token retrieved from keyring")

	return token, nil
}

// DeleteRefreshToken removes a stored refresh token from the OS keychain
func DeleteRefreshToken(domain, clientID string) error {
	account := fmt.Sprintf("%s:%s", domain, clientID)

	if err := keyring.Delete(keyringService, account); err != nil && err != keyring.ErrNotFound {
		log.Debug().
			Err(err).
			Str("account", account).
			Msg("failed to delete refresh token from keyring")
		return err
	}

	log.Debug().
		Str("account", account).
		Msg("refresh token deleted from keyring")

	return nil
}

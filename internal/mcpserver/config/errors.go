package config

import "errors"

var (
	// ErrMissingAPIBaseURL indicates that the API base URL is not configured
	ErrMissingAPIBaseURL = errors.New("apiBaseUrl is required in configuration")

	// ErrMissingAuth0Domain indicates that the Auth0 domain is not configured
	ErrMissingAuth0Domain = errors.New("auth0.domain is required when not in dev mode")

	// ErrMissingAuth0Clients indicates that no Auth0 clients are configured
	ErrMissingAuth0Clients = errors.New("auth0.clients is required and must have at least one client")

	// ErrNoValidAuth0Client indicates that no Auth0 client has a valid clientId
	ErrNoValidAuth0Client = errors.New("at least one auth0 client must have a clientId")

	// ErrMissingSyncAPI indicates that the syncApi configuration is missing
	ErrMissingSyncAPI = errors.New("auth0.syncApi is required when not in dev mode")

	// ErrMissingIntrospectionClientID indicates that introspection client ID is missing
	ErrMissingIntrospectionClientID = errors.New("auth0.introspection.clientId is required when introspection is configured")

	// ErrMissingIntrospectionClientSecret indicates that introspection client secret is missing
	ErrMissingIntrospectionClientSecret = errors.New("auth0.introspection.clientSecret is required when introspection is configured")

	// ErrConfigFileNotFound indicates that the config file was not found
	ErrConfigFileNotFound = errors.New("configuration file not found")

	// ErrInvalidConfigFormat indicates that the config file has invalid JSON
	ErrInvalidConfigFormat = errors.New("invalid configuration file format")
)

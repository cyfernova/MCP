package auth

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims represents delegated user claims expected by MCP.
type Claims struct {
	jwt.RegisteredClaims
	ClientID string   `json:"client_id,omitempty"`
	Scope    string   `json:"scope,omitempty"`
	Scopes   []string `json:"scopes,omitempty"`
	Role     string   `json:"role,omitempty"`
	Roles    []string `json:"roles,omitempty"`
}

// Principal is the validated delegated identity.
type Principal struct {
	UserID string
	Scopes []string
	Roles  []string
	Claims *Claims
}

// VerifyConfig controls JWT verification behavior.
type VerifyConfig struct {
	Issuer             string
	Audience           string
	AllowedSigningAlgs []string
	ClockSkew          time.Duration

	JWKSURL              string
	JWKSRefreshInterval  time.Duration
	JWKSHTTPTimeout      time.Duration
	UnknownKIDMinRefresh time.Duration
}

// Verifier validates delegated user JWTs against remote JWKS.
type Verifier struct {
	cache       *JWKSCache
	issuer      string
	audience    string
	allowedAlgs []string
	clockSkew   time.Duration
	log         *slog.Logger
}

func NewVerifier(ctx context.Context, cfg VerifyConfig, log *slog.Logger) (*Verifier, error) {
	cache, err := NewJWKSCache(
		ctx,
		cfg.JWKSURL,
		cfg.JWKSRefreshInterval,
		cfg.UnknownKIDMinRefresh,
		cfg.JWKSHTTPTimeout,
		log,
	)
	if err != nil {
		return nil, err
	}

	return &Verifier{
		cache:       cache,
		issuer:      cfg.Issuer,
		audience:    cfg.Audience,
		allowedAlgs: cfg.AllowedSigningAlgs,
		clockSkew:   cfg.ClockSkew,
		log:         log,
	}, nil
}

func (v *Verifier) Verify(ctx context.Context, tokenString string) (*Principal, error) {
	claims := &Claims{}

	parserOpts := []jwt.ParserOption{
		jwt.WithIssuer(v.issuer),
		jwt.WithValidMethods(v.allowedAlgs),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(v.clockSkew),
	}
	parser := jwt.NewParser(parserOpts...)

	token, err := parser.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		alg, _ := token.Header["alg"].(string)
		if !contains(v.allowedAlgs, alg) {
			return nil, fmt.Errorf("jwt signing algorithm %q is not allowed", alg)
		}

		kid, _ := token.Header["kid"].(string)
		return v.cache.GetKey(ctx, kid)
	})
	if err != nil {
		return nil, fmt.Errorf("verify delegated token: %w", err)
	}
	if !token.Valid {
		return nil, fmt.Errorf("verify delegated token: token invalid")
	}
	if claims.Subject == "" {
		return nil, fmt.Errorf("verify delegated token: missing sub claim")
	}
	if strings.TrimSpace(v.audience) != "" {
		hasAudience := false
		for _, aud := range claims.Audience {
			if strings.TrimSpace(aud) == strings.TrimSpace(v.audience) {
				hasAudience = true
				break
			}
		}
		if !hasAudience && strings.TrimSpace(claims.ClientID) != strings.TrimSpace(v.audience) {
			return nil, fmt.Errorf("verify delegated token: audience/client_id mismatch")
		}
	}

	// nbf is validated by jwt parser when present; this guard keeps failures explicit.
	if claims.NotBefore != nil && time.Now().Add(v.clockSkew).Before(claims.NotBefore.Time) {
		return nil, fmt.Errorf("verify delegated token: token not yet valid")
	}

	principal := &Principal{
		UserID: claims.Subject,
		Scopes: normalizeScopes(claims),
		Roles:  normalizeRoles(claims),
		Claims: claims,
	}

	return principal, nil
}

func normalizeScopes(claims *Claims) []string {
	seen := make(map[string]struct{})
	var out []string

	for _, scope := range strings.Fields(claims.Scope) {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		out = append(out, scope)
	}

	for _, scope := range claims.Scopes {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		out = append(out, scope)
	}

	return out
}

func normalizeRoles(claims *Claims) []string {
	seen := make(map[string]struct{})
	var out []string

	if role := strings.TrimSpace(claims.Role); role != "" {
		seen[role] = struct{}{}
		out = append(out, role)
	}

	for _, role := range claims.Roles {
		role = strings.TrimSpace(role)
		if role == "" {
			continue
		}
		if _, ok := seen[role]; ok {
			continue
		}
		seen[role] = struct{}{}
		out = append(out, role)
	}

	return out
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

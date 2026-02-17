package auth

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"sync"
	"time"
)

const (
	maxJWKSBodyBytes = int64(1 << 20) // 1 MiB
	minRSABits       = 2048
)

type jwksDocument struct {
	Keys []jwkKey `json:"keys"`
}

type jwkKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use,omitempty"`
	Alg string `json:"alg,omitempty"`

	// RSA parameters
	N string `json:"n,omitempty"`
	E string `json:"e,omitempty"`

	// EC parameters
	Crv string `json:"crv,omitempty"`
	X   string `json:"x,omitempty"`
	Y   string `json:"y,omitempty"`
}

type JWKSCache struct {
	mu sync.RWMutex

	keys map[string]any

	jwksURL string
	ttl     time.Duration

	lastFetch          time.Time
	lastKidMissRefresh time.Time

	kidMissMinRefresh time.Duration
	httpClient        *http.Client
	log               *slog.Logger
}

func NewJWKSCache(
	ctx context.Context,
	jwksURL string,
	ttl time.Duration,
	kidMissMinRefresh time.Duration,
	httpTimeout time.Duration,
	log *slog.Logger,
) (*JWKSCache, error) {
	cache := &JWKSCache{
		keys:              make(map[string]any),
		jwksURL:           jwksURL,
		ttl:               ttl,
		kidMissMinRefresh: kidMissMinRefresh,
		httpClient: &http.Client{
			Timeout: httpTimeout,
		},
		log: log,
	}

	if err := cache.refresh(ctx); err != nil {
		return nil, fmt.Errorf("initial JWKS refresh failed: %w", err)
	}

	go cache.backgroundRefresh(ctx)
	return cache, nil
}

func (c *JWKSCache) backgroundRefresh(ctx context.Context) {
	ticker := time.NewTicker(c.ttl)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			refreshCtx, cancel := context.WithTimeout(ctx, c.httpClient.Timeout)
			err := c.refresh(refreshCtx)
			cancel()
			if err != nil {
				c.log.Warn("jwks periodic refresh failed", "error", err)
			}
		}
	}
}

func (c *JWKSCache) GetKey(ctx context.Context, kid string) (any, error) {
	if kid == "" {
		return nil, fmt.Errorf("missing kid in JWT header")
	}

	c.mu.RLock()
	key, ok := c.keys[kid]
	isExpired := c.lastFetch.IsZero() || time.Since(c.lastFetch) > c.ttl
	c.mu.RUnlock()

	if ok && !isExpired {
		return key, nil
	}

	if isExpired {
		if err := c.refresh(ctx); err != nil {
			c.log.Warn("jwks refresh on cache expiry failed", "error", err)
		}
	}

	c.mu.RLock()
	key, ok = c.keys[kid]
	c.mu.RUnlock()
	if ok {
		return key, nil
	}

	shouldRefresh := false
	c.mu.RLock()
	if c.lastKidMissRefresh.IsZero() || time.Since(c.lastKidMissRefresh) >= c.kidMissMinRefresh {
		shouldRefresh = true
	}
	c.mu.RUnlock()

	if shouldRefresh {
		c.mu.Lock()
		c.lastKidMissRefresh = time.Now()
		c.mu.Unlock()

		if err := c.refresh(ctx); err != nil {
			return nil, fmt.Errorf("refresh JWKS after kid miss: %w", err)
		}
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	key, ok = c.keys[kid]
	if !ok {
		return nil, fmt.Errorf("key %q not found in JWKS", kid)
	}
	return key, nil
}

func (c *JWKSCache) refresh(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.jwksURL, nil)
	if err != nil {
		return fmt.Errorf("create JWKS request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch JWKS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch JWKS: unexpected status %d", resp.StatusCode)
	}

	bodyReader := io.LimitReader(resp.Body, maxJWKSBodyBytes+1)
	body, err := io.ReadAll(bodyReader)
	if err != nil {
		return fmt.Errorf("read JWKS body: %w", err)
	}
	if int64(len(body)) > maxJWKSBodyBytes {
		return fmt.Errorf("decode JWKS: body exceeds limit")
	}

	var doc jwksDocument
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&doc); err != nil {
		return fmt.Errorf("decode JWKS: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("decode JWKS: unexpected trailing content")
	}

	next := make(map[string]any, len(doc.Keys))
	for _, k := range doc.Keys {
		if k.Kid == "" {
			continue
		}
		if k.Use != "" && k.Use != "sig" {
			continue
		}

		switch k.Kty {
		case "RSA":
			pub, err := parseRSAPublicKey(k)
			if err != nil {
				c.log.Warn("skip invalid RSA jwk", "kid", k.Kid, "error", err)
				continue
			}
			next[k.Kid] = pub
		case "EC":
			pub, err := parseECDSAPublicKey(k)
			if err != nil {
				c.log.Warn("skip invalid EC jwk", "kid", k.Kid, "error", err)
				continue
			}
			next[k.Kid] = pub
		default:
		}
	}

	if len(next) == 0 {
		return fmt.Errorf("JWKS contains no usable signing keys")
	}

	c.mu.Lock()
	c.keys = next
	c.lastFetch = time.Now()
	c.mu.Unlock()

	c.log.Debug("jwks refreshed", "keys", len(next))
	return nil
}

func parseRSAPublicKey(k jwkKey) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("decode n: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("decode e: %w", err)
	}

	n := new(big.Int).SetBytes(nBytes)
	e := 0
	for _, b := range eBytes {
		e = e<<8 + int(b)
	}
	if e == 0 {
		return nil, fmt.Errorf("invalid rsa exponent")
	}
	if n.BitLen() < minRSABits {
		return nil, fmt.Errorf("rsa modulus too small: %d bits", n.BitLen())
	}

	return &rsa.PublicKey{N: n, E: e}, nil
}

func parseECDSAPublicKey(k jwkKey) (*ecdsa.PublicKey, error) {
	curve, err := curveFromName(k.Crv)
	if err != nil {
		return nil, err
	}

	xBytes, err := base64.RawURLEncoding.DecodeString(k.X)
	if err != nil {
		return nil, fmt.Errorf("decode x: %w", err)
	}
	yBytes, err := base64.RawURLEncoding.DecodeString(k.Y)
	if err != nil {
		return nil, fmt.Errorf("decode y: %w", err)
	}

	x := new(big.Int).SetBytes(xBytes)
	y := new(big.Int).SetBytes(yBytes)
	if !curve.IsOnCurve(x, y) {
		return nil, fmt.Errorf("ec key point not on curve")
	}

	return &ecdsa.PublicKey{Curve: curve, X: x, Y: y}, nil
}

func curveFromName(name string) (elliptic.Curve, error) {
	switch name {
	case "P-256":
		return elliptic.P256(), nil
	case "P-384":
		return elliptic.P384(), nil
	case "P-521":
		return elliptic.P521(), nil
	default:
		return nil, fmt.Errorf("unsupported ec curve %q", name)
	}
}

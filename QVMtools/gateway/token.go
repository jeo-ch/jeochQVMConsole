package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/base64"
	"sync"
	"time"
)

// GatewayToken 是一次性迁移令牌：绑定源主机与操作范围，哈希存储，
// 短生命周期且连接成功后即被消费（一次性），避免令牌泄露可被重复使用。
type GatewayToken struct {
	TokenHash  string     `json:"-"`
	HostID     uint       `json:"host_id"`
	Operation  string     `json:"operation"` // register | migration
	ConsumedAt *time.Time `json:"-"`
	ExpiresAt  time.Time  `json:"expires_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// TokenService 负责一次性令牌的签发与校验（纯内存存储）。
type TokenService struct {
	ttl       time.Duration
	maxLength int

	mu     sync.RWMutex
	tokens map[string]*GatewayToken // key = tokenHash
}

// TokenConfig 配置一次性令牌规则。
type TokenConfig struct {
	TTL       time.Duration
	MaxLength int
}

func NewTokenService(cfg TokenConfig) *TokenService {
	if cfg.TTL <= 0 {
		cfg.TTL = 24 * time.Hour
	}
	if cfg.MaxLength <= 0 {
		cfg.MaxLength = 256
	}
	s := &TokenService{
		ttl:       cfg.TTL,
		maxLength: cfg.MaxLength,
		tokens:    make(map[string]*GatewayToken),
	}
	go s.cleanupLoop()
	return s
}

// Issue 签发一个绑定主机与操作的一次性令牌，返回明文 token 供下发给 agent。
func (s *TokenService) Issue(hostID uint, operation string) (string, *GatewayToken, error) {
	plain, err := generateToken()
	if err != nil {
		return "", nil, err
	}
	now := time.Now()
	tok := &GatewayToken{
		TokenHash: hashToken(plain),
		HostID:    hostID,
		Operation: operation,
		ExpiresAt: now.Add(s.ttl),
		CreatedAt: now,
	}
	s.mu.Lock()
	s.tokens[tok.TokenHash] = tok
	s.mu.Unlock()
	return plain, tok, nil
}

// Consume 校验令牌是否有效并一次性消费。无效返回 ErrInvalidToken。
func (s *TokenService) Consume(plain string) (*GatewayToken, error) {
	if plain == "" || len(plain) > s.maxLength {
		return nil, ErrInvalidToken
	}
	h := hashToken(plain)

	s.mu.Lock()
	defer s.mu.Unlock()

	tok, ok := s.tokens[h]
	if !ok {
		return nil, ErrInvalidToken
	}
	if tok.ConsumedAt != nil {
		return nil, ErrTokenConsumed
	}
	if time.Now().After(tok.ExpiresAt) {
		return nil, ErrTokenExpired
	}
	now := time.Now()
	tok.ConsumedAt = &now
	return tok, nil
}

// cleanupLoop 每分钟清理已过期或已消费的令牌。
func (s *TokenService) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		now := time.Now()
		for h, tok := range s.tokens {
			if tok.ConsumedAt != nil || now.After(tok.ExpiresAt) {
				delete(s.tokens, h)
			}
		}
		s.mu.Unlock()
	}
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

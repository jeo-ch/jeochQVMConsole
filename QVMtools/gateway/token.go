package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/base64"
	"errors"
	"time"

	"gorm.io/gorm"
)

// GatewayToken 是一次性迁移令牌：绑定源主机与操作范围，哈希存储，
// 短生命周期且连接成功后即被消费（一次性），避免令牌泄露可被重复使用。
type GatewayToken struct {
	ID         uint       `gorm:"primaryKey" json:"id"`
	TokenHash  string     `gorm:"uniqueIndex;not null" json:"-"`
	HostID     uint       `gorm:"index" json:"host_id"`
	Operation  string     `json:"operation"` // register | migration
	ConsumedAt *time.Time `json:"-"`
	ExpiresAt  time.Time  `json:"expires_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// TokenService 负责一次性令牌的签发与校验。
type TokenService struct {
	db        *gorm.DB
	ttl       time.Duration
	maxLength int
}

// TokenConfig 配置一次性令牌规则。
type TokenConfig struct {
	TTL       time.Duration
	MaxLength int
}

func NewTokenService(db *gorm.DB, cfg TokenConfig) *TokenService {
	if cfg.TTL <= 0 {
		cfg.TTL = 24 * time.Hour
	}
	if cfg.MaxLength <= 0 {
		cfg.MaxLength = 256
	}
	return &TokenService{db: db, ttl: cfg.TTL, maxLength: cfg.MaxLength}
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
	if err := s.db.Create(tok).Error; err != nil {
		return "", nil, err
	}
	return plain, tok, nil
}

// Consume 校验令牌是否有效并一次性消费。无效返回 ErrInvalidToken。
func (s *TokenService) Consume(plain string) (*GatewayToken, error) {
	if plain == "" || len(plain) > s.maxLength {
		return nil, ErrInvalidToken
	}
	sum := sha256.Sum256([]byte(plain))
	h := hex.EncodeToString(sum[:])

	var tok GatewayToken
	if err := s.db.Where("token_hash = ?", h).First(&tok).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvalidToken
		}
		return nil, err
	}
	if tok.ConsumedAt != nil {
		return nil, ErrTokenConsumed
	}
	if time.Now().After(tok.ExpiresAt) {
		return nil, ErrTokenExpired
	}
	now := time.Now()
	res := s.db.Model(&GatewayToken{}).Where("id = ? AND consumed_at IS NULL", tok.ID).Update("consumed_at", now)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, ErrTokenConsumed
	}
	return &tok, nil
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

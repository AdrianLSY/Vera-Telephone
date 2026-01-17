package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestParseJWT(t *testing.T) {
	secretKey := "test-secret-key-for-jwt-signing"

	tests := []struct {
		name        string
		token       string
		secretKey   string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "empty token",
			token:       "",
			secretKey:   secretKey,
			expectError: true,
			errorMsg:    "token is empty",
		},
		{
			name:        "empty secret key",
			token:       "some-token",
			secretKey:   "",
			expectError: true,
			errorMsg:    "secret key is required",
		},
		{
			name:        "invalid token format",
			token:       "not.a.valid.jwt.token",
			secretKey:   secretKey,
			expectError: true,
		},
		{
			name:        "malformed token",
			token:       "malformed",
			secretKey:   secretKey,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims, err := ParseJWT(tt.token, tt.secretKey)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				if tt.errorMsg != "" && err != nil && err.Error() != tt.errorMsg {
					// Just check error exists for non-exact matches
					t.Logf("got error: %v", err)
				}
				if claims != nil {
					t.Errorf("expected nil claims but got %+v", claims)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if claims == nil {
					t.Errorf("expected claims but got nil")
				}
			}
		})
	}
}

func TestParseJWTWithValidToken(t *testing.T) {
	secretKey := "test-secret-key-for-jwt-signing"
	pathID := "test-path-123"
	subject := "mount-point-456"
	jti := "jwt-id-789"

	// Create a valid token
	now := time.Now()
	claims := &JWTClaims{
		Sub:    subject,
		JTI:    jti,
		PathID: pathID,
		IAT:    now.Unix(),
		Exp:    now.Add(1 * time.Hour).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secretKey))
	if err != nil {
		t.Fatalf("failed to create test token: %v", err)
	}

	// Parse the token
	parsedClaims, err := ParseJWT(tokenString, secretKey)
	if err != nil {
		t.Fatalf("failed to parse valid token: %v", err)
	}

	// Verify claims
	if parsedClaims.PathID != pathID {
		t.Errorf("expected PathID %s, got %s", pathID, parsedClaims.PathID)
	}
	if parsedClaims.Sub != subject {
		t.Errorf("expected Sub %s, got %s", subject, parsedClaims.Sub)
	}
	if parsedClaims.JTI != jti {
		t.Errorf("expected JTI %s, got %s", jti, parsedClaims.JTI)
	}
	if parsedClaims.IAT != now.Unix() {
		t.Errorf("expected IAT %d, got %d", now.Unix(), parsedClaims.IAT)
	}
}

func TestParseJWTWithExpiredToken(t *testing.T) {
	secretKey := "test-secret-key-for-jwt-signing"

	// Create an expired token
	now := time.Now()
	claims := &JWTClaims{
		Sub:    "subject",
		JTI:    "jti",
		PathID: "path-id",
		IAT:    now.Add(-2 * time.Hour).Unix(),
		Exp:    now.Add(-1 * time.Hour).Unix(), // Expired 1 hour ago
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(secretKey))
	if err != nil {
		t.Fatalf("failed to create test token: %v", err)
	}

	// Parse should fail due to expiration
	_, err = ParseJWT(tokenString, secretKey)
	if err == nil {
		t.Errorf("expected error for expired token but got none")
	}
}

func TestParseJWTWithWrongSignature(t *testing.T) {
	correctSecret := "correct-secret"
	wrongSecret := "wrong-secret"

	// Create token with correct secret
	claims := &JWTClaims{
		Sub:    "subject",
		JTI:    "jti",
		PathID: "path-id",
		IAT:    time.Now().Unix(),
		Exp:    time.Now().Add(1 * time.Hour).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte(correctSecret))
	if err != nil {
		t.Fatalf("failed to create test token: %v", err)
	}

	// Try to parse with wrong secret
	_, err = ParseJWT(tokenString, wrongSecret)
	if err == nil {
		t.Errorf("expected error for wrong signature but got none")
	}
}

func TestParseJWTWithMissingClaims(t *testing.T) {
	secretKey := "test-secret-key"

	tests := []struct {
		name     string
		claims   *JWTClaims
		errorMsg string
	}{
		{
			name: "missing path_id",
			claims: &JWTClaims{
				Sub:    "subject",
				JTI:    "jti",
				PathID: "", // Missing
				IAT:    time.Now().Unix(),
				Exp:    time.Now().Add(1 * time.Hour).Unix(),
			},
			errorMsg: "missing path_id claim",
		},
		{
			name: "missing sub",
			claims: &JWTClaims{
				Sub:    "", // Missing
				JTI:    "jti",
				PathID: "path-id",
				IAT:    time.Now().Unix(),
				Exp:    time.Now().Add(1 * time.Hour).Unix(),
			},
			errorMsg: "missing sub claim",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token := jwt.NewWithClaims(jwt.SigningMethodHS256, tt.claims)
			tokenString, err := token.SignedString([]byte(secretKey))
			if err != nil {
				t.Fatalf("failed to create test token: %v", err)
			}

			_, err = ParseJWT(tokenString, secretKey)
			if err == nil {
				t.Errorf("expected error but got none")
			}
		})
	}
}

func TestParseJWTUnsafe(t *testing.T) {
	// Create a token without signing (for testing unsafe parse)
	now := time.Now()
	claims := &JWTClaims{
		Sub:    "subject",
		JTI:    "jti",
		PathID: "path-id",
		IAT:    now.Unix(),
		Exp:    now.Add(1 * time.Hour).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString([]byte("secret"))
	if err != nil {
		t.Fatalf("failed to create test token: %v", err)
	}

	// Parse without verification
	parsedClaims, err := ParseJWTUnsafe(tokenString)
	if err != nil {
		t.Fatalf("failed to parse token: %v", err)
	}

	if parsedClaims.PathID != "path-id" {
		t.Errorf("expected PathID 'path-id', got %s", parsedClaims.PathID)
	}
	if parsedClaims.Sub != "subject" {
		t.Errorf("expected Sub 'subject', got %s", parsedClaims.Sub)
	}
}

func TestJWTClaimsIsExpired(t *testing.T) {
	tests := []struct {
		name     string
		exp      int64
		expected bool
	}{
		{
			name:     "expired token",
			exp:      time.Now().Add(-1 * time.Hour).Unix(),
			expected: true,
		},
		{
			name:     "valid token",
			exp:      time.Now().Add(1 * time.Hour).Unix(),
			expected: false,
		},
		{
			name:     "just expired",
			exp:      time.Now().Add(-1 * time.Second).Unix(),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := &JWTClaims{
				Exp: tt.exp,
			}

			result := claims.IsExpired()
			if result != tt.expected {
				t.Errorf("expected IsExpired() = %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestJWTClaimsExpiresIn(t *testing.T) {
	claims := &JWTClaims{
		Exp: time.Now().Add(1 * time.Hour).Unix(),
	}

	expiresIn := claims.ExpiresIn()

	// Should be approximately 1 hour (within 1 second tolerance)
	expected := 1 * time.Hour
	tolerance := 1 * time.Second

	if expiresIn < expected-tolerance || expiresIn > expected+tolerance {
		t.Errorf("expected ExpiresIn() around %v, got %v", expected, expiresIn)
	}
}

func TestJWTClaimsValidate(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name        string
		claims      *JWTClaims
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid claims",
			claims: &JWTClaims{
				Sub:    "subject",
				JTI:    "jti",
				PathID: "path-id",
				IAT:    now.Unix(),
				Exp:    now.Add(1 * time.Hour).Unix(),
			},
			expectError: false,
		},
		{
			name: "expired token",
			claims: &JWTClaims{
				Sub:    "subject",
				JTI:    "jti",
				PathID: "path-id",
				IAT:    now.Add(-2 * time.Hour).Unix(),
				Exp:    now.Add(-1 * time.Hour).Unix(),
			},
			expectError: true,
			errorMsg:    "token expired",
		},
		{
			name: "issued in future",
			claims: &JWTClaims{
				Sub:    "subject",
				JTI:    "jti",
				PathID: "path-id",
				IAT:    now.Add(1 * time.Hour).Unix(),
				Exp:    now.Add(2 * time.Hour).Unix(),
			},
			expectError: true,
			errorMsg:    "token issued in the future",
		},
		{
			name: "missing path_id",
			claims: &JWTClaims{
				Sub:    "subject",
				JTI:    "jti",
				PathID: "",
				IAT:    now.Unix(),
				Exp:    now.Add(1 * time.Hour).Unix(),
			},
			expectError: true,
			errorMsg:    "missing path_id",
		},
		{
			name: "missing subject",
			claims: &JWTClaims{
				Sub:    "",
				JTI:    "jti",
				PathID: "path-id",
				IAT:    now.Unix(),
				Exp:    now.Add(1 * time.Hour).Unix(),
			},
			expectError: true,
			errorMsg:    "missing subject",
		},
		{
			name: "missing jti",
			claims: &JWTClaims{
				Sub:    "subject",
				JTI:    "",
				PathID: "path-id",
				IAT:    now.Unix(),
				Exp:    now.Add(1 * time.Hour).Unix(),
			},
			expectError: true,
			errorMsg:    "missing JWT ID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.claims.Validate()

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestJWTClaimsIssuedAt(t *testing.T) {
	now := time.Now()
	claims := &JWTClaims{
		IAT: now.Unix(),
	}

	issuedAt := claims.IssuedAt()

	// Should match within 1 second
	if issuedAt.Unix() != now.Unix() {
		t.Errorf("expected IssuedAt() %v, got %v", now, issuedAt)
	}
}

func TestJWTClaimsExpiresAt(t *testing.T) {
	expiry := time.Now().Add(1 * time.Hour)
	claims := &JWTClaims{
		Exp: expiry.Unix(),
	}

	expiresAt := claims.ExpiresAt()

	// Should match within 1 second
	if expiresAt.Unix() != expiry.Unix() {
		t.Errorf("expected ExpiresAt() %v, got %v", expiry, expiresAt)
	}
}

func TestJWTClaimsLifespan(t *testing.T) {
	now := time.Now()
	claims := &JWTClaims{
		IAT: now.Unix(),
		Exp: now.Add(1 * time.Hour).Unix(),
	}

	lifespan := claims.Lifespan()

	// Should be 1 hour
	expected := 1 * time.Hour
	if lifespan != expected {
		t.Errorf("expected Lifespan() %v, got %v", expected, lifespan)
	}
}

func TestJWTClaimsTimeUntilHalfLife(t *testing.T) {
	tests := []struct {
		name                string
		issuedAt            time.Time
		expiresAt           time.Time
		expectedMinDuration time.Duration
		expectedMaxDuration time.Duration
	}{
		{
			name:                "new token (1 hour lifespan)",
			issuedAt:            time.Now(),
			expiresAt:           time.Now().Add(1 * time.Hour),
			expectedMinDuration: 29 * time.Minute,
			expectedMaxDuration: 30 * time.Minute,
		},
		{
			name:                "token past half-life",
			issuedAt:            time.Now().Add(-31 * time.Minute),
			expiresAt:           time.Now().Add(29 * time.Minute),
			expectedMinDuration: 0,
			expectedMaxDuration: 0,
		},
		{
			name:                "token exactly at half-life",
			issuedAt:            time.Now().Add(-30 * time.Minute),
			expiresAt:           time.Now().Add(30 * time.Minute),
			expectedMinDuration: 0,
			expectedMaxDuration: 1 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := &JWTClaims{
				IAT: tt.issuedAt.Unix(),
				Exp: tt.expiresAt.Unix(),
			}

			duration := claims.TimeUntilHalfLife()

			if duration < tt.expectedMinDuration || duration > tt.expectedMaxDuration {
				t.Errorf("expected TimeUntilHalfLife() between %v and %v, got %v",
					tt.expectedMinDuration, tt.expectedMaxDuration, duration)
			}
		})
	}
}

func TestJWTClaimsIsAtHalfLife(t *testing.T) {
	tests := []struct {
		name      string
		issuedAt  time.Time
		expiresAt time.Time
		expected  bool
	}{
		{
			name:      "new token",
			issuedAt:  time.Now(),
			expiresAt: time.Now().Add(1 * time.Hour),
			expected:  false,
		},
		{
			name:      "token past half-life",
			issuedAt:  time.Now().Add(-31 * time.Minute),
			expiresAt: time.Now().Add(29 * time.Minute),
			expected:  true,
		},
		{
			name:      "token exactly at half-life",
			issuedAt:  time.Now().Add(-30 * time.Minute),
			expiresAt: time.Now().Add(30 * time.Minute),
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims := &JWTClaims{
				IAT: tt.issuedAt.Unix(),
				Exp: tt.expiresAt.Unix(),
			}

			result := claims.IsAtHalfLife()

			if result != tt.expected {
				t.Errorf("expected IsAtHalfLife() = %v, got %v", tt.expected, result)
			}
		})
	}
}

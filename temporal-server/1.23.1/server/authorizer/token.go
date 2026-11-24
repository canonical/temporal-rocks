package authorizer

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	serviceAccountSuffix = ".iam.gserviceaccount.com"
	// tokenInfoRequestTimeout bounds each tokeninfo request so a slow or
	// unreachable endpoint cannot stall request authorization indefinitely.
	tokenInfoRequestTimeout = 10 * time.Second
	// defaultTokenInfoCacheTTL bounds how long a fetched TokenInfo is reused
	// before the tokeninfo endpoint is queried again. It is kept short because
	// it also bounds how quickly a revoked token stops being accepted; cached
	// entries never outlive the token's own expiry either way.
	defaultTokenInfoCacheTTL = 5 * time.Minute
	// tokenInfoCacheMaxSize bounds the token info cache; each unique access
	// token is a single entry.
	tokenInfoCacheMaxSize = 1024
)

// TokenInfo holds information parsed from a Google OAuth token.
type TokenInfo struct {
	Azp           string `json:"azp"`
	Aud           string `json:"aud"`
	Sub           string `json:"sub"`
	Scope         string `json:"scope"`
	Exp           string `json:"exp"`
	ExpiresIn     string `json:"expires_in"`
	Email         string `json:"email"`
	EmailVerified string `json:"email_verified"`
	AccessType    string `json:"access_type"`
}

// tokenInfoCacheEntry is a fetched TokenInfo together with the time after
// which it must be fetched again.
type tokenInfoCacheEntry struct {
	info      *TokenInfo
	expiresAt time.Time
}

// Verifier provides configuration parameters for verifying Google OAuth tokens.
type Verifier struct {
	GoogleClientID string
	TokenURL       string
	RequiredScope  string
	// CacheTTL controls how long a fetched TokenInfo is reused before the
	// tokeninfo endpoint is queried again. Zero uses
	// defaultTokenInfoCacheTTL; a negative value disables caching.
	CacheTTL time.Duration
	// HTTPTimeout bounds each tokeninfo request. Zero uses
	// tokenInfoRequestTimeout.
	HTTPTimeout time.Duration

	mu     sync.Mutex
	client *http.Client
	// cache is keyed by the SHA-256 digest of the access token so the cache
	// never holds usable bearer tokens.
	cache map[tokenCacheKey]tokenInfoCacheEntry
}

// tokenCacheKey is the SHA-256 digest of an access token.
type tokenCacheKey [sha256.Size]byte

// NewVerifier returns a new Verifier implementation.
func NewVerifier(clientID string, tokenUrl string, requiredScope string) *Verifier {
	return &Verifier{
		GoogleClientID: clientID,
		TokenURL:       tokenUrl,
		RequiredScope:  requiredScope,
	}
}

// GetTokenInfo fetches a given access token's information. Responses that
// pass VerifyToken are cached briefly so a burst of requests carrying the same
// token does not hit the tokeninfo endpoint once per request.
func (v *Verifier) GetTokenInfo(accessToken string) (*TokenInfo, error) {
	client := v.lazyClient()
	key := tokenCacheKey(sha256.Sum256([]byte(accessToken)))
	if info, ok := v.cachedTokenInfo(key); ok {
		return info, nil
	}

	req, err := http.NewRequest(http.MethodGet, v.TokenURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Read the response body for more error details
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		bodyString := string(bodyBytes)

		return nil, fmt.Errorf("request error: %s, request body: %s", resp.Status, bodyString)
	}

	var tokenInfo TokenInfo
	if err := json.NewDecoder(resp.Body).Decode(&tokenInfo); err != nil {
		return nil, err
	}

	// Only cache tokens this verifier would accept. tokeninfo returns 200 for
	// any valid Google token, including ones issued to other OAuth clients;
	// caching those would let anyone with a Google account fill the cache.
	if v.VerifyToken(&tokenInfo) == nil {
		v.storeTokenInfo(key, &tokenInfo)
	}
	return &tokenInfo, nil
}

// lazyClient returns the shared HTTP client, creating it on first use so that
// a zero-value Verifier remains usable.
func (v *Verifier) lazyClient() *http.Client {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.client == nil {
		timeout := v.HTTPTimeout
		if timeout == 0 {
			timeout = tokenInfoRequestTimeout
		}
		v.client = &http.Client{Timeout: timeout}
	}
	return v.client
}

// cachedTokenInfo returns the cached TokenInfo for the given token digest if
// it is still fresh.
func (v *Verifier) cachedTokenInfo(key tokenCacheKey) (*TokenInfo, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	entry, ok := v.cache[key]
	if !ok {
		return nil, false
	}
	if !time.Now().Before(entry.expiresAt) {
		delete(v.cache, key)
		return nil, false
	}
	return entry.info, true
}

// storeTokenInfo caches a verified TokenInfo. Only tokens that passed
// VerifyToken are cached: a token that is rejected, either by the endpoint or
// by VerifyToken, is re-checked on every request and can never become
// accepted through the cache. The entry expires at the earlier of the
// configured TTL and the token's own expiry, and VerifyToken re-validates the
// expiry on every call regardless, so a cached token can never be accepted
// after it has expired.
func (v *Verifier) storeTokenInfo(key tokenCacheKey, info *TokenInfo) {
	ttl := v.CacheTTL
	if ttl == 0 {
		ttl = defaultTokenInfoCacheTTL
	}
	expiresAt := time.Now().Add(ttl)
	if intExp, err := strconv.ParseInt(info.Exp, 0, 64); err == nil {
		if exp := time.Unix(intExp, 0); exp.Before(expiresAt) {
			expiresAt = exp
		}
	}
	if !time.Now().Before(expiresAt) {
		return
	}

	v.mu.Lock()
	defer v.mu.Unlock()
	if v.cache == nil {
		v.cache = make(map[tokenCacheKey]tokenInfoCacheEntry)
	}
	// Opportunistically drop expired entries to keep the map bounded.
	for k, entry := range v.cache {
		if !time.Now().Before(entry.expiresAt) {
			delete(v.cache, k)
		}
	}
	if len(v.cache) >= tokenInfoCacheMaxSize {
		return
	}
	v.cache[key] = tokenInfoCacheEntry{info: info, expiresAt: expiresAt}
}

// VerifyToken verifies that a given TokenInfo is valid by
// checking that the required scope, email and expiry time
// restrictions exist.
func (v *Verifier) VerifyToken(token *TokenInfo) error {
	intExp, err := strconv.ParseInt(token.Exp, 0, 64)
	if err != nil {
		return fmt.Errorf("error validating token: %v", err)
	}

	expirationTime := time.Unix(intExp, 0)
	currentTime := time.Now()

	// Google service-account access tokens report their numeric unique ID as
	// `azp` rather than an OAuth client ID, so the client check is skipped for
	// them; the suffix still bounds the exemption and the usual expiry, scope
	// and email-verified checks apply.
	if !strings.HasSuffix(token.Email, serviceAccountSuffix) && v.GoogleClientID != "" && token.Azp != v.GoogleClientID {
		return errors.New("incorrect token client id")
	}

	if v.RequiredScope != "" && !slices.Contains(strings.Fields(token.Scope), v.RequiredScope) {
		return errors.New("token scope must include email")
	}

	if token.EmailVerified != "true" {
		return errors.New("token email not verified")
	}

	if currentTime.After(expirationTime) {
		return errors.New("token expired")
	}

	return nil
}

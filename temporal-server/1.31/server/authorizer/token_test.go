package authorizer_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	qt "github.com/frankban/quicktest"

	"github.com/canonical/temporal-rocks/temporal-server/1.31/server/authorizer"
)

// tokenInfoServer returns an httptest server that answers tokeninfo requests
// with the given status/body and counts how many times it was hit.
func tokenInfoServer(c *qt.C, status int, body string) (*httptest.Server, *int32) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	c.Cleanup(srv.Close)
	return srv, &calls
}

func validTokenInfoJSON() string {
	return fmt.Sprintf(`{
		"azp": "client_id",
		"aud": "client_id",
		"scope": "https://www.googleapis.com/auth/userinfo.email",
		"exp": "%d",
		"expires_in": "3599",
		"email": "user@example.com",
		"email_verified": "true"
	}`, time.Now().Add(time.Hour).Unix())
}

func TestGetTokenInfoCachesVerifiedToken(t *testing.T) {
	c := qt.New(t)
	srv, calls := tokenInfoServer(c, http.StatusOK, validTokenInfoJSON())

	v := authorizer.NewVerifier("client_id", srv.URL, "")
	info, err := v.GetTokenInfo("token-abc")
	c.Assert(err, qt.IsNil)
	c.Assert(info.Email, qt.Equals, "user@example.com")

	// A second request for the same token must be served from the cache.
	info2, err := v.GetTokenInfo("token-abc")
	c.Assert(err, qt.IsNil)
	c.Assert(info2, qt.DeepEquals, info)
	c.Assert(atomic.LoadInt32(calls), qt.Equals, int32(1))

	// A different token must not collide with the cached entry.
	_, err = v.GetTokenInfo("token-xyz")
	c.Assert(err, qt.IsNil)
	c.Assert(atomic.LoadInt32(calls), qt.Equals, int32(2))
}

func TestGetTokenInfoDoesNotCacheFailures(t *testing.T) {
	c := qt.New(t)
	srv, calls := tokenInfoServer(c, http.StatusBadRequest, `{"error": "invalid_token"}`)

	v := authorizer.NewVerifier("client_id", srv.URL, "")
	_, err := v.GetTokenInfo("bad-token")
	c.Assert(err, qt.ErrorMatches, "request error: .*")

	// A rejected token is never cached: every request re-checks with the
	// endpoint so it can never become accepted through the cache.
	_, err = v.GetTokenInfo("bad-token")
	c.Assert(err, qt.ErrorMatches, "request error: .*")
	c.Assert(atomic.LoadInt32(calls), qt.Equals, int32(2))
}

func TestGetTokenInfoDoesNotCacheUnverifiedTokens(t *testing.T) {
	c := qt.New(t)
	// tokeninfo accepts the token (200), but it was issued to a different
	// OAuth client, so VerifyToken rejects it and it must not be cached.
	otherClient := fmt.Sprintf(`{
		"azp": "other_client_id",
		"scope": "https://www.googleapis.com/auth/userinfo.email",
		"exp": "%d",
		"email": "user@example.com",
		"email_verified": "true"
	}`, time.Now().Add(time.Hour).Unix())
	srv, calls := tokenInfoServer(c, http.StatusOK, otherClient)

	v := authorizer.NewVerifier("client_id", srv.URL, "")
	for i := 0; i < 2; i++ {
		info, err := v.GetTokenInfo("other-client-token")
		c.Assert(err, qt.IsNil)
		c.Assert(v.VerifyToken(info), qt.ErrorMatches, "incorrect token client id")
	}
	c.Assert(atomic.LoadInt32(calls), qt.Equals, int32(2))
}

func TestGetTokenInfoExpiredTokenNotCached(t *testing.T) {
	c := qt.New(t)
	expired := fmt.Sprintf(`{
		"scope": "s",
		"exp": "%d",
		"email": "user@example.com",
		"email_verified": "true"
	}`, time.Now().Add(-time.Hour).Unix())
	srv, calls := tokenInfoServer(c, http.StatusOK, expired)

	v := authorizer.NewVerifier("client_id", srv.URL, "")
	_, err := v.GetTokenInfo("expired-token")
	c.Assert(err, qt.IsNil)

	// An already-expired token is never stored, so it is re-fetched.
	_, err = v.GetTokenInfo("expired-token")
	c.Assert(err, qt.IsNil)
	c.Assert(atomic.LoadInt32(calls), qt.Equals, int32(2))
}

func TestGetTokenInfoCacheTTLExpires(t *testing.T) {
	c := qt.New(t)
	srv, calls := tokenInfoServer(c, http.StatusOK, validTokenInfoJSON())

	v := authorizer.NewVerifier("client_id", srv.URL, "")
	v.CacheTTL = 50 * time.Millisecond

	_, err := v.GetTokenInfo("token-abc")
	c.Assert(err, qt.IsNil)
	_, err = v.GetTokenInfo("token-abc")
	c.Assert(err, qt.IsNil)
	c.Assert(atomic.LoadInt32(calls), qt.Equals, int32(1))

	// Once the TTL passes the entry is stale and the endpoint is queried again.
	time.Sleep(60 * time.Millisecond)
	_, err = v.GetTokenInfo("token-abc")
	c.Assert(err, qt.IsNil)
	c.Assert(atomic.LoadInt32(calls), qt.Equals, int32(2))
}

func TestGetTokenInfoCachedStillExpiryChecked(t *testing.T) {
	c := qt.New(t)
	// A token that expires very soon: it is cached only until exp, and
	// VerifyToken must still reject it once exp passes — the cache can never
	// extend a token's life. exp is second-granular, so use a short window.
	exp := time.Now().Add(2 * time.Second).Unix()
	shortLived := fmt.Sprintf(`{
		"azp": "client_id",
		"scope": "https://www.googleapis.com/auth/userinfo.email",
		"exp": "%d",
		"email": "user@example.com",
		"email_verified": "true"
	}`, exp)
	srv, calls := tokenInfoServer(c, http.StatusOK, shortLived)

	v := authorizer.NewVerifier("client_id", srv.URL, "")
	info, err := v.GetTokenInfo("soon-expired")
	c.Assert(err, qt.IsNil)
	c.Assert(v.VerifyToken(info), qt.IsNil)

	for !time.Now().After(time.Unix(exp, 0)) {
		time.Sleep(50 * time.Millisecond)
	}

	// The cache entry died with the token's exp, so the endpoint is queried
	// again; either way VerifyToken must deny it — the cache can never extend
	// a token's life.
	cached, err := v.GetTokenInfo("soon-expired")
	c.Assert(err, qt.IsNil)
	err = v.VerifyToken(cached)
	c.Assert(err, qt.ErrorMatches, "token expired")
	c.Assert(atomic.LoadInt32(calls), qt.Equals, int32(2))
}

func TestGetTokenInfoRequestTimeout(t *testing.T) {
	c := qt.New(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(250 * time.Millisecond)
		fmt.Fprint(w, validTokenInfoJSON())
	}))
	c.Cleanup(srv.Close)

	v := authorizer.NewVerifier("client_id", srv.URL, "")
	v.HTTPTimeout = 50 * time.Millisecond

	start := time.Now()
	_, err := v.GetTokenInfo("token-abc")
	c.Assert(err, qt.Not(qt.IsNil))
	c.Assert(time.Since(start) < 200*time.Millisecond, qt.IsTrue)
}

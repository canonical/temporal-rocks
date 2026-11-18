package authorizer_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/canonical/ofga"
	"github.com/canonical/temporal-rocks/temporal-server/1.23.1/server/authorizer"
	mock "github.com/canonical/temporal-rocks/temporal-server/1.23.1/server/authorizer/mocks"
	gomock "github.com/golang/mock/gomock"

	qt "github.com/frankban/quicktest"
	"go.temporal.io/server/common/authorization"
)

func TestTokenVerifier(t *testing.T) {
	c := qt.New(t)

	validToken := &authorizer.TokenInfo{
		Exp:           fmt.Sprint(time.Now().Add(time.Hour).Unix()),
		EmailVerified: "true",
		Email:         "user@example.com",
		Scope:         "https://www.googleapis.com/auth/userinfo.email",
		Azp:           "client_id",
	}

	expiredToken := &authorizer.TokenInfo{
		Exp:           fmt.Sprint(time.Now().Add(-time.Hour).Unix()),
		EmailVerified: "true",
		Email:         "user@example.com",
		Scope:         "https://www.googleapis.com/auth/userinfo.email",
		Azp:           "client_id",
	}

	emailNotVerifiedToken := &authorizer.TokenInfo{
		Exp:           fmt.Sprint(time.Now().Add(time.Hour).Unix()),
		EmailVerified: "false",
		Email:         "user@example.com",
		Scope:         "https://www.googleapis.com/auth/userinfo.email",
		Azp:           "client_id",
	}

	noScopeToken := &authorizer.TokenInfo{
		Exp:           fmt.Sprint(time.Now().Add(time.Hour).Unix()),
		EmailVerified: "true",
		Email:         "user@example.com",
		Scope:         "profile",
		Azp:           "client_id",
	}

	differentClientToken := &authorizer.TokenInfo{
		Exp:           fmt.Sprint(time.Now().Add(time.Hour).Unix()),
		EmailVerified: "true",
		Email:         "user@example.com",
		Scope:         "https://www.googleapis.com/auth/userinfo.email",
		Azp:           "badwolf_client_id",
	}

	serviceAccountToken := &authorizer.TokenInfo{
		Exp:           fmt.Sprint(time.Now().Add(time.Hour).Unix()),
		EmailVerified: "true",
		Email:         "service-account@project-id.iam.gserviceaccount.com",
		Scope:         "https://www.googleapis.com/auth/userinfo.email",
		Azp:           "123",
	}

	tests := []struct {
		desc string
		// Inputs
		token *authorizer.TokenInfo
		// Outputs
		expectedErr string
	}{
		{
			desc:  "success: valid token",
			token: validToken,
		},
		{
			desc:        "error: expired token",
			token:       expiredToken,
			expectedErr: "token expired",
		},
		{
			desc:        "error: token email not verified",
			token:       emailNotVerifiedToken,
			expectedErr: "token email not verified",
		},
		{
			desc:        "error: missing required scope",
			token:       noScopeToken,
			expectedErr: "token scope must include email",
		},
		{
			desc:        "error: mismatch in client id",
			token:       differentClientToken,
			expectedErr: "incorrect token client id",
		},
		{
			desc:  "success: valid service account token",
			token: serviceAccountToken,
		},
	}

	for _, test := range tests {
		test := test

		c.Run(test.desc, func(c *qt.C) {
			c.Parallel()

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			tv := authorizer.NewVerifier("client_id", "https://www.googleapis.com/oauth2/v3/tokeninfo", "https://www.googleapis.com/auth/userinfo.email")

			err := tv.VerifyToken(test.token)
			if test.expectedErr != "" {
				c.Assert(err, qt.ErrorMatches, test.expectedErr)
			} else {
				c.Assert(err, qt.IsNil)
			}
		})
	}
}

func TestGetClaims(t *testing.T) {
	c := qt.New(t)

	validToken := &authorizer.TokenInfo{
		Exp:           fmt.Sprint(time.Now().Add(time.Hour).Unix()),
		EmailVerified: "true",
		Email:         "user@example.com",
		Scope:         "https://www.googleapis.com/auth/userinfo.email",
	}
	validAuthToken := "Bearer sometoken"
	tests := []struct {
		desc string
		// Inputs
		authInfo          *authorization.AuthInfo
		adminGroups       string
		setupExpectations func(tv *mock.MockTokenVerifier, np *mock.MockNamespaceAccessProvider) []*gomock.Call
		// Outputs
		expectedClaims *authorization.Claims
		expectedErr    string
	}{{
		desc: "success: authInfo contains valid token and user belongs to admin group",
		authInfo: &authorization.AuthInfo{
			AuthToken: validAuthToken,
		},
		adminGroups: "group1",
		setupExpectations: func(tv *mock.MockTokenVerifier, np *mock.MockNamespaceAccessProvider) []*gomock.Call {
			return []*gomock.Call{
				tv.EXPECT().GetTokenInfo(gomock.Any()).Return(validToken, nil),
				tv.EXPECT().VerifyToken(gomock.Any()).Return(nil),
				np.EXPECT().GetUserGroups(gomock.Any(), gomock.Any()).Return([]string{"group1", "group2"}, nil),
			}
		},
		expectedClaims: &authorization.Claims{
			System:     authorization.RoleWriter,
			Namespaces: map[string]authorization.Role{},
		},
	}, {
		desc: "success: authInfo contains valid token and user does not have access to namespace",
		authInfo: &authorization.AuthInfo{
			AuthToken: validAuthToken,
		},
		adminGroups: "group1",
		setupExpectations: func(tv *mock.MockTokenVerifier, np *mock.MockNamespaceAccessProvider) []*gomock.Call {
			return []*gomock.Call{
				tv.EXPECT().GetTokenInfo(gomock.Any()).Return(validToken, nil),
				tv.EXPECT().VerifyToken(gomock.Any()).Return(nil),
				np.EXPECT().GetUserGroups(gomock.Any(), gomock.Any()).Return([]string{}, nil),
				np.EXPECT().GetNamespaceAccessInformation(gomock.Any(), gomock.Any(), gomock.Any()).Return([]authorizer.NamespaceAccess{}, nil),
			}
		},
		expectedClaims: &authorization.Claims{
			Namespaces: map[string]authorization.Role{"": authorization.RoleReader},
		},
	}, {
		desc: "success: authInfo contains valid token and user has access to namespace",
		authInfo: &authorization.AuthInfo{
			AuthToken: validAuthToken,
		},
		adminGroups: "group1",
		setupExpectations: func(tv *mock.MockTokenVerifier, np *mock.MockNamespaceAccessProvider) []*gomock.Call {
			return []*gomock.Call{
				tv.EXPECT().GetTokenInfo(gomock.Any()).Return(validToken, nil),
				tv.EXPECT().VerifyToken(gomock.Any()).Return(nil),
				np.EXPECT().GetUserGroups(gomock.Any(), gomock.Any()).Return([]string{}, nil),
				np.EXPECT().GetNamespaceAccessInformation(gomock.Any(), gomock.Any(), gomock.Any()).Return([]authorizer.NamespaceAccess{{Namespace: "foobar", Relation: "writer"}}, nil),
			}
		},
		expectedClaims: &authorization.Claims{
			Namespaces: map[string]authorization.Role{"": authorization.RoleReader, "foobar": authorization.RoleWriter},
		},
	},
	}

	for _, test := range tests {
		test := test

		c.Run(test.desc, func(c *qt.C) {
			c.Parallel()

			// Execution
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			tv := mock.NewMockTokenVerifier(ctrl)
			np := mock.NewMockNamespaceAccessProvider(ctrl)
			if test.setupExpectations != nil {
				gomock.InAnyOrder(test.setupExpectations(tv, np))
			}

			cm := authorizer.TokenClaimMapper{
				TokenVerifier:           tv,
				NamespaceAccessProvider: np,
				AdminGroups:             test.adminGroups,
			}
			claims, err := cm.GetClaims(test.authInfo)
			c.Assert(claims, qt.DeepEquals, test.expectedClaims)
			if test.expectedErr != "" {
				c.Assert(err, qt.ErrorMatches, test.expectedErr)
			}
		})
	}
}

// fakeTupleFinder serves scripted FindMatchingTuples pages keyed by the
// continuation token it is called with, so the pagination loops can be
// exercised without a live OpenFGA server.
type fakeTupleFinder struct {
	pages map[string]fakePage
	calls []string
}

type fakePage struct {
	tuples []ofga.TimestampedTuple
	next   string
	err    error
}

func (f *fakeTupleFinder) FindMatchingTuples(_ context.Context, _ ofga.Tuple, _ int32, continuationToken string) ([]ofga.TimestampedTuple, string, error) {
	f.calls = append(f.calls, continuationToken)
	if len(f.calls) > 10 {
		return nil, "", fmt.Errorf("FindMatchingTuples did not advance past the first page")
	}
	page, ok := f.pages[continuationToken]
	if !ok {
		return nil, "", fmt.Errorf("unexpected continuation token %q", continuationToken)
	}
	if page.err != nil {
		return nil, "", page.err
	}
	return page.tuples, page.next, nil
}

func groupTuple(id string) ofga.TimestampedTuple {
	return ofga.TimestampedTuple{Tuple: ofga.Tuple{
		Target: &ofga.Entity{Kind: "group", ID: id},
	}}
}

func namespaceTuple(ns, relation string) ofga.TimestampedTuple {
	return ofga.TimestampedTuple{Tuple: ofga.Tuple{
		Relation: ofga.Relation(relation),
		Target:   &ofga.Entity{Kind: "namespace", ID: ns},
	}}
}

func TestGetUserGroupsPagination(t *testing.T) {
	c := qt.New(t)

	// Two pages of results: the second page must actually be requested with
	// the continuation token from the first. A regression that re-declares
	// the token inside the loop refetches page one forever.
	finder := &fakeTupleFinder{pages: map[string]fakePage{
		"":       {tuples: []ofga.TimestampedTuple{groupTuple("group-a")}, next: "page-2"},
		"page-2": {tuples: []ofga.TimestampedTuple{groupTuple("group-b")}},
	}}
	client := &authorizer.AuthClient{OfgaClient: finder}

	groups, err := client.GetUserGroups(context.Background(), "user@example.com")
	c.Assert(err, qt.IsNil)
	c.Assert(groups, qt.DeepEquals, []string{"group-a", "group-b"})
	c.Assert(finder.calls, qt.DeepEquals, []string{"", "page-2"})
}

func TestGetUserGroupsPaginationError(t *testing.T) {
	c := qt.New(t)

	finder := &fakeTupleFinder{pages: map[string]fakePage{
		"":       {tuples: []ofga.TimestampedTuple{groupTuple("group-a")}, next: "page-2"},
		"page-2": {err: fmt.Errorf("ofga read failed")},
	}}
	client := &authorizer.AuthClient{OfgaClient: finder}

	_, err := client.GetUserGroups(context.Background(), "user@example.com")
	c.Assert(err, qt.ErrorMatches, "ofga read failed")
}

func TestGetNamespaceAccessInformationPagination(t *testing.T) {
	c := qt.New(t)

	// Each group paginates independently and resets to an empty continuation
	// token, so two groups mean two full paginated walks.
	finder := &fakeTupleFinder{pages: map[string]fakePage{
		"":       {tuples: []ofga.TimestampedTuple{namespaceTuple("ns-a", "writer")}, next: "page-2"},
		"page-2": {tuples: []ofga.TimestampedTuple{namespaceTuple("ns-b", "reader")}},
	}}
	client := &authorizer.AuthClient{OfgaClient: finder}

	access, err := client.GetNamespaceAccessInformation(context.Background(), "user@example.com", []string{"group-a", "group-b"})
	c.Assert(err, qt.IsNil)
	c.Assert(access, qt.DeepEquals, []authorizer.NamespaceAccess{
		{Namespace: "ns-a", Relation: "writer"},
		{Namespace: "ns-b", Relation: "reader"},
		{Namespace: "ns-a", Relation: "writer"},
		{Namespace: "ns-b", Relation: "reader"},
	})
	c.Assert(finder.calls, qt.DeepEquals, []string{"", "page-2", "", "page-2"})
}

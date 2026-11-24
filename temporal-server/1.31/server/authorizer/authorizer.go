// The MIT License
//
// Copyright (c) 2020 Temporal Technologies Inc.  All rights reserved.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package authorizer

import (
	"context"
	"fmt"
	"strings"

	"go.temporal.io/server/common/api"
	"go.temporal.io/server/common/authorization"
	"go.uber.org/zap"
)

type authorizer struct {
	logger *zap.Logger
}

// NewAuthorizer returns a new authorization.Authorizer implementation.
func NewAuthorizer(logger *zap.Logger) authorization.Authorizer {
	return &authorizer{
		logger: logger,
	}
}

var decisionAllow = authorization.Result{Decision: authorization.DecisionAllow}
var decisionDeny = authorization.Result{Decision: authorization.DecisionDeny}

// Authorize returns an authorization decision (either DecisionAllow or
// DecisionDeny) based on the information contained in the provided Claims.
//
// It determines if the request is for a read or write operation and it checks
// the namespace to which the request is directed, comparing that against the
// claims to take a decision.
//
// Note: the provided Claims are trusted completely, no additional checks are
// performed on their source.
func (a *authorizer) Authorize(_ context.Context, claims *authorization.Claims,
	target *authorization.CallTarget) (authorization.Result, error) {
	apiName := shortApiName(target.APIName)

	if authorization.IsHealthCheckAPI(apiName) || authorization.IsHealthCheckAPI(target.APIName) {
		a.logger.Info(fmt.Sprintf("allowing access to health check API %s", apiName))
		return decisionAllow, nil
	}

	if claims == nil {
		a.logWarn(fmt.Sprintf("denied access to %s on namespace %s, no claims provided", apiName, target.Namespace))
		return decisionDeny, nil
	}

	metadata := api.GetMethodMetadata(target.APIName)

	var hasRole authorization.Role
	switch metadata.Scope {
	case api.ScopeCluster:
		hasRole = claims.System
		if metadata.Access == api.AccessReadOnly {
			// Any namespace role implies cluster-wide read access (e.g.
			// ListNamespaces); namespace claims do not grant cluster write or
			// admin operations.
			for _, role := range claims.Namespaces {
				if role > hasRole {
					hasRole = role
				}
			}
		}
	case api.ScopeNamespace:
		// System-level claims apply across all namespaces.
		hasRole = claims.System | claims.Namespaces[target.Namespace]
	default:
		a.logWarn(fmt.Sprintf("denied access to %s on namespace %s: unknown API scope", apiName, target.Namespace))
		return decisionDeny, nil
	}

	if hasRole >= requiredRole(metadata.Access) {
		a.logger.Info(fmt.Sprintf("allowing access to %s on namespace %s", apiName, target.Namespace))
		return decisionAllow, nil
	}

	a.logWarn(fmt.Sprintf("denied access to %s on namespace %s; namespaces found in claims: %v", apiName, target.Namespace, claims.Namespaces))

	return decisionDeny, nil
}

// requiredRole maps an api.Access level to the minimum role that grants it:
// read-only APIs need a reader, write APIs a writer, and everything else
// (admin-level or unknown access) an admin. This mirrors upstream's
// defaultAuthorizer.
func requiredRole(access api.Access) authorization.Role {
	switch access {
	case api.AccessReadOnly:
		return authorization.RoleReader
	case api.AccessWrite:
		return authorization.RoleWriter
	default:
		return authorization.RoleAdmin
	}
}

func (a *authorizer) logWarn(msg string) {
	if a.logger != nil {
		a.logger.Warn(msg)
	}
}

func shortApiName(api string) string {
	index := strings.LastIndex(api, "/")
	if index > -1 {
		return api[index+1:]
	}
	return api
}

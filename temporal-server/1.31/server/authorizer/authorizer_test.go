package authorizer_test

import (
	"context"
	"testing"

	"github.com/canonical/temporal-rocks/temporal-server/1.31/server/authorizer"

	qt "github.com/frankban/quicktest"
	"go.temporal.io/server/common/authorization"
	"go.temporal.io/server/common/log"
)

const (
	apiListNamespaces    = "/temporal.api.workflowservice.v1.WorkflowService/ListNamespaces"
	apiStartWorkflow     = "/temporal.api.workflowservice.v1.WorkflowService/StartWorkflowExecution"
	apiListWorkflows     = "/temporal.api.workflowservice.v1.WorkflowService/ListWorkflowExecutions"
	apiDescribeNamespace = "/temporal.api.workflowservice.v1.WorkflowService/DescribeNamespace"
	apiRegisterNamespace = "/temporal.api.workflowservice.v1.WorkflowService/RegisterNamespace"
	apiUpdateNamespace   = "/temporal.api.workflowservice.v1.WorkflowService/UpdateNamespace"
	apiPollWorkflowTask  = "/temporal.api.workflowservice.v1.WorkflowService/PollWorkflowTaskQueue"
	apiHealthCheck       = "/grpc.health.v1.Health/Check"
	apiGetSystemInfo     = "/temporal.api.workflowservice.v1.WorkflowService/GetSystemInfo"
)

func TestAuthorize(t *testing.T) {
	c := qt.New(t)

	tests := []struct {
		desc string
		// Inputs
		claims   *authorization.Claims
		target   string
		targetNS string
		// Outputs
		expectedDecision authorization.Decision
	}{{
		desc:             "allow: health check API is reachable without claims",
		claims:           nil,
		target:           apiHealthCheck,
		expectedDecision: authorization.DecisionAllow,
	}, {
		desc:             "allow: GetSystemInfo is reachable without claims",
		claims:           nil,
		target:           apiGetSystemInfo,
		expectedDecision: authorization.DecisionAllow,
	}, {
		desc:             "deny: no claims on a regular API",
		claims:           nil,
		target:           apiListNamespaces,
		expectedDecision: authorization.DecisionDeny,
	}, {
		desc: "allow: caller has system read access permissions, read-only cluster API",
		claims: &authorization.Claims{
			System: authorization.RoleReader,
		},
		target:           apiListNamespaces,
		expectedDecision: authorization.DecisionAllow,
	}, {
		desc: "deny: caller has system read access permissions, write namespace API",
		claims: &authorization.Claims{
			System: authorization.RoleReader,
		},
		target:           apiStartWorkflow,
		targetNS:         "test-ns",
		expectedDecision: authorization.DecisionDeny,
	}, {
		desc: "deny: system writer cannot call admin-level APIs",
		claims: &authorization.Claims{
			System: authorization.RoleWriter,
		},
		target:           apiRegisterNamespace,
		targetNS:         "new-ns",
		expectedDecision: authorization.DecisionDeny,
	}, {
		desc: "allow: system admin can call admin-level APIs",
		claims: &authorization.Claims{
			System: authorization.RoleAdmin,
		},
		target:           apiRegisterNamespace,
		targetNS:         "new-ns",
		expectedDecision: authorization.DecisionAllow,
	}, {
		desc: "allow: caller has specific namespace permissions, get system info",
		claims: &authorization.Claims{
			Namespaces: map[string]authorization.Role{"test-ns": authorization.RoleWriter},
		},
		target:           apiGetSystemInfo,
		expectedDecision: authorization.DecisionAllow,
	}, {
		desc: "allow: namespace writer can call cluster read-only APIs",
		claims: &authorization.Claims{
			Namespaces: map[string]authorization.Role{"test-ns": authorization.RoleWriter},
		},
		target:           apiListNamespaces,
		expectedDecision: authorization.DecisionAllow,
	}, {
		desc: "deny: namespace writer cannot call cluster admin APIs",
		claims: &authorization.Claims{
			Namespaces: map[string]authorization.Role{"test-ns": authorization.RoleWriter},
		},
		target:           "/temporal.server.api.adminservice.v1.AdminService/ListClusters",
		expectedDecision: authorization.DecisionDeny,
	}, {
		desc: "allow: caller has specific namespace permissions, execute workflow",
		claims: &authorization.Claims{
			System:     authorization.RoleReader,
			Namespaces: map[string]authorization.Role{"test-ns": authorization.RoleWriter},
		},
		target:           apiStartWorkflow,
		targetNS:         "test-ns",
		expectedDecision: authorization.DecisionAllow,
	}, {
		desc: "deny: caller has specific namespace permissions, execute workflow in another ns",
		claims: &authorization.Claims{
			System:     authorization.RoleReader,
			Namespaces: map[string]authorization.Role{"test-ns": authorization.RoleWriter},
		},
		target:           apiStartWorkflow,
		targetNS:         "test-ns-2",
		expectedDecision: authorization.DecisionDeny,
	}, {
		desc: "deny: namespace writer cannot update or delete the namespace (admin API)",
		claims: &authorization.Claims{
			System:     authorization.RoleReader,
			Namespaces: map[string]authorization.Role{"test-ns": authorization.RoleWriter},
		},
		target:           apiUpdateNamespace,
		targetNS:         "test-ns",
		expectedDecision: authorization.DecisionDeny,
	}, {
		desc: "allow: namespace admin can update its own namespace",
		claims: &authorization.Claims{
			System:     authorization.RoleReader,
			Namespaces: map[string]authorization.Role{"test-ns": authorization.RoleAdmin},
		},
		target:           apiUpdateNamespace,
		targetNS:         "test-ns",
		expectedDecision: authorization.DecisionAllow,
	}, {
		desc: "deny: namespace admin on one ns cannot update another ns",
		claims: &authorization.Claims{
			System:     authorization.RoleReader,
			Namespaces: map[string]authorization.Role{"test-ns": authorization.RoleAdmin},
		},
		target:           apiUpdateNamespace,
		targetNS:         "other-ns",
		expectedDecision: authorization.DecisionDeny,
	}, {
		desc: "allow: namespace reader can call read-only APIs on its ns",
		claims: &authorization.Claims{
			System:     authorization.RoleReader,
			Namespaces: map[string]authorization.Role{"test-ns": authorization.RoleReader},
		},
		target:           apiListWorkflows,
		targetNS:         "test-ns",
		expectedDecision: authorization.DecisionAllow,
	}, {
		desc: "deny: namespace reader cannot call write APIs on its ns",
		claims: &authorization.Claims{
			System:     authorization.RoleReader,
			Namespaces: map[string]authorization.Role{"test-ns": authorization.RoleReader},
		},
		target:           apiPollWorkflowTask,
		targetNS:         "test-ns",
		expectedDecision: authorization.DecisionDeny,
	}, {
		desc: "allow: caller has system write access permissions, execute workflow",
		claims: &authorization.Claims{
			System: authorization.RoleWriter,
		},
		target:           apiStartWorkflow,
		targetNS:         "test-ns",
		expectedDecision: authorization.DecisionAllow,
	}, {
		desc: "deny: unknown API defaults to cluster admin, denied for regular user",
		claims: &authorization.Claims{
			System:     authorization.RoleReader,
			Namespaces: map[string]authorization.Role{"test-ns": authorization.RoleWriter},
		},
		target:           "/some.unknown.Service/Method",
		targetNS:         "test-ns",
		expectedDecision: authorization.DecisionDeny,
	}}

	for _, test := range tests {
		c.Run(test.desc, func(c *qt.C) {
			c.Parallel()

			// Execution
			zapLogger := log.BuildZapLogger(log.Config{})
			a := authorizer.NewAuthorizer(zapLogger)
			result, err := a.Authorize(context.Background(), test.claims, &authorization.CallTarget{
				APIName:   test.target,
				Namespace: test.targetNS,
			})

			c.Assert(result.Decision, qt.Equals, test.expectedDecision)
			c.Assert(err, qt.IsNil)
		})
	}
}

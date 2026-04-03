/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	kallucinateiov1 "github.com/NickCao/kallucinate/api/v1"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/artifact"
	"google.golang.org/adk/memory"
	"google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/mcptoolset"
	"google.golang.org/genai"

	genaiopenai "github.com/achetronic/adk-utils-go/genai/openai"
)

// KallucinateReconciler reconciles a Kallucinate object
type KallucinateReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	runner *runner.Runner
}

func NewKallucinateReconciler(kclient client.Client, scheme *runtime.Scheme) (*KallucinateReconciler, error) {
	openaiModel := genaiopenai.New(genaiopenai.Config{
		APIKey:    os.Getenv("OPENAI_API_KEY"),
		BaseURL:   os.Getenv("OPENAI_API_BASE"),
		ModelName: os.Getenv("OPENAI_API_MODEL"),
	})

	k8sMcp, err := mcptoolset.New(mcptoolset.Config{
		Transport: &mcp.StreamableClientTransport{
			Endpoint: "http://127.0.0.1:8080/mcp",
		},
	})
	if err != nil {
		return nil, err
	}

	kallucinateAgent, err := llmagent.New(llmagent.Config{
		Name:        "kallucinate",
		Model:       openaiModel,
		Description: "Manages Kubernetes resources by kallucinating",
		Instruction: `
You are kallucinate, a kubernetes admin agent.
You receive events from the reconciliation loop when the Kallucinate CRD (or the managed resources) are changed.
You use kubernetes MCP tools to receive desired state descriptions from the Kallucinate CRD.
Your job:
1. If no resources exist yet, use the kubernetes MCP tools to create the resources described in the prompt.
2. If resources already exist, compare their current state against what the prompt describes. Fix any drift, errors, or unhealthy conditions.
3. Only make changes when necessary. If everything matches the desired state and is healthy, respond with "No changes needed."
4. When creating resources, set ownerReferences as well as ownerReferences.controller to ensure update to the managed resources would notify you.
Rules:
- All resources you create must be in the same namespace as the Kallucinate CR that triggered this request, unless the prompt explicitly specifies otherwise.
- Use the prompt as the source of truth for what SHOULD exist.
- Use the current resource state to understand what DOES exist and whether it's healthy.
- When resources are failing (CrashLoopBackOff, pending, error conditions), diagnose and fix them.
- Never delete resources unless they clearly contradict the prompt.
- Be precise with resource specifications. Do not add unnecessary fields or defaults.
- After making changes, briefly summarize what you did and why.
`,
		Toolsets: []tool.Toolset{k8sMcp},
	})

	runner, err := runner.New(runner.Config{
		AppName:           "kallucinate",
		Agent:             kallucinateAgent,
		SessionService:    session.InMemoryService(),
		ArtifactService:   artifact.InMemoryService(),
		MemoryService:     memory.InMemoryService(),
		AutoCreateSession: true,
	})
	if err != nil {
		return nil, err
	}

	return &KallucinateReconciler{
		Client: kclient,
		Scheme: scheme,
		runner: runner,
	}, nil
}

// +kubebuilder:rbac:groups=kallucinate.io,resources=kallucinates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kallucinate.io,resources=kallucinates/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kallucinate.io,resources=kallucinates/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Kallucinate object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.23.3/pkg/reconcile
func (r *KallucinateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	for event, err := range r.runner.Run(ctx, "dummy", "dummy", &genai.Content{
		Role: "user",
		Parts: []*genai.Part{
			genai.NewPartFromText(fmt.Sprintf("The changed kallucinate resource is: namespace(%s), name(%s)", req.Namespace, req.Name)),
		},
	}, agent.RunConfig{
		StreamingMode: agent.StreamingModeSSE,
	}) {
		logger.Info("event", "event", event, "err", err)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KallucinateReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kallucinateiov1.Kallucinate{}).
		Named("kallucinate").
		Complete(r)
}

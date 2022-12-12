package schemagen

import (
	"context"
	"encoding/json"

	"github.com/whyrusleeping/gosky/xrpc"
)

// schema: app.bsky.actor.createScene

type ActorCreateScene_Input struct {
	Handle      string `json:"handle" cborgen:"handle"`
	RecoveryKey string `json:"recoveryKey" cborgen:"recoveryKey"`
}

func (t *ActorCreateScene_Input) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["handle"] = t.Handle
	out["recoveryKey"] = t.RecoveryKey
	return json.Marshal(out)
}

type ActorCreateScene_Output struct {
	Handle      string         `json:"handle" cborgen:"handle"`
	Did         string         `json:"did" cborgen:"did"`
	Declaration *SystemDeclRef `json:"declaration" cborgen:"declaration"`
}

func (t *ActorCreateScene_Output) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	out["declaration"] = t.Declaration
	out["did"] = t.Did
	out["handle"] = t.Handle
	return json.Marshal(out)
}

func ActorCreateScene(ctx context.Context, c *xrpc.Client, input ActorCreateScene_Input) (*ActorCreateScene_Output, error) {
	var out ActorCreateScene_Output
	if err := c.Do(ctx, xrpc.Procedure, "application/json", "app.bsky.actor.createScene", nil, input, &out); err != nil {
		return nil, err
	}

	return &out, nil
}

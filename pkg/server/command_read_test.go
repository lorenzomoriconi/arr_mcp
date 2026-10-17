package server

import (
	"context"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"strings"
	"testing"
)

func TestCommandReceiptsCanBeReadWithoutExposingNativeBody(t *testing.T) {
	srv, paths := recordingArr(t, `[{"id":10,"name":"ManualImport","status":"completed","body":{"path":"private-path"}}]`)
	cs := connect(t, mediaCfg(srv.URL))
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "sonarr_list_commands", Arguments: map[string]any{}})
	if err != nil || res.IsError {
		t.Fatalf("command read failed: %v %+v", err, res)
	}
	if len(*paths) != 1 || (*paths)[0] != "GET /api/v3/command" || strings.Contains(contentText(res), "private-path") {
		t.Fatalf("unexpected receipt result: %s %v", contentText(res), *paths)
	}
}
